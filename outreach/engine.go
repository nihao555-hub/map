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
	aiWriteTimeout      = 75 * time.Second
	sendFailureBackoff  = 15 * time.Minute
	// mailboxErrorHold pauses all outbound after a sender-account-level SMTP
	// failure (blocked relay, broken auth): every recipient would fail, so
	// retrying per contact only burns sequences and reputation.
	mailboxErrorHold = 30 * time.Minute

	metaInboxUID       = "imap_last_uid"
	metaInboxLastPoll  = "imap_last_poll_unix"
	metaNextOutbound   = "next_outbound_unix"
	metaOutboundHold   = "outbound_hold_until"
	metaMailboxError   = "mailbox_error"
	metaMailboxErrorAt = "mailbox_error_at_unix"
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
	AutoReplies  int    `json:"auto_replies"`
	Escalated    int    `json:"escalated"`
	State        string `json:"state"`
}

// autoReplyCap is the maximum number of autopilot replies per thread before it
// is escalated to a human, preventing an AI-to-AI loop.
const autoReplyCap = 1

// highStakesSignals force a human handoff: money, commitments, complaints and
// legal are never auto-answered by the AI.
var highStakesSignals = []string{
	"price", "prices", "pricing", "quote", "quotation", "cost", "moq",
	"discount", "contract", "agreement", "invoice", "payment", "refund",
	"warranty", "lawyer", "legal", "complaint", "claim", "lawsuit",
	"报价", "价格", "多少钱", "折扣", "合同", "协议", "发票", "付款",
	"退款", "投诉", "律师", "保修", "赔偿", "起诉",
}

// Engine executes the idempotent outreach loop.
//
// A Tick does at most one SMTP send. This makes the persisted randomized gap
// authoritative even across restarts and prevents accidental bursts when many
// contacts become due at the same time.
type Engine struct {
	store      *Store
	sender     MailSender
	inbox      Inbox
	ai         AICompleter
	researcher Researcher
	now        func() time.Time

	mu sync.Mutex
}

// NewEngine constructs an outreach engine. Nil collaborators are replaced by
// the production implementations.
func NewEngine(store *Store, sender MailSender, inbox Inbox) *Engine {
	if sender == nil {
		sender = &SMTPMailer{}
	}

	if inbox == nil {
		inbox = &IMAPInbox{}
	}

	return &Engine{
		store:      store,
		sender:     sender,
		inbox:      inbox,
		ai:         NewAIClient(),
		researcher: NewWebsiteResearcher(),
		now:        time.Now,
	}
}

// SetAI overrides the AI completer (used by tests).
func (e *Engine) SetAI(ai AICompleter) {
	e.ai = ai
}

