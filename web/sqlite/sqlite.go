package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite driver

	"github.com/gosom/google-maps-scraper/web"
)

// Store is the SQLite-backed job + invite repository.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database at path.
func New(path string) (*Store, error) {
	db, err := initDatabase(path)
	if err != nil {
		return nil, err
	}

	return &Store{db: db}, nil
}

func (s *Store) Get(ctx context.Context, id string) (web.Job, error) {
	const q = `SELECT * from jobs WHERE id = ?`

	row := s.db.QueryRowContext(ctx, q, id)

	return rowToJob(row)
}

func (s *Store) Create(ctx context.Context, job *web.Job) error {
	item, err := jobToRow(job)
	if err != nil {
		return err
	}

	const q = `INSERT INTO jobs (id, name, status, data, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`

	_, err = s.db.ExecContext(ctx, q, item.ID, item.Name, item.Status, item.Data, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return err
	}

	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM jobs WHERE id = ?`

	_, err := s.db.ExecContext(ctx, q, id)

	return err
}

func (s *Store) Select(ctx context.Context, params web.SelectParams) ([]web.Job, error) {
	q := `SELECT * from jobs`

	var args []any

	if params.Status != "" {
		q += ` WHERE status = ?`

		args = append(args, params.Status)
	}

	q += " ORDER BY created_at DESC"

	if params.Limit > 0 {
		q += " LIMIT ?"

		args = append(args, params.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var ans []web.Job

	for rows.Next() {
		job, err := rowToJob(rows)
		if err != nil {
			return nil, err
		}

		ans = append(ans, job)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ans, nil
}

func (s *Store) Update(ctx context.Context, job *web.Job) error {
	item, err := jobToRow(job)
	if err != nil {
		return err
	}

	const q = `UPDATE jobs SET name = ?, status = ?, data = ?, updated_at = ? WHERE id = ?`

	_, err = s.db.ExecContext(ctx, q, item.Name, item.Status, item.Data, item.UpdatedAt, item.ID)

	return err
}

// ClaimPending atomically takes the oldest pending job.
func (s *Store) ClaimPending(ctx context.Context) (web.Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return web.Job{}, err
	}
	defer func() { _ = tx.Rollback() }()

	const sel = `SELECT id FROM jobs WHERE status = ? ORDER BY created_at ASC LIMIT 1`

	var id string
	err = tx.QueryRowContext(ctx, sel, web.StatusPending).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return web.Job{}, web.ErrNoPending
	}
	if err != nil {
		return web.Job{}, err
	}

	now := time.Now().UTC().Unix()
	res, err := tx.ExecContext(ctx,
		`UPDATE jobs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		web.StatusWorking, now, id, web.StatusPending,
	)
	if err != nil {
		return web.Job{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return web.Job{}, err
	}
	if n == 0 {
		return web.Job{}, web.ErrNoPending
	}

	row := tx.QueryRowContext(ctx, `SELECT * FROM jobs WHERE id = ?`, id)
	job, err := rowToJob(row)
	if err != nil {
		return web.Job{}, err
	}

	if err := tx.Commit(); err != nil {
		return web.Job{}, err
	}

	job.Status = web.StatusWorking
	return job, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func rowToJob(row scannable) (web.Job, error) {
	var j job

	err := row.Scan(&j.ID, &j.Name, &j.Status, &j.Data, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return web.Job{}, err
	}

	ans := web.Job{
		ID:     j.ID,
		Name:   j.Name,
		Status: j.Status,
		Date:   time.Unix(j.CreatedAt, 0).UTC(),
	}

	err = json.Unmarshal([]byte(j.Data), &ans.Data)
	if err != nil {
		return web.Job{}, err
	}

	return ans, nil
}

func jobToRow(item *web.Job) (job, error) {
	data, err := json.Marshal(item.Data)
	if err != nil {
		return job{}, err
	}

	return job{
		ID:        item.ID,
		Name:      item.Name,
		Status:    item.Status,
		Data:      string(data),
		CreatedAt: item.Date.Unix(),
		UpdatedAt: time.Now().UTC().Unix(),
	}, nil
}

type job struct {
	ID        string
	Name      string
	Status    string
	Data      string
	CreatedAt int64
	UpdatedAt int64
}

func initDatabase(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)

	_, err = db.Exec("PRAGMA busy_timeout = 5000")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("PRAGMA synchronous=NORMAL")
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("PRAGMA cache_size=1000")
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	return db, createSchema(db)
}

func createSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			status TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at INT NOT NULL,
			updated_at INT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS invite_codes (
			code TEXT PRIMARY KEY,
			created_at INT NOT NULL,
			used_at INT,
			session_id TEXT
		);
		CREATE TABLE IF NOT EXISTS invite_sessions (
			token TEXT PRIMARY KEY,
			invite_code TEXT NOT NULL,
			created_at INT NOT NULL,
			expires_at INT NOT NULL,
			last_seen_at INT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_jobs_status_created ON jobs(status, created_at);
		CREATE INDEX IF NOT EXISTS idx_invite_sessions_expires ON invite_sessions(expires_at);
	`)

	return err
}

// EnsureSeed creates invite codes until at least count exist. Returns newly created codes.
func (s *Store) EnsureSeed(ctx context.Context, count int) ([]string, error) {
	if count <= 0 {
		return nil, nil
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM invite_codes`).Scan(&total); err != nil {
		return nil, err
	}

	need := count - total
	if need <= 0 {
		return nil, nil
	}

	created := make([]string, 0, need)
	now := time.Now().UTC().Unix()

	for len(created) < need {
		code, err := web.GenerateInviteCode()
		if err != nil {
			return created, err
		}
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO invite_codes (code, created_at) VALUES (?, ?)`,
			code, now,
		)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "constraint") {
				continue
			}
			return created, err
		}
		created = append(created, code)
	}

	return created, nil
}

// Redeem marks a code as used and creates a session token.
func (s *Store) Redeem(ctx context.Context, code string) (string, time.Time, error) {
	code = web.NormalizeInviteCode(code)
	if code == "" || !strings.HasPrefix(code, "GMS-") {
		return "", time.Time{}, web.ErrInvalidInvite
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var usedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT used_at FROM invite_codes WHERE code = ?`, code).Scan(&usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, web.ErrInvalidInvite
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if usedAt.Valid {
		return "", time.Time{}, web.ErrInvalidInvite
	}

	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}

	now := time.Now().UTC()
	expires := now.Add(web.InviteSessionTTL)

	res, err := tx.ExecContext(ctx,
		`UPDATE invite_codes SET used_at = ?, session_id = ? WHERE code = ? AND used_at IS NULL`,
		now.Unix(), token, code,
	)
	if err != nil {
		return "", time.Time{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", time.Time{}, err
	}
	if n == 0 {
		return "", time.Time{}, web.ErrInvalidInvite
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO invite_sessions (token, invite_code, created_at, expires_at, last_seen_at) VALUES (?, ?, ?, ?, ?)`,
		token, code, now.Unix(), expires.Unix(), now.Unix(),
	)
	if err != nil {
		return "", time.Time{}, err
	}

	if err := tx.Commit(); err != nil {
		return "", time.Time{}, err
	}

	return token, expires, nil
}

// ValidSession checks the session token and refreshes last_seen_at.
func (s *Store) ValidSession(ctx context.Context, token string) (bool, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return false, nil
	}

	now := time.Now().UTC().Unix()
	var expires int64
	err := s.db.QueryRowContext(ctx,
		`SELECT expires_at FROM invite_sessions WHERE token = ?`, token,
	).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if expires < now {
		return false, nil
	}

	_, _ = s.db.ExecContext(ctx,
		`UPDATE invite_sessions SET last_seen_at = ? WHERE token = ?`, now, token,
	)
	return true, nil
}

// Stats returns invite code counts.
func (s *Store) Stats(ctx context.Context) (total, used, unused int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM invite_codes`).Scan(&total)
	if err != nil {
		return 0, 0, 0, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM invite_codes WHERE used_at IS NOT NULL`).Scan(&used)
	if err != nil {
		return 0, 0, 0, err
	}
	unused = total - used
	return total, used, unused, nil
}

// ListAll returns all invite codes ordered by creation time.
func (s *Store) ListAll(ctx context.Context) ([]web.InviteCode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT code, created_at, used_at, COALESCE(session_id, '') FROM invite_codes ORDER BY created_at ASC, code ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []web.InviteCode
	for rows.Next() {
		var (
			code      string
			createdAt int64
			usedAt    sql.NullInt64
			sessionID string
		)
		if err := rows.Scan(&code, &createdAt, &usedAt, &sessionID); err != nil {
			return nil, err
		}
		item := web.InviteCode{
			Code:      code,
			CreatedAt: time.Unix(createdAt, 0).UTC(),
			SessionID: sessionID,
		}
		if usedAt.Valid {
			t := time.Unix(usedAt.Int64, 0).UTC()
			item.UsedAt = &t
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ExportFile writes a human-readable invite code list.
func (s *Store) ExportFile(ctx context.Context, path string) error {
	codes, err := s.ListAll(ctx)
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# 地图获客 · 邀请码清单\n")
	b.WriteString("# 格式: CODE\\tSTATUS\\tUSED_AT\n")
	b.WriteString(fmt.Sprintf("# generated_at=%s total=%d\n", time.Now().UTC().Format(time.RFC3339), len(codes)))
	for _, c := range codes {
		status := "unused"
		used := ""
		if c.UsedAt != nil {
			status = "used"
			used = c.UsedAt.Format(time.RFC3339)
		}
		b.WriteString(fmt.Sprintf("%s\t%s\t%s\n", c.Code, status, used))
	}

	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
