package outreach

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultTickInterval = 30 * time.Second
	inboxPollInterval   = 2 * time.Minute

	metaInboxUID      = "imap_last_uid"
	metaInboxLastPoll = "imap_last_poll_unix"
	metaNextOutbound  = "next_outbound_unix"
)

var emailInTextPattern = regexp.MustCompile(
	`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+`,
)

// TickReport summarizes one idempotent engine cycle.
type TickReport struct {
	InboxChecked int    `json:"inbox_checked"`
	Replies      int    `json:"replies"`
	Bounces      int    `json:"bounces"`
	Unsubscribed int    `json:"unsubscribed"`
	Sent         int    `json:"sent"`
	State        string `json:"state"`
}

// Engine executes the idempotent outreach loop.
//
// A Tick does at most one SMTP send. This makes the persisted randomized gap
// authoritative even across restarts and prevents accidental bursts when many
// contacts become due at the same time.
type Engine struct {
	store  *Store
	sender MailSender
	inbox  Inbox
	now    func() time.Time

	mu sync.Mutex
}

// NewEngine constructs an outreach engine.
func NewEngine(store *Store, sender MailSender, inbox Inbox) *Engine {
	if sender == nil {
		sender = &SMTPMailer{}
	}

	if inbox == nil {
		inbox = &IMAPInbox{}
	}

	return &Engine{
		store:  store,
		sender: sender,
		inbox:  inbox,
		now:    time.Now,
	}
}

// Run polls replies and sends due messages until the context is cancelled.
func (e *Engine) Run(ctx context.Context) error {
	ticker := time.NewTicker(defaultTickInterval)
	defer ticker.Stop()

	if _, err := e.Tick(ctx); err != nil {
		log.Printf("outreach initial tick: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := e.Tick(ctx); err != nil {
				log.Printf("outreach tick: %v", err)
			}
		}
	}
}

// Tick is safe to call from both the background loop and the HTTP API.
func (e *Engine) Tick(ctx context.Context) (TickReport, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now().UTC()

	settings, err := e.store.Settings(ctx)
	if err != nil {
		return TickReport{}, err
	}

	report, err := e.syncInboxIfDue(ctx, settings, now)
	if err != nil {
		return report, err
	}

	if strings.EqualFold(os.Getenv(EnvDisabled), "1") ||
		strings.EqualFold(os.Getenv(EnvDisabled), "true") {
		report.State = "sending disabled by OUTREACH_DISABLED"

		return report, nil
	}

	if !settings.SMTPConfigured() {
		report.State = "SMTP not configured"

		return report, nil
	}

	nextOutbound, err := e.metaTime(ctx, metaNextOutbound)
	if err != nil {
		return report, err
	}

	if now.Before(nextOutbound) {
		report.State = "waiting for randomized send gap"

		return report, nil
	}

	allowed, sent, err := e.dailyAllowance(ctx, settings, now)
	if err != nil {
		return report, err
	}

	if sent >= allowed {
		report.State = fmt.Sprintf("daily cap reached (%d/%d)", sent, allowed)

		return report, nil
	}

	contacts, err := e.store.DueContacts(ctx, now, 25)
	if err != nil {
		return report, err
	}

	for i := range contacts {
		contact := &contacts[i]
		location := LoadLocation(contact.Timezone, settings.DefaultTimezone)
		nextWindow := NextSendTime(now, location, settings.Window())

		if nextWindow.After(now.Add(time.Second)) {
			if err := e.store.RescheduleContact(ctx, contact.ID, NextSendTimeJittered(now, location, settings.Window())); err != nil {
				return report, err
			}

			continue
		}

		sentMessage, err := e.sendSequenceStep(ctx, settings, contact, now)
		if err != nil {
			retryAt := now.Add(time.Duration(contact.SendFailures+1) * 15 * time.Minute)
			if recordErr := e.store.RecordSendFailure(ctx, contact.ID, retryAt); recordErr != nil {
				return report, errors.Join(err, recordErr)
			}

			return report, err
		}

		if !sentMessage {
			continue
		}

		if err := e.store.SetMeta(
			ctx,
			metaNextOutbound,
			strconv.FormatInt(now.Add(settings.SendGap()).Unix(), 10),
		); err != nil {
			return report, err
		}

		report.Sent = 1
		report.State = "sent"

		return report, nil
	}

	report.State = "no messages due"

	return report, nil
}