// SetResearcher overrides the website researcher (used by tests).
func (e *Engine) SetResearcher(researcher Researcher) {
	e.researcher = researcher
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

	report, err := e.syncInboxIfDue(ctx, &settings, now)
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

	holdUntil, err := e.metaTime(ctx, metaOutboundHold)
	if err != nil {
		return report, err
	}

	if now.Before(holdUntil) {
		report.State = "outbound paused: mailbox-level SMTP error (retry " +
			holdUntil.UTC().Format(time.RFC3339) + ")"

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

	allowed, sent, err := e.dailyAllowance(ctx, &settings, now)
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
			jittered := NextSendTimeJittered(now, location, settings.Window())
			if err := e.store.RescheduleContact(ctx, contact.ID, jittered); err != nil {
				return report, err
			}

			continue
		}

		sentMessage, err := e.sendSequenceStep(ctx, &settings, contact, now)
		if err != nil {
			if IsMailboxLevelError(err) {
				// The sender account itself is broken (e.g. "relay access
				// denied"): keep the contact scheduled and pause all
				// outbound instead of failing every lead in the batch.
				if holdErr := e.holdOutbound(ctx, now, err); holdErr != nil {
					return report, errors.Join(err, holdErr)
				}

				report.State = "outbound paused: mailbox-level SMTP error"

				return report, fmt.Errorf("mailbox-level SMTP error, outbound paused %s: %w",
					mailboxErrorHold, err)
			}

			retryAt := now.Add(time.Duration(contact.SendFailures+1) * sendFailureBackoff)
			if recordErr := e.store.RecordSendFailure(ctx, contact.ID, retryAt); recordErr != nil {
				return report, errors.Join(err, recordErr)
			}

			return report, err
		}

		if !sentMessage {
			continue
		}

		if err := e.clearMailboxError(ctx); err != nil {
			return report, err
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
	settings *Settings,
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

	subject, body := e.composeStep(ctx, settings, &campaign, contact)

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

	if err := e.sender.Send(ctx, settings, &message); err != nil {
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

// composeStep produces the subject and body for the contact's next step. With
// AI configured it writes a fresh personalized email from the scraped facts
// and website research; any AI failure falls back to the campaign templates
// so the sequence never stalls.
func (e *Engine) composeStep(
	ctx context.Context,
	settings *Settings,
	campaign *Campaign,
	contact *Contact,
) (subject, body string) {
	if settings.AIConfigured() {
		subject, body, err := e.composeStepAI(ctx, settings, campaign, contact)
		if err == nil {
			return subject, body
		}

		log.Printf("outreach AI writer fallback for contact %d step %d: %v", contact.ID, contact.NextStep, err)
	}

	step := &campaign.Sequence[contact.NextStep]
	renderContext := NewRenderContext(contact, settings, campaign)

	subject, body, err := RenderStep(step, &renderContext, contact.RootSubject)
	if err != nil {
		// Templates were validated at campaign creation; render errors here
		// mean template variables misbehaved. Fail visibly in the body
		// rather than sending an empty email.
		log.Printf("outreach template render error for contact %d: %v", contact.ID, err)

		return "Quick question about " + contact.Name, "Hi " + contact.Name + " team,\n\n" +
			campaign.ValueProposition + ".\n\n" + campaign.CallToAction + "\n\n" + signatureFor(settings)
	}

	return subject, body
}

func (e *Engine) composeStepAI(
	ctx context.Context,
	settings *Settings,
	campaign *Campaign,
	contact *Contact,
) (subject, body string, err error) {
	research := e.ensureResearch(ctx, contact)

	thread, err := e.store.ContactMessages(ctx, contact.ID, 20)
	if err != nil {
		return "", "", err
	}

	aiCtx, cancel := context.WithTimeout(ctx, aiWriteTimeout)
	defer cancel()

	facts := EmailFacts{
		Contact:  contact,
		Campaign: campaign,
		Settings: settings,
		Research: research,
		Thread:   thread,
	}

	email, err := GenerateStepEmail(aiCtx, e.ai, &facts, contact.NextStep)
	if err != nil {
		return "", "", err
	}

	subject = email.Subject
	if contact.NextStep > 0 || subject == "" {
		subject = contact.RootSubject
		if subject == "" {
			subject = email.Subject
		}

		if contact.NextStep > 0 && !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}
	}

	if subject == "" {
		return "", "", errors.New("AI produced no usable subject")
	}

	return subject, email.Body, nil
}

// ensureResearch returns cached website research, refreshing it when stale.
// Research failures are logged and ignored: a missing summary only reduces
// personalization quality.
func (e *Engine) ensureResearch(ctx context.Context, contact *Contact) string {
	if contact.Website == "" || e.researcher == nil {
		return contact.Research
	}

	fresh := !contact.ResearchAt.IsZero() && e.now().UTC().Sub(contact.ResearchAt) < researchTTL
	if contact.Research != "" && fresh {
		return contact.Research
	}

	summary, err := e.researcher.Research(ctx, contact.Website)
	if err != nil || summary == "" {
		if err != nil {
			log.Printf("outreach research failed for %s (%s): %v", contact.Name, contact.Website, err)
		}

		return contact.Research
	}

	if err := e.store.SaveContactResearch(ctx, contact.ID, summary); err != nil {
		log.Printf("outreach research cache failed for contact %d: %v", contact.ID, err)
	}

	contact.Research = summary
	contact.ResearchAt = e.now().UTC()

	return summary
}

func (e *Engine) syncInboxIfDue(
	ctx context.Context,
	settings *Settings,
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

	cursor := parseInboxCursor(cursorValue)

	inbound, nextCursor, err := e.inbox.Poll(ctx, settings, cursor)
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
		case InboundKindNotice:
			// Transient delivery notice: recorded, but not an actionable reply.
		default:
			intent := e.assessIntent(ctx, settings, &contact, &message)
			report.Replies++

			if err := e.handleInboundReply(ctx, settings, &contact, &message, intent, &report); err != nil {
				return report, err
			}
		}
	}

	if nextCursor != cursor {
		if err := e.store.SetMeta(ctx, metaInboxUID, formatInboxCursor(nextCursor)); err != nil {
			return report, err
		}
	}

	if err := e.store.SetMeta(ctx, metaInboxLastPoll, strconv.FormatInt(now.Unix(), 10)); err != nil {
		return report, err
	}

	return report, nil
}

