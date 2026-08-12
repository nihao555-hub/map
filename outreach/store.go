package outreach

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // SQLite driver.
)

const defaultQueryLimit = 100

// Store persists outreach campaigns, contacts, message history and
// suppressions in SQLite.
//
// Store deliberately does not persist Settings.Password. Mailbox credentials
// must be supplied through OUTREACH_SMTP_PASSWORD so an authorization code is
// never written to the database or committed with application configuration.
type Store struct {
	db *sql.DB

	secretMu        sync.RWMutex
	runtimePassword string
}

// NewStore opens (or creates) the outreach SQLite database.
func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open outreach database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)

	store := &Store{db: db}

	if err := store.initialize(); err != nil {
		_ = db.Close()

		return nil, err
	}

	return store, nil
}

func (s *Store) initialize() error {
	pragmas := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	}

	for _, statement := range pragmas {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("configure outreach database: %w", err)
		}
	}

	const schema = `
CREATE TABLE IF NOT EXISTS outreach_campaigns (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	status TEXT NOT NULL,
	job_id TEXT NOT NULL DEFAULT '',
	value_proposition TEXT NOT NULL DEFAULT '',
	proof TEXT NOT NULL DEFAULT '',
	call_to_action TEXT NOT NULL DEFAULT '',
	sequence TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS outreach_contacts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	campaign_id TEXT NOT NULL REFERENCES outreach_campaigns(id) ON DELETE CASCADE,
	email TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT '',
	address TEXT NOT NULL DEFAULT '',
	city TEXT NOT NULL DEFAULT '',
	website TEXT NOT NULL DEFAULT '',
	phone TEXT NOT NULL DEFAULT '',
	rating TEXT NOT NULL DEFAULT '',
	review_count INTEGER NOT NULL DEFAULT 0,
	timezone TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	next_step INTEGER NOT NULL DEFAULT 0,
	next_send_at INTEGER NOT NULL DEFAULT 0,
	last_sent_at INTEGER NOT NULL DEFAULT 0,
	root_message_id TEXT NOT NULL DEFAULT '',
	root_subject TEXT NOT NULL DEFAULT '',
	last_message_id TEXT NOT NULL DEFAULT '',
	send_failures INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	UNIQUE(campaign_id, email)
);

CREATE INDEX IF NOT EXISTS outreach_contacts_due_idx
	ON outreach_contacts(status, next_send_at);
CREATE INDEX IF NOT EXISTS outreach_contacts_email_idx
	ON outreach_contacts(email);

CREATE TABLE IF NOT EXISTS outreach_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	campaign_id TEXT NOT NULL REFERENCES outreach_campaigns(id) ON DELETE CASCADE,
	contact_id INTEGER NOT NULL REFERENCES outreach_contacts(id) ON DELETE CASCADE,
	direction TEXT NOT NULL,
	kind TEXT NOT NULL DEFAULT '',
	step INTEGER NOT NULL DEFAULT 0,
	subject TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL DEFAULT '',
	message_id TEXT NOT NULL DEFAULT '',
	in_reply_to TEXT NOT NULL DEFAULT '',
	from_email TEXT NOT NULL DEFAULT '',
	to_email TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS outreach_messages_message_id_idx
	ON outreach_messages(message_id) WHERE message_id <> '';
CREATE INDEX IF NOT EXISTS outreach_messages_contact_idx
	ON outreach_messages(contact_id, created_at);

CREATE TABLE IF NOT EXISTS outreach_suppressions (
	email TEXT PRIMARY KEY,
	reason TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS outreach_settings (
	id INTEGER PRIMARY KEY CHECK(id = 1),
	data TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS outreach_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("create outreach schema: %w", err)
	}

	return nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// SaveSettings stores non-secret mailbox and scheduling settings.
func (s *Store) SaveSettings(ctx context.Context, settings Settings) error {
	if settings.Password != "" {
		s.secretMu.Lock()
		s.runtimePassword = settings.Password
		s.secretMu.Unlock()
	}

	settings.Password = ""

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode outreach settings: %w", err)
	}

	const query = `