func (e *Engine) sendSequenceStep(
	ctx context.Context,
	settings Settings,
	contact *Contact,
	now time.Time,
) (bool, error) {
	campaign, err := e.store.Campaign(ctx, contact.CampaignID)
	if err != nil {
		return false, err
	}

	if contact.NextStep < 0 || contact.NextStep >= len(campaign.Sequence) {
		return false, e.store.CompleteContact(ctx, contact.ID)
	}

	step := &campaign.Sequence[contact.NextStep]
	renderContext := NewRenderContext(contact, &settings, &campaign)

	subject, body, err := RenderStep(step, &renderContext, contact.RootSubject)
	if err != nil {
		return false, err
	}

	message := Message{
		CampaignID: campaign.ID,
		ContactID:  contact.ID,
		Direction:  DirectionOut,
		Step:       contact.NextStep,
		Subject:    subject,
		Body:       body,
		MessageID:  NewMessageID(settings.EmailAddress),
		InReplyTo:  contact.LastMessageID,
		FromEmail:  settings.EmailAddress,
		ToEmail:    contact.Email,
		CreatedAt:  now,
	}

	if err := e.sender.Send(ctx, settings, message); err != nil {
		return false, err
	}

	completed := contact.NextStep+1 >= len(campaign.Sequence)
	var nextSendAt time.Time

	if !completed {
		nextStep := campaign.Sequence[contact.NextStep+1]
		after := now.AddDate(0, 0, nextStep.DelayDays)
		location := LoadLocation(contact.Timezone, settings.DefaultTimezone)
		nextSendAt = NextSendTimeJittered(after, location, settings.Window())
	}

	if err := e.store.RecordSent(ctx, contact, &message, nextSendAt, completed); err != nil {
		// SMTP has already accepted the message. Returning the error is safer
		// than retrying immediately; the stable Message-ID lets IMAP/recovery
		// identify a duplicate if the operator reconciles it.
		return false, fmt.Errorf("message sent but local state update failed: %w", err)
	}

	return true, nil
}

func (e *Engine) syncInboxIfDue(
	ctx context.Context,
	settings Settings,
	now time.Time,
) (TickReport, error) {
	report := TickReport{}

	if !settings.IMAPConfigured() {
		return report, nil
	}

	lastPoll, err := e.metaTime(ctx, metaInboxLastPoll)
	if err != nil {
		return report, err
	}

	if !lastPoll.IsZero() && now.Sub(lastPoll) < inboxPollInterval {
		return report, nil
	}

	cursorValue, err := e.store.Meta(ctx, metaInboxUID)
	if err != nil {
		return report, err
	}

	cursor, _ := strconv.ParseUint(cursorValue, 10, 32)

	inbound, lastUID, err := e.inbox.Poll(ctx, settings, uint32(cursor))
	if err != nil {
		return report, err
	}

	for i := range inbound {
		incoming := &inbound[i]
		report.InboxChecked++

		// Some providers copy sent messages into INBOX. Never treat our own
		// sent copy as a customer response.
		if normalizeEmail(incoming.FromEmail) == normalizeEmail(settings.EmailAddress) {
			continue
		}

		references := append([]string{}, incoming.InReplyTo...)
		references = append(references, incoming.References...)

		contact, err := e.store.FindContactForInbound(ctx, incoming.FromEmail, references)
		if errors.Is(err, ErrNotFound) && ClassifyInbound(incoming) == InboundKindBounce {
			contact, err = e.findBouncedContact(ctx, incoming, settings.EmailAddress)
		}

		if errors.Is(err, ErrNotFound) {
			continue
		}

		if err != nil {
			return report, err
		}

		kind := ClassifyInbound(incoming)
		messageTime := incoming.Date
		if messageTime.IsZero() {
			messageTime = now
		}

		message := Message{
			CampaignID: contact.CampaignID,
			ContactID:  contact.ID,
			Direction:  DirectionIn,
			Kind:       kind,
			Subject:    incoming.Subject,
			Body:       incoming.Body,
			MessageID:  incoming.MessageID,
			FromEmail:  incoming.FromEmail,
			ToEmail:    settings.EmailAddress,
			CreatedAt:  messageTime.UTC(),
		}
		if len(incoming.InReplyTo) > 0 {
			message.InReplyTo = incoming.InReplyTo[0]
		}

		if err := e.store.RecordInbound(ctx, &contact, &message); err != nil {
			return report, err
		}

		switch kind {
		case InboundKindBounce:
			report.Bounces++
		case InboundKindUnsubscribe:
			report.Unsubscribed++
		default:
			report.Replies++
		}
	}

	if lastUID > uint32(cursor) {
		if err := e.store.SetMeta(ctx, metaInboxUID, strconv.FormatUint(uint64(lastUID), 10)); err != nil {
			return report, err
		}
	}

	if err := e.store.SetMeta(ctx, metaInboxLastPoll, strconv.FormatInt(now.Unix(), 10)); err != nil {
		return report, err
	}

	return report, nil
}