// parseInboxCursor decodes a "uidvalidity:uid" meta value. A legacy bare UID
// or unpardsable value yields validity 0, which forces one safe recent-window
// rescan on the next poll.
func parseInboxCursor(value string) InboxCursor {
	if value == "" {
		return InboxCursor{}
	}

	var cursor InboxCursor

	if validity, uid, ok := strings.Cut(value, ":"); ok {
		if v, err := strconv.ParseUint(validity, 10, 32); err == nil {
			cursor.UIDValidity = uint32(v)
		}

		if u, err := strconv.ParseUint(uid, 10, 32); err == nil {
			cursor.LastUID = uint32(u)
		}
	}

	return cursor
}

func formatInboxCursor(cursor InboxCursor) string {
	return strconv.FormatUint(uint64(cursor.UIDValidity), 10) + ":" +
		strconv.FormatUint(uint64(cursor.LastUID), 10)
}

// assessIntent scores the contact from the inbound message: heuristics first,
// then the AI classifier when configured. Intent problems never fail the
// inbound pipeline.
func (e *Engine) assessIntent(
	ctx context.Context,
	settings *Settings,
	contact *Contact,
	message *Message,
) Intent {
	intent := HeuristicIntent(message.Kind, message.Subject, message.Body)

	if message.Kind == InboundKindReply && settings.AIConfigured() {
		aiCtx, cancel := context.WithTimeout(ctx, aiWriteTimeout)

		aiIntent, err := ClassifyIntentAI(aiCtx, e.ai, settings, message)

		cancel()

		if err == nil {
			intent = aiIntent
		} else {
			log.Printf("outreach AI intent fallback for contact %d: %v", contact.ID, err)
		}
	}

	if err := e.store.UpdateContactIntent(ctx, contact.ID, intent); err != nil {
		log.Printf("outreach intent update failed for contact %d: %v", contact.ID, err)
	}

	return intent
}

// handleInboundReply applies the autonomy policy after a customer reply:
// autopilot auto-answers safe, non-committal replies and escalates everything
// involving money/commitment/complaint/legal or a hot lead to the human queue.
// With autopilot off (or AI unavailable), every reply is escalated for review.
func (e *Engine) handleInboundReply(
	ctx context.Context,
	settings *Settings,
	contact *Contact,
	inbound *Message,
	intent Intent,
	report *TickReport,
) error {
	escalate := func(reason string) error {
		report.Escalated++

		return e.store.SetAttention(ctx, contact.ID, reason)
	}

	if !settings.AutoReply || !settings.AIConfigured() {
		return escalate("客户已回复，待人工处理")
	}

	auto, reason := routeInbound(intent, inbound.Body)
	if !auto {
		return escalate(reason)
	}

	count, err := e.store.AutoReplyCount(ctx, contact.ID)
	if err != nil {
		return err
	}

	if count >= autoReplyCap {
		return escalate("AI 已自动回复过一次，转人工继续跟进")
	}

	if err := e.autoReply(ctx, settings, contact, inbound); err != nil {
		log.Printf("outreach autopilot reply failed for contact %d: %v", contact.ID, err)

		return escalate("AI 自动回信失败，转人工处理")
	}

	report.AutoReplies++

	return e.store.ClearAttention(ctx, contact.ID)
}