INSERT INTO outreach_settings(id, data, updated_at) VALUES(1, ?, ?)
ON CONFLICT(id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`

	if _, err := s.db.ExecContext(ctx, query, data, time.Now().UTC().Unix()); err != nil {
		return fmt.Errorf("save outreach settings: %w", err)
	}

	return nil
}

// Settings loads stored settings, applies defaults and finally applies
// environment overrides (including the authorization code).
func (s *Store) Settings(ctx context.Context) (Settings, error) {
	settings := DefaultSettings()

	var data []byte

	err := s.db.QueryRowContext(ctx, "SELECT data FROM outreach_settings WHERE id = 1").Scan(&data)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Settings{}, fmt.Errorf("load outreach settings: %w", err)
	}

	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return Settings{}, fmt.Errorf("decode outreach settings: %w", err)
		}
	}

	s.secretMu.RLock()
	settings.Password = s.runtimePassword
	s.secretMu.RUnlock()

	settings.ApplyEnvOverrides()

	return settings, nil
}

// CreateCampaign creates a campaign. The sequence is validated before any
// data is written.
func (s *Store) CreateCampaign(ctx context.Context, campaign *Campaign) error {
	if err := campaign.Validate(); err != nil {
		return err
	}

	if err := ValidateSequenceTemplates(campaign.Sequence); err != nil {
		return err
	}

	now := time.Now().UTC()
	if campaign.CreatedAt.IsZero() {
		campaign.CreatedAt = now
	}

	campaign.UpdatedAt = now

	if campaign.Status == "" {
		campaign.Status = CampaignStatusDraft
	}

	sequence, err := json.Marshal(campaign.Sequence)
	if err != nil {
		return fmt.Errorf("encode campaign sequence: %w", err)
	}

	const query = `
INSERT INTO outreach_campaigns(
	id, name, status, job_id, value_proposition, proof, call_to_action,
	sequence, created_at, updated_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.db.ExecContext(
		ctx,
		query,
		campaign.ID,
		campaign.Name,
		campaign.Status,
		campaign.JobID,
		campaign.ValueProposition,
		campaign.Proof,
		campaign.CallToAction,
		sequence,
		campaign.CreatedAt.Unix(),
		campaign.UpdatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("create outreach campaign: %w", err)
	}

	return nil
}

// Campaign returns one campaign.
func (s *Store) Campaign(ctx context.Context, id string) (Campaign, error) {
	const query = `
SELECT id, name, status, job_id, value_proposition, proof, call_to_action,
	sequence, created_at, updated_at
FROM outreach_campaigns WHERE id = ?`

	return scanCampaign(s.db.QueryRowContext(ctx, query, id))
}

// Campaigns returns campaigns ordered newest first.
func (s *Store) Campaigns(ctx context.Context) ([]Campaign, error) {
	const query = `
SELECT id, name, status, job_id, value_proposition, proof, call_to_action,
	sequence, created_at, updated_at
FROM outreach_campaigns ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list outreach campaigns: %w", err)
	}
	defer rows.Close()

	var campaigns []Campaign

	for rows.Next() {
		campaign, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}

		campaigns = append(campaigns, campaign)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list outreach campaigns: %w", err)
	}

	return campaigns, nil
}

// SetCampaignStatus changes a campaign status. Contacts stay stopped whenever
// the campaign is not active because DueContacts joins against campaign state.
func (s *Store) SetCampaignStatus(ctx context.Context, id, status string) error {
	switch status {
	case CampaignStatusDraft, CampaignStatusActive, CampaignStatusPaused, CampaignStatusDone:
	default:
		return fmt.Errorf("invalid campaign status %q", status)
	}

	result, err := s.db.ExecContext(
		ctx,
		"UPDATE outreach_campaigns SET status = ?, updated_at = ? WHERE id = ?",
		status,
		time.Now().UTC().Unix(),
		id,
	)
	if err != nil {
		return fmt.Errorf("update campaign status: %w", err)
	}

	return requireAffected(result)
}

// DeleteCampaign permanently deletes a campaign and its contacts/messages.
func (s *Store) DeleteCampaign(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM outreach_campaigns WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete outreach campaign: %w", err)
	}

	return requireAffected(result)
}

// AddContacts inserts contacts, normalizing addresses and skipping duplicates
// and globally suppressed recipients. It returns the number inserted.
func (s *Store) AddContacts(ctx context.Context, contacts []Contact) (int, error) {
	if len(contacts) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin contact import: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	const query = `
INSERT OR IGNORE INTO outreach_contacts(
	campaign_id, email, name, category, address, city, website, phone, rating,
	review_count, timezone,
	status, next_step, next_send_at, last_sent_at, root_message_id, root_subject,
	last_message_id, send_failures, created_at, updated_at
)
SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
WHERE NOT EXISTS(SELECT 1 FROM outreach_suppressions WHERE email = ?)`

	inserted := 0
	now := time.Now().UTC()

	for i := range contacts {
		contact := &contacts[i]
		contact.Email = normalizeEmail(contact.Email)
		if contact.CampaignID == "" || contact.Email == "" {
			continue
		}

		if contact.Status == "" {
			contact.Status = ContactStatusActive
		}

		if contact.NextSendAt.IsZero() {
			contact.NextSendAt = now
		}

		if contact.CreatedAt.IsZero() {
			contact.CreatedAt = now
		}

		contact.UpdatedAt = now

		result, err := tx.ExecContext(
			ctx,
			query,
			contact.CampaignID,
			contact.Email,
			contact.Name,
			contact.Category,
			contact.Address,
			contact.City,
			contact.Website,
			contact.Phone,
			contact.Rating,
			contact.ReviewCount,
			contact.Timezone,
			contact.Status,
			contact.NextStep,
			toUnix(contact.NextSendAt),
			toUnix(contact.LastSentAt),
			contact.RootMessageID,
			contact.RootSubject,
			contact.LastMessageID,
			contact.SendFailures,
			toUnix(contact.CreatedAt),
			toUnix(contact.UpdatedAt),
			contact.Email,
		)
		if err != nil {
			return 0, fmt.Errorf("insert outreach contact %s: %w", contact.Email, err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("count inserted contacts: %w", err)
		}

		inserted += int(affected)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit contact import: %w", err)
	}

	return inserted, nil
}

// Contacts lists contacts in a campaign.
func (s *Store) Contacts(ctx context.Context, campaignID string, limit int) ([]Contact, error) {
	if limit <= 0 {
		limit = defaultQueryLimit
	}

	const query = `
SELECT id, campaign_id, email, name, category, address, city, website, phone,
	rating, review_count, timezone, status, next_step, next_send_at, last_sent_at,
	root_message_id, root_subject, last_message_id, send_failures, created_at,
	updated_at
FROM outreach_contacts
WHERE campaign_id = ?
ORDER BY created_at DESC
LIMIT ?`

	return queryContacts(ctx, s.db, query, campaignID, limit)
}

// Contact returns one contact by ID.
func (s *Store) Contact(ctx context.Context, id int64) (Contact, error) {
	const query = `
SELECT id, campaign_id, email, name, category, address, city, website, phone,
	rating, review_count, timezone, status, next_step, next_send_at, last_sent_at,
	root_message_id, root_subject, last_message_id, send_failures, created_at,
	updated_at
FROM outreach_contacts WHERE id = ?`

	return scanContact(s.db.QueryRowContext(ctx, query, id))
}

// DueContacts returns contacts eligible to send now, but only from active
// campaigns.
func (s *Store) DueContacts(ctx context.Context, now time.Time, limit int) ([]Contact, error) {
	if limit <= 0 {
		limit = 1
	}

	const query = `
SELECT c.id, c.campaign_id, c.email, c.name, c.category, c.address, c.city,
	c.website, c.phone, c.rating, c.review_count, c.timezone, c.status,
	c.next_step, c.next_send_at, c.last_sent_at, c.root_message_id,
	c.root_subject, c.last_message_id, c.send_failures, c.created_at,
	c.updated_at
FROM outreach_contacts c
JOIN outreach_campaigns p ON p.id = c.campaign_id
WHERE c.status = ? AND c.next_send_at <= ? AND p.status = ?
ORDER BY c.next_send_at, c.id
LIMIT ?`

	return queryContacts(
		ctx,
		s.db,
		query,
		ContactStatusActive,
		now.UTC().Unix(),
		CampaignStatusActive,
		limit,
	)
}

// RescheduleContact moves an active contact to the next valid send window.
func (s *Store) RescheduleContact(ctx context.Context, id int64, sendAt time.Time) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE outreach_contacts
		 SET next_send_at = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		toUnix(sendAt),
		time.Now().UTC().Unix(),
		id,
		ContactStatusActive,
	)
	if err != nil {
		return fmt.Errorf("reschedule outreach contact: %w", err)
	}

	return requireAffected(result)
}

// CompleteContact closes a contact whose sequence has no remaining step.
func (s *Store) CompleteContact(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE outreach_contacts
		 SET status = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		ContactStatusCompleted,
		time.Now().UTC().Unix(),
		id,
		ContactStatusActive,
	)
	if err != nil {
		return fmt.Errorf("complete outreach contact: %w", err)
	}

	return requireAffected(result)
}

// RecordSent atomically records an outgoing message and advances the contact
// to its next step/status.
func (s *Store) RecordSent(
	ctx context.Context,
	contact *Contact,
	message *Message,
	nextSendAt time.Time,
	completed bool,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sent message transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := insertMessage(ctx, tx, message); err != nil {
		return err
	}

	status := ContactStatusActive
	if completed {
		status = ContactStatusCompleted
	}

	rootMessageID := contact.RootMessageID
	rootSubject := contact.RootSubject
	if rootMessageID == "" {
		rootMessageID = message.MessageID
		rootSubject = strings.TrimPrefix(message.Subject, "Re: ")
	}

	const update = `
UPDATE outreach_contacts
SET status = ?, next_step = ?, next_send_at = ?, last_sent_at = ?,
	root_message_id = ?, root_subject = ?, last_message_id = ?,
	send_failures = 0, updated_at = ?
WHERE id = ? AND status = ?`

	now := message.CreatedAt.UTC()
	result, err := tx.ExecContext(
		ctx,
		update,
		status,
		contact.NextStep+1,
		toUnix(nextSendAt),
		toUnix(now),
		rootMessageID,
		rootSubject,
		message.MessageID,
		toUnix(now),
		contact.ID,
		ContactStatusActive,
	)
	if err != nil {
		return fmt.Errorf("advance outreach contact: %w", err)
	}

	if err := requireAffected(result); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sent message: %w", err)
	}

	return nil
}

// RecordManualMessage stores a manually composed reply without advancing the
// automated sequence.
func (s *Store) RecordManualMessage(ctx context.Context, message *Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin manual message transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := insertMessage(ctx, tx, message); err != nil {
		return err
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE outreach_contacts
		 SET last_message_id = ?, last_sent_at = ?, updated_at = ?
		 WHERE id = ?`,
		message.MessageID,
		toUnix(message.CreatedAt),
		toUnix(message.CreatedAt),
		message.ContactID,
	); err != nil {
		return fmt.Errorf("update contact after manual reply: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit manual message: %w", err)
	}

	return nil
}

