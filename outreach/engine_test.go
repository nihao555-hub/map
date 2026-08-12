package outreach_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gosom/google-maps-scraper/outreach"
)

const testMailboxSecret = "in-memory-test-secret"

type fakeSender struct {
	messages []outreach.Message
}

func (f *fakeSender) Send(
	_ context.Context,
	_ *outreach.Settings,
	message *outreach.Message,
) error {
	f.messages = append(f.messages, *message)

	return nil
}

func (f *fakeSender) Test(context.Context, *outreach.Settings) error {
	return nil
}

type emptyInbox struct{}

func (i *emptyInbox) Poll(
	context.Context,
	*outreach.Settings,
	outreach.InboxCursor,
) ([]outreach.InboundEmail, outreach.InboxCursor, error) {
	return nil, outreach.InboxCursor{}, nil
}

func (i *emptyInbox) Test(context.Context, *outreach.Settings) error {
	return nil
}

// queueAI returns responses in order, so a test can script the intent-JSON
// call followed by the reply-text call.
type queueAI struct {
	responses []string
	i         int
}

func (q *queueAI) Complete(context.Context, *outreach.Settings, string, string) (string, error) {
	if q.i >= len(q.responses) {
		return "", errors.New("queueAI exhausted")
	}

	r := q.responses[q.i]
	q.i++

	return r, nil
}

type scriptedInbox struct {
	gotCursor outreach.InboxCursor
	messages  []outreach.InboundEmail
	next      outreach.InboxCursor
}

func (i *scriptedInbox) Poll(
	_ context.Context,
	_ *outreach.Settings,
	cursor outreach.InboxCursor,
) ([]outreach.InboundEmail, outreach.InboxCursor, error) {
	i.gotCursor = cursor

	return i.messages, i.next, nil
}

func (i *scriptedInbox) Test(context.Context, *outreach.Settings) error {
	return nil
}