// routeInbound decides whether autopilot may answer a reply automatically.
func routeInbound(intent Intent, body string) (auto bool, reason string) {
	text := strings.ToLower(unquotedReply(body))
	if containsAny(text, highStakesSignals) {
		return false, "涉及报价/合同/付款/投诉等，需人工把关"
	}

	switch intent.Label {
	case IntentHigh:
		return false, "高意向客户，建议人工亲自跟进促成"
	case IntentNone:
		return false, "客户表达消极/无意向，待人工确认"
	case IntentInvalid:
		return false, "地址或投递异常，待人工确认"
	}

	if intent.Score >= 80 {
		return false, "购买信号较强，建议人工跟进"
	}

	return true, ""
}

// autoReply drafts a constrained reply with the AI and sends it in-thread.
func (e *Engine) autoReply(
	ctx context.Context,
	settings *Settings,
	contact *Contact,
	inbound *Message,
) error {
	campaign, err := e.store.Campaign(ctx, contact.CampaignID)
	if err != nil {
		return err
	}

	thread, err := e.store.ContactMessages(ctx, contact.ID, 20)
	if err != nil {
		return err
	}

	aiCtx, cancel := context.WithTimeout(ctx, aiWriteTimeout)
	defer cancel()

	facts := EmailFacts{
		Contact:  contact,
		Campaign: &campaign,
		Settings: settings,
		Research: contact.Research,
		Thread:   thread,
	}

	draft, err := SuggestReply(aiCtx, e.ai, &facts)
	if err != nil {
		return err
	}

	subject := contact.RootSubject
	if subject == "" {
		subject = inbound.Subject
	}

	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}

	now := e.now().UTC()
	message := Message{
		CampaignID: contact.CampaignID,
		ContactID:  contact.ID,
		Direction:  DirectionOut,
		Kind:       KindAutoReply,
		Step:       -2,
		Subject:    subject,
		Body:       draft,
		MessageID:  NewMessageID(settings.EmailAddress),
		InReplyTo:  inbound.MessageID,
		FromEmail:  settings.EmailAddress,
		ToEmail:    contact.Email,
		CreatedAt:  now,
	}

	if err := e.sender.Send(ctx, settings, &message); err != nil {
		return err
	}

	return e.store.RecordManualMessage(ctx, &message)
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
	settings *Settings,
	now time.Time,
) (allowed, sent int, err error) {
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

// holdOutbound pauses sending for mailboxErrorHold and records the error so
// the overview can surface a "sender mailbox is blocked" warning.
func (e *Engine) holdOutbound(ctx context.Context, now time.Time, cause error) error {
	until := strconv.FormatInt(now.Add(mailboxErrorHold).Unix(), 10)
	if err := e.store.SetMeta(ctx, metaOutboundHold, until); err != nil {
		return err
	}

	if err := e.store.SetMeta(ctx, metaMailboxError, cause.Error()); err != nil {
		return err
	}

	return e.store.SetMeta(ctx, metaMailboxErrorAt, strconv.FormatInt(now.Unix(), 10))
}

// clearMailboxError removes the outbound hold after a successful send.
func (e *Engine) clearMailboxError(ctx context.Context) error {
	if err := e.store.SetMeta(ctx, metaOutboundHold, ""); err != nil {
		return err
	}

	return e.store.SetMeta(ctx, metaMailboxError, "")
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
// A full pass (including the external relay probe) also clears a recorded
// mailbox error, so the overview warning disappears as soon as the operator
// confirms the account is unblocked.
func (e *Engine) TestConnections(ctx context.Context) error {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return err
	}

	if err := e.sender.Test(ctx, &settings); err != nil {
		return err
	}

	if err := e.inbox.Test(ctx, &settings); err != nil {
		return err
	}

	return e.clearMailboxError(ctx)
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

	suppressed, err := e.store.IsSuppressed(ctx, contact.Email)
	if err != nil {
		return Message{}, err
	}

	if suppressed {
		return Message{}, errors.New("this address is on the suppression list (bounced or unsubscribed)")
	}

	settings, err := e.store.Settings(ctx)
	if err != nil {
		return Message{}, err
	}

	if !settings.SMTPConfigured() {
		return Message{}, errors.New("SMTP is not configured")
	}

	subject := contact.RootSubject
	if subject == "" {
		subject = "Regarding " + contact.Name
	}

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

	if err := e.sender.Send(ctx, &settings, &message); err != nil {
		return Message{}, err
	}

	if err := e.store.RecordManualMessage(ctx, &message); err != nil {
		return Message{}, fmt.Errorf("reply sent but local state update failed: %w", err)
	}

	// A human handled the thread: clear it from the attention queue.
	if err := e.store.ClearAttention(ctx, contact.ID); err != nil {
		log.Printf("outreach clear attention after manual reply failed for contact %d: %v", contact.ID, err)
	}

	return message, nil
}

// EvaluateContact scores the contact's most recent outbound email with the AI
// and caches the result, so repeated views don't re-call the model.
func (e *Engine) EvaluateContact(ctx context.Context, contactID int64) (Evaluation, Message, error) {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return Evaluation{}, Message{}, err
	}

	if !settings.AIConfigured() {
		return Evaluation{}, Message{}, errors.New("AI 未配置：请在设置中填写 AI 接口地址、模型和 OUTREACH_AI_API_KEY")
	}

	message, err := e.store.LatestOutbound(ctx, contactID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Evaluation{}, Message{}, errors.New("该客户还没有已发送的开发信，无法评估")
		}

		return Evaluation{}, Message{}, err
	}

	cacheKey := evaluationKey(&message)

	if cached, ok, err := e.store.Evaluation(ctx, cacheKey); err != nil {
		return Evaluation{}, Message{}, err
	} else if ok {
		return cached, message, nil
	}

	contact, err := e.store.Contact(ctx, contactID)
	if err != nil {
		return Evaluation{}, Message{}, err
	}

	aiCtx, cancel := context.WithTimeout(ctx, aiWriteTimeout)
	defer cancel()

	evaluation, err := EvaluateEmail(aiCtx, e.ai, &settings, &contact, message.Subject, message.Body)
	if err != nil {
		return Evaluation{}, Message{}, err
	}

	if err := e.store.SaveEvaluation(ctx, cacheKey, evaluation); err != nil {
		return Evaluation{}, Message{}, err
	}

	return evaluation, message, nil
}