// RecordSendFailure schedules a retry with backoff, or marks the contact
// failed after three consecutive errors.
func (s *Store) RecordSendFailure(ctx context.Context, contactID int64, retryAt time.Time) error {
	const query = `
UPDATE outreach_contacts
SET send_failures = send_failures + 1,
	status = CASE WHEN send_failures + 1 >= 3 THEN ? ELSE status END,
	next_send_at = ?, updated_at = ?
WHERE id = ? AND status = ?`

	result, err := s.db.ExecContext(
		ctx,
		query,
		ContactStatusFailed,
		toUnix(retryAt),
		time.Now().UTC().Unix(),
		contactID,
		ContactStatusActive,
	)
	if err != nil {
		return fmt.Errorf("record send failure: %w", err)
	}

	return requireAffected(result)
}

// FindContactForInbound matches an inbound email by RFC Message-ID reference,
// falling back to sender address.
func (s *Store) FindContactForInbound(
	ctx context.Context,
	fromEmail string,
	references []string,
) (Contact, error) {
	for _, reference := range references {
		reference = normalizeMessageID(reference)
		if reference == "" {
			continue
		}

		const query = `
SELECT c.id, c.campaign_id, c.email, c.name, c.category, c.address, c.city,
	c.website, c.phone, c.rating, c.review_count, c.timezone, c.status,
	c.next_step, c.next_send_at, c.last_sent_at, c.root_message_id,
	c.root_subject, c.last_message_id, c.send_failures, c.created_at,
	c.updated_at
FROM outreach_contacts c
LEFT JOIN outreach_messages m ON m.contact_id = c.id
WHERE c.root_message_id = ? OR c.last_message_id = ? OR m.message_id = ?
ORDER BY c.updated_at DESC LIMIT 1`

		contact, err := scanContact(s.db.QueryRowContext(ctx, query, reference, reference, reference))
		if err == nil {
			return contact, nil
		}

		if !errors.Is(err, ErrNotFound) {
			return Contact{}, err
		}
	}

	fromEmail = normalizeEmail(fromEmail)
	if fromEmail == "" {
		return Contact{}, ErrNotFound
	}

	const byEmail = `
SELECT id, campaign_id, email, name, category, address, city, website, phone,
	rating, review_count, timezone, status, next_step, next_send_at, last_sent_at,
	root_message_id, root_subject, last_message_id, send_failures, created_at,
	updated_at
FROM outreach_contacts
WHERE email = ? AND last_sent_at > 0
ORDER BY last_sent_at DESC LIMIT 1`

	return scanContact(s.db.QueryRowContext(ctx, byEmail, fromEmail))
}