func TestEngineTickSendsOneDueMessageAndAdvancesSequence(t *testing.T) {
	t.Parallel()

	store, err := outreach.NewStore(filepath.Join(t.TempDir(), "outreach.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})

	ctx := context.Background()
	settings := outreach.DefaultSettings()
	settings.SMTPHost = "smtp.example.com"
	settings.SMTPPort = 465
	settings.EmailAddress = testSenderEmail
	settings.Password = testMailboxSecret
	settings.FromName = testSenderName
	settings.SenderCompany = testSenderCompany
	settings.SendDays = "1234567"
	settings.SendStartHour = 0
	settings.SendEndHour = 24
	settings.DefaultTimezone = "UTC"

	if err := store.SaveSettings(ctx, &settings); err != nil {
		t.Fatal(err)
	}

	campaign := outreach.Campaign{
		ID:               uuid.NewString(),
		Name:             "Test campaign",
		Status:           outreach.CampaignStatusActive,
		ValueProposition: "help clinics turn more web visits into appointments",
		CallToAction:     "Would a quick conversation be useful?",
		Sequence:         outreach.DefaultSequence(),
	}
	if err := store.CreateCampaign(ctx, &campaign); err != nil {
		t.Fatal(err)
	}

	contacts := []outreach.Contact{{
		CampaignID: campaign.ID,
		Email:      "owner@example.org",
		Name:       "Example Dental",
		Category:   "dentist",
		Status:     outreach.ContactStatusActive,
		NextSendAt: time.Now().UTC().Add(-time.Minute),
	}}
	if _, err := store.AddContacts(ctx, contacts); err != nil {
		t.Fatal(err)
	}

	sender := &fakeSender{}
	engine := outreach.NewEngine(store, sender, &emptyInbox{})

	report, err := engine.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if report.Sent != 1 || len(sender.messages) != 1 {
		t.Fatalf("tick did not send one message: report=%+v messages=%d", report, len(sender.messages))
	}

	storedContacts, err := store.Contacts(ctx, campaign.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	if storedContacts[0].NextStep != 1 || storedContacts[0].RootMessageID == "" {
		t.Fatalf("contact did not advance: %+v", storedContacts[0])
	}

	messages, err := store.Messages(ctx, campaign.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(messages) != 1 || messages[0].Direction != outreach.DirectionOut {
		t.Fatalf("outgoing message not persisted: %+v", messages)
	}
}

func TestEngineSyncInboxRecordsReplyAndPersistsCursor(t *testing.T) {
	t.Parallel()

	store, err := outreach.NewStore(filepath.Join(t.TempDir(), "outreach.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})

	ctx := context.Background()
	settings := outreach.DefaultSettings()
	settings.EmailAddress = testSenderEmail
	settings.Password = testMailboxSecret
	settings.IMAPHost = "imap.example.com"
	settings.IMAPPort = 993
	// SMTP intentionally left unconfigured so the tick only exercises inbox sync.

	if err := store.SaveSettings(ctx, &settings); err != nil {
		t.Fatal(err)
	}

	campaign := outreach.Campaign{
		ID:               uuid.NewString(),
		Name:             "Inbox campaign",
		Status:           outreach.CampaignStatusActive,
		ValueProposition: "help clinics grow",
		CallToAction:     "Worth a chat?",
		Sequence:         outreach.DefaultSequence(),
	}
	if err := store.CreateCampaign(ctx, &campaign); err != nil {
		t.Fatal(err)
	}

	if _, err := store.AddContacts(ctx, []outreach.Contact{{
		CampaignID: campaign.ID,
		Email:      "owner@example.org",
		Name:       "Example Dental",
		Status:     outreach.ContactStatusActive,
		NextSendAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}

	contacts, err := store.Contacts(ctx, campaign.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	contact := contacts[0]

	// Simulate an already-sent opener so the inbound reply matches by thread.
	outbound := outreach.Message{
		CampaignID: campaign.ID,
		ContactID:  contact.ID,
		Direction:  outreach.DirectionOut,
		Step:       0,
		Subject:    "Quick question about Example Dental",
		Body:       "opener",
		MessageID:  outreach.NewMessageID(settings.EmailAddress),
		FromEmail:  settings.EmailAddress,
		ToEmail:    contact.Email,
		CreatedAt:  time.Now().UTC(),
	}
	if err := store.RecordSent(ctx, &contact, &outbound, time.Time{}, true); err != nil {
		t.Fatal(err)
	}

	inbox := &scriptedInbox{
		messages: []outreach.InboundEmail{{
			UID:       7,
			MessageID: outreach.NewMessageID(contact.Email),
			InReplyTo: []string{outbound.MessageID},
			FromEmail: contact.Email,
			Subject:   "Re: Quick question about Example Dental",
			Body:      "Please send your price list and MOQ.",
			Date:      time.Now().UTC(),
		}},
		next: outreach.InboxCursor{UIDValidity: 42, LastUID: 7},
	}

	engine := outreach.NewEngine(store, &fakeSender{}, inbox)

	report, err := engine.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if report.Replies != 1 {
		t.Fatalf("expected one reply recorded, got %+v", report)
	}

	updated, err := store.Contact(ctx, contact.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updated.Status != outreach.ContactStatusReplied {
		t.Fatalf("contact should be marked replied, got %s", updated.Status)
	}

	if updated.IntentScore < 80 || updated.IntentLabel != outreach.IntentHigh {
		t.Fatalf("price question should score high intent, got %d/%s", updated.IntentScore, updated.IntentLabel)
	}

	cursor, err := store.Meta(ctx, "imap_last_uid")
	if err != nil {
		t.Fatal(err)
	}

	if cursor != "42:7" {
		t.Fatalf("cursor not persisted with UIDVALIDITY, got %q", cursor)
	}
}

// autopilotContact sets up a store/campaign/contact that already received one
// outbound email (so a reply can thread), with autopilot AI settings.
func autopilotContact(t *testing.T) (*outreach.Store, outreach.Contact, string) {
	t.Helper()

	store, err := outreach.NewStore(filepath.Join(t.TempDir(), "outreach.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	settings := outreach.DefaultSettings()
	settings.EmailAddress = testSenderEmail
	settings.Password = testMailboxSecret
	settings.IMAPHost = "imap.example.com"
	settings.IMAPPort = 993
	settings.SMTPHost = "smtp.example.com"
	settings.SMTPPort = 465
	settings.AIBaseURL = "https://ai.example.com/v1"
	settings.AIModel = testAIModel
	settings.AIAPIKey = testAIKey
	settings.AutoReply = true

	if err := store.SaveSettings(ctx, &settings); err != nil {
		t.Fatal(err)
	}

	campaign := outreach.Campaign{
		ID: uuid.NewString(), Name: "Autopilot", Status: outreach.CampaignStatusActive,
		ValueProposition: "help clinics grow", CallToAction: "Worth a chat?",
		Sequence: outreach.DefaultSequence(),
	}
	if err := store.CreateCampaign(ctx, &campaign); err != nil {
		t.Fatal(err)
	}

	if _, err := store.AddContacts(ctx, []outreach.Contact{{
		CampaignID: campaign.ID, Email: "owner@example.org", Name: "Example Dental",
		Status: outreach.ContactStatusActive, NextSendAt: time.Now().UTC().Add(72 * time.Hour),
	}}); err != nil {
		t.Fatal(err)
	}

	contacts, err := store.Contacts(ctx, campaign.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	contact := contacts[0]
	outbound := outreach.Message{
		CampaignID: campaign.ID, ContactID: contact.ID, Direction: outreach.DirectionOut,
		Step: 0, Subject: "Quick question about Example Dental", Body: "opener",
		MessageID: outreach.NewMessageID(settings.EmailAddress),
		FromEmail: settings.EmailAddress, ToEmail: contact.Email, CreatedAt: time.Now().UTC(),
	}

	if err := store.RecordSent(ctx, &contact, &outbound, time.Time{}, true); err != nil {
		t.Fatal(err)
	}

	return store, contact, outbound.MessageID
}

func inboundReply(contact *outreach.Contact, rootMsgID, body string) *scriptedInbox {
	return &scriptedInbox{
		messages: []outreach.InboundEmail{{
			UID: 11, MessageID: outreach.NewMessageID(contact.Email),
			InReplyTo: []string{rootMsgID}, FromEmail: contact.Email,
			Subject: "Re: Quick question about Example Dental", Body: body,
			Date: time.Now().UTC(),
		}},
		next: outreach.InboxCursor{UIDValidity: 9, LastUID: 11},
	}
}

func TestAutopilotAutoRepliesToSafeMessage(t *testing.T) {
	t.Parallel()

	store, contact, rootID := autopilotContact(t)
	ctx := context.Background()

	ai := &queueAI{responses: []string{
		`{"score":58,"label":"中意向","reason":"想了解更多资料"}`,
		"Thanks for your interest! Here's a quick overview... Would a short call help?\n\nAlex\nExample Co",
	}}
	sender := &fakeSender{}
	engine := outreach.NewEngine(store, sender, inboundReply(&contact, rootID, "Sounds interesting, could you tell me more about how it works?"))
	engine.SetAI(ai)

	report, err := engine.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if report.AutoReplies != 1 {
		t.Fatalf("expected one autopilot reply, got %+v", report)
	}

	sentAuto := false

	for _, m := range sender.messages {
		if m.Kind == outreach.KindAutoReply {
			sentAuto = true
		}
	}

	if !sentAuto {
		t.Fatal("no auto_reply message was sent")
	}

	updated, err := store.Contact(ctx, contact.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updated.NeedsAttention {
		t.Fatal("safe auto-handled reply should not need attention")
	}
}

func TestAutopilotEscalatesHighStakesReply(t *testing.T) {
	t.Parallel()

	store, contact, rootID := autopilotContact(t)
	ctx := context.Background()

	ai := &queueAI{responses: []string{
		`{"score":72,"label":"中意向","reason":"询问价格"}`,
	}}
	sender := &fakeSender{}
	engine := outreach.NewEngine(store, sender, inboundReply(&contact, rootID, "Please send your best price and MOQ for 500 units."))
	engine.SetAI(ai)

	report, err := engine.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if report.Escalated != 1 || report.AutoReplies != 0 {
		t.Fatalf("price question should escalate, not auto-reply: %+v", report)
	}

	for _, m := range sender.messages {
		if m.Kind == outreach.KindAutoReply {
			t.Fatal("high-stakes reply must not be auto-answered")
		}
	}

	updated, err := store.Contact(ctx, contact.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !updated.NeedsAttention || updated.AttentionReason == "" {
		t.Fatalf("high-stakes reply should be flagged for a human: %+v", updated)
	}
}