func evaluationKey(message *Message) string {
	if message.MessageID != "" {
		return message.MessageID
	}

	return "msg-" + strconv.FormatInt(message.ID, 10)
}

// SuggestReplyForContact drafts an answer to the contact's latest reply for
// human review. It requires the AI writer to be configured.
func (e *Engine) SuggestReplyForContact(ctx context.Context, contactID int64) (string, error) {
	settings, err := e.store.Settings(ctx)
	if err != nil {
		return "", err
	}

	if !settings.AIConfigured() {
		return "", errors.New("AI 未配置：请在设置中填写 AI 接口地址、模型和 OUTREACH_AI_API_KEY")
	}

	contact, err := e.store.Contact(ctx, contactID)
	if err != nil {
		return "", err
	}

	campaign, err := e.store.Campaign(ctx, contact.CampaignID)
	if err != nil {
		return "", err
	}

	thread, err := e.store.ContactMessages(ctx, contact.ID, 20)
	if err != nil {
		return "", err
	}

	aiCtx, cancel := context.WithTimeout(ctx, aiWriteTimeout)
	defer cancel()

	facts := EmailFacts{
		Contact:  &contact,
		Campaign: &campaign,
		Settings: &settings,
		Research: contact.Research,
		Thread:   thread,
	}

	return SuggestReply(aiCtx, e.ai, &facts)
}
