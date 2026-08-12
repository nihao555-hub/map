package outreach_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gosom/google-maps-scraper/outreach"
)

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
	uint32,
) ([]outreach.InboundEmail, uint32, error) {
	return nil, 0, nil
}

func (i *emptyInbox) Test(context.Context, *outreach.Settings) error {
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
	settings.Password = "in-memory-test-secret"
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