// RecordInbound stores a reply/bounce/unsubscribe once, updates the contact,
// and adds a global suppression for bounces/unsubscribes.
func (s *Store) RecordInbound(ctx context.Context, contact *Contact, message *Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin inbound message transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := insertMessage(ctx, tx, message); err != nil {
		if isUniqueConstraint(err) {
			return nil
		}

		return err
	}

	status := ContactStatusReplied
	if message.Kind == InboundKindBounce {
		status = ContactStatusBounced
	}

	if message.Kind == InboundKindUnsubscribe {
		status = ContactStatusUnsubscribed
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE outreach_contacts
		 SET status = ?, last_message_id = ?, updated_at = ?
		 WHERE id = ?`,
		status,
		message.MessageID,
		time.Now().UTC().Unix(),
		contact.ID,
	); err != nil {
		return fmt.Errorf("stop outreach contact after inbound message: %w", err)
	}

	if message.Kind == InboundKindBounce || message.Kind == InboundKindUnsubscribe {
		const suppress = `
INSERT INTO outreach_suppressions(email, reason, created_at) VALUES(?, ?, ?)
ON CONFLICT(email) DO UPDATE SET reason = excluded.reason`

		if _, err := tx.ExecContext(
			ctx,
			suppress,
			normalizeEmail(contact.Email),
			message.Kind,
			time.Now().UTC().Unix(),
		); err != nil {
			return fmt.Errorf("suppress outreach contact: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit inbound message: %w", err)
	}

	return nil
}

// Messages lists messages, optionally constrained to one campaign.
func (s *Store) Messages(ctx context.Context, campaignID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = defaultQueryLimit
	}

	query := `
SELECT id, campaign_id, contact_id, direction, kind, step, subject, body,
	message_id, in_reply_to, from_email, to_email, created_at
FROM outreach_messages`

	var args []any
	if campaignID != "" {
		query += " WHERE campaign_id = ?"
		args = append(args, campaignID)
	}

	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list outreach messages: %w", err)
	}
	defer rows.Close()

	var messages []Message

	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}

		messages = append(messages, message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list outreach messages: %w", err)
	}

	return messages, nil
}

// Stats returns aggregate counters for one campaign.
func (s *Store) Stats(ctx context.Context, campaignID string) (CampaignStats, error) {
	const query = `
SELECT
	COUNT(*),
	COALESCE(SUM(CASE WHEN last_sent_at > 0 THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0)
FROM outreach_contacts WHERE campaign_id = ?`

	var stats CampaignStats

	err := s.db.QueryRowContext(
		ctx,
		query,
		ContactStatusReplied,
		ContactStatusBounced,
		ContactStatusUnsubscribed,
		ContactStatusCompleted,
		campaignID,
	).Scan(
		&stats.Contacts,
		&stats.Sent,
		&stats.Replied,
		&stats.Bounced,
		&stats.Unsubscribed,
		&stats.Completed,
	)
	if err != nil {
		return CampaignStats{}, fmt.Errorf("load campaign stats: %w", err)
	}

	return stats, nil
}

// SentToday counts outgoing messages since start, normally the sender's local
// midnight.
func (s *Store) SentToday(ctx context.Context, start time.Time) (int, error) {
	var count int

	const query = `
SELECT COUNT(*) FROM outreach_messages
WHERE direction = ? AND created_at >= ?`

	if err := s.db.QueryRowContext(ctx, query, DirectionOut, start.UTC().Unix()).Scan(&count); err != nil {
		return 0, fmt.Errorf("count today's sent messages: %w", err)
	}

	return count, nil
}

// FirstSentAt returns the timestamp of the first outgoing message.
func (s *Store) FirstSentAt(ctx context.Context) (time.Time, error) {
	var unix sql.NullInt64

	const query = `SELECT MIN(created_at) FROM outreach_messages WHERE direction = ?`

	if err := s.db.QueryRowContext(ctx, query, DirectionOut).Scan(&unix); err != nil {
		return time.Time{}, fmt.Errorf("load first sent time: %w", err)
	}

	if !unix.Valid {
		return time.Time{}, nil
	}

	return fromUnix(unix.Int64), nil
}

// Meta returns an internal cursor/value.
func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var value string

	err := s.db.QueryRowContext(ctx, "SELECT value FROM outreach_meta WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("load outreach metadata: %w", err)
	}

	return value, nil
}

// SetMeta persists an internal cursor/value.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	const query = `
INSERT INTO outreach_meta(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`

	if _, err := s.db.ExecContext(ctx, query, key, value); err != nil {
		return fmt.Errorf("save outreach metadata: %w", err)
	}

	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func scanCampaign(row rowScanner) (Campaign, error) {
	var (
		campaign      Campaign
		sequence      []byte
		createdAtUnix int64
		updatedAtUnix int64
	)

	err := row.Scan(
		&campaign.ID,
		&campaign.Name,
		&campaign.Status,
		&campaign.JobID,
		&campaign.ValueProposition,
		&campaign.Proof,
		&campaign.CallToAction,
		&sequence,
		&createdAtUnix,
		&updatedAtUnix,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Campaign{}, ErrNotFound
	}

	if err != nil {
		return Campaign{}, fmt.Errorf("scan outreach campaign: %w", err)
	}

	if err := json.Unmarshal(sequence, &campaign.Sequence); err != nil {
		return Campaign{}, fmt.Errorf("decode campaign sequence: %w", err)
	}

	campaign.CreatedAt = fromUnix(createdAtUnix)
	campaign.UpdatedAt = fromUnix(updatedAtUnix)

	return campaign, nil
}

func queryContacts(
	ctx context.Context,
	queryer queryer,
	query string,
	args ...any,
) ([]Contact, error) {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query outreach contacts: %w", err)
	}
	defer rows.Close()

	var contacts []Contact

	for rows.Next() {
		contact, err := scanContact(rows)
		if err != nil {
			return nil, err
		}

		contacts = append(contacts, contact)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query outreach contacts: %w", err)
	}

	return contacts, nil
}

func scanContact(row rowScanner) (Contact, error) {
	var (
		contact                                      Contact
		nextSendAt, lastSentAt, createdAt, updatedAt int64
	)

	err := row.Scan(
		&contact.ID,
		&contact.CampaignID,
		&contact.Email,
		&contact.Name,
		&contact.Category,
		&contact.Address,
		&contact.City,
		&contact.Website,
		&contact.Phone,
		&contact.Rating,
		&contact.ReviewCount,
		&contact.Timezone,
		&contact.Status,
		&contact.NextStep,
		&nextSendAt,
		&lastSentAt,
		&contact.RootMessageID,
		&contact.RootSubject,
		&contact.LastMessageID,
		&contact.SendFailures,
		&createdAt,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Contact{}, ErrNotFound
	}

	if err != nil {
		return Contact{}, fmt.Errorf("scan outreach contact: %w", err)
	}

	contact.NextSendAt = fromUnix(nextSendAt)
	contact.LastSentAt = fromUnix(lastSentAt)
	contact.CreatedAt = fromUnix(createdAt)
	contact.UpdatedAt = fromUnix(updatedAt)

	return contact, nil
}

func insertMessage(ctx context.Context, tx *sql.Tx, message *Message) error {
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}

	const query = `
INSERT INTO outreach_messages(
	campaign_id, contact_id, direction, kind, step, subject, body,
	message_id, in_reply_to, from_email, to_email, created_at
) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := tx.ExecContext(
		ctx,
		query,
		message.CampaignID,
		message.ContactID,
		message.Direction,
		message.Kind,
		message.Step,
		message.Subject,
		message.Body,
		normalizeMessageID(message.MessageID),
		normalizeMessageID(message.InReplyTo),
		normalizeEmail(message.FromEmail),
		normalizeEmail(message.ToEmail),
		message.CreatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("insert outreach message: %w", err)
	}

	messageID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read outreach message id: %w", err)
	}

	message.ID = messageID
	message.MessageID = normalizeMessageID(message.MessageID)
	message.InReplyTo = normalizeMessageID(message.InReplyTo)

	return nil
}

func scanMessage(row rowScanner) (Message, error) {
	var (
		message   Message
		createdAt int64
	)

	err := row.Scan(
		&message.ID,
		&message.CampaignID,
		&message.ContactID,
		&message.Direction,
		&message.Kind,
		&message.Step,
		&message.Subject,
		&message.Body,
		&message.MessageID,
		&message.InReplyTo,
		&message.FromEmail,
		&message.ToEmail,
		&createdAt,
	)
	if err != nil {
		return Message{}, fmt.Errorf("scan outreach message: %w", err)
	}

	message.CreatedAt = fromUnix(createdAt)

	return message, nil
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeMessageID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if !strings.HasPrefix(value, "<") {
		value = "<" + value
	}

	if !strings.HasSuffix(value, ">") {
		value += ">"
	}

	return value
}

func toUnix(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}

	return value.UTC().Unix()
}

func fromUnix(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}

	return time.Unix(value, 0).UTC()
}

func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected rows: %w", err)
	}

	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

func isUniqueConstraint(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