func (e *Engine) findBouncedContact(
	ctx context.Context,
	message *InboundEmail,
	senderEmail string,
) (Contact, error) {
	for _, email := range emailInTextPattern.FindAllString(message.Body, -1) {
		if normalizeEmail(email) == normalizeEmail(senderEmail) {
			continue
		}

		contact, err := e.store.FindContactForInbound(ctx, email, nil)
		if err == nil {
			return contact, nil
		}

		if !errors.Is(err, ErrNotFound) {
			return Contact{}, err
		}
	}

	return Contact{}, ErrNotFound
}

func (e *Engine) dailyAllowance(
	ctx context.Context,
	settings Settings,
	now time.Time,
) (allowed int, sent int, err error) {
	location := LoadLocation(settings.DefaultTimezone, "UTC")
	localNow := now.In(location)
	startLocal := time.Date(
		localNow.Year(),
		localNow.Month(),
		localNow.Day(),
		0,
		0,
		0,
		0,
		location,
	)

	sent, err = e.store.SentToday(ctx, startLocal)
	if err != nil {
		return 0, 0, err
	}

	firstSent, err := e.store.FirstSentAt(ctx)
	if err != nil {
		return 0, 0, err
	}

	daysActive := 0
	if !firstSent.IsZero() {
		daysActive = int(now.Sub(firstSent).Hours() / 24)
		if daysActive < 0 {
			daysActive = 0
		}
	}

	return settings.AllowedToday(daysActive), sent, nil
}

func (e *Engine) metaTime(ctx context.Context, key string) (time.Time, error) {
	value, err := e.store.Meta(ctx, key)
	if err != nil || value == "" {
		return time.Time{}, err
	}

	unix, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %s metadata: %w", key, err)
	}

	return fromUnix(unix), nil
}

// TestConnections verifies both SMTP and IMAP without sending any email.
func (e *Engine) TestConnections(ctx context.Context) error {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return err
	}

	if err := e.sender.Test(ctx, settings); err != nil {
		return err
	}

	if err := e.inbox.Test(ctx, settings); err != nil {
		return err
	}

	return nil
}

// Reply sends a human-approved reply in the same thread. Automated sequences
// remain stopped after the customer replied.
func (e *Engine) Reply(ctx context.Context, contactID int64, body string) (Message, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	body = strings.TrimSpace(body)
	if body == "" {
		return Message{}, errors.New("reply body is empty")
	}

	contact, err := e.store.Contact(ctx, contactID)
	if err != nil {
		return Message{}, err
	}

	if contact.Status == ContactStatusBounced || contact.Status == ContactStatusUnsubscribed {
		return Message{}, fmt.Errorf("cannot reply to contact with status %s", contact.Status)
	}

	settings, err := e.store.Settings(ctx)
	if err != nil {
		return Message{}, err
	}

	if !settings.SMTPConfigured() {
		return Message{}, errors.New("SMTP is not configured")
	}

	subject := contact.RootSubject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}

	now := e.now().UTC()
	message := Message{
		CampaignID: contact.CampaignID,
		ContactID:  contact.ID,
		Direction:  DirectionOut,
		Kind:       "manual_reply",
		Step:       -1,
		Subject:    subject,
		Body:       body,
		MessageID:  NewMessageID(settings.EmailAddress),
		InReplyTo:  contact.LastMessageID,
		FromEmail:  settings.EmailAddress,
		ToEmail:    contact.Email,
		CreatedAt:  now,
	}

	if err := e.sender.Send(ctx, settings, message); err != nil {
		return Message{}, err
	}

	if err := e.store.RecordManualMessage(ctx, &message); err != nil {
		return Message{}, fmt.Errorf("reply sent but local state update failed: %w", err)
	}

	return message, nil
}
