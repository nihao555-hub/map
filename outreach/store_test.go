package outreach_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gosom/google-maps-scraper/outreach"
)

func TestImportCSVAndCampaignStats(t *testing.T) {
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
	settings.DefaultTimezone = "Europe/Berlin"

	if err := store.SaveSettings(ctx, &settings); err != nil {
		t.Fatal(err)
	}

	campaign := outreach.Campaign{
		ID:               uuid.NewString(),
		Name:             "Berlin dentists",
		Status:           outreach.CampaignStatusDraft,
		ValueProposition: "help clinics turn more web visits into appointments",
		CallToAction:     "Would a quick conversation be useful?",
		Sequence:         outreach.DefaultSequence(),
	}
	if err := store.CreateCampaign(ctx, &campaign); err != nil {
		t.Fatal(err)
	}

	var data bytes.Buffer

	writer := csv.NewWriter(&data)

	if err := writer.Write([]string{
		"title", "category", "address", "website", "phone", "review_rating",
		"review_count", "timezone", "complete_address", "emails",
	}); err != nil {
		t.Fatal(err)
	}

	if err := writer.Write([]string{
		"Acme Dental", "dentist", "Main Street", "https://example.com", "+49 1",
		"4.8", "120", "Europe/Berlin", `{"city":"Berlin"}`,
		"info@example.com, sales@example.com, info@example.com",
	}); err != nil {
		t.Fatal(err)
	}

	writer.Flush()

	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}

	summary, err := outreach.ImportCSV(
		ctx,
		store,
		campaign.ID,
		&data,
		time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}

	if summary.Inserted != 1 {
		t.Fatalf("inserted = %d, want 1 (summary: %+v)", summary.Inserted, summary)
	}

	contacts, err := store.Contacts(ctx, campaign.ID, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(contacts) != 1 {
		t.Fatalf("contacts = %d, want 1", len(contacts))
	}

	for _, contact := range contacts {
		if contact.Email != "sales@example.com" {
			t.Fatalf("expected best business recipient, got %q", contact.Email)
		}

		if contact.City != "Berlin" || contact.ReviewCount != 120 || contact.Rating != "4.8" {
			t.Fatalf("imported contact missing personalization: %+v", contact)
		}
	}

	stats, err := store.Stats(ctx, campaign.ID)
	if err != nil {
		t.Fatal(err)
	}

	if stats.Contacts != 1 || stats.Sent != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func newTestStore(t *testing.T) *outreach.Store {
	t.Helper()

	store, err := outreach.NewStore(filepath.Join(t.TempDir(), "outreach.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})

	return store
}

func seedContact(t *testing.T, store *outreach.Store, campaignID, email string) outreach.Contact {
	t.Helper()

	if _, err := store.AddContacts(context.Background(), []outreach.Contact{{
		CampaignID: campaignID,
		Email:      email,
		Name:       email,
		Status:     outreach.ContactStatusActive,
		NextSendAt: time.Now().UTC().Add(-time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}

	contacts, err := store.Contacts(context.Background(), campaignID, 50)
	if err != nil {
		t.Fatal(err)
	}

	for i := range contacts {
		if contacts[i].Email == email {
			return contacts[i]
		}
	}

	t.Fatalf("seeded contact %q not found", email)

	return outreach.Contact{}
}

func makeCampaign(t *testing.T, store *outreach.Store, name string) outreach.Campaign {
	t.Helper()

	campaign := outreach.Campaign{
		ID:               uuid.NewString(),
		Name:             name,
		Status:           outreach.CampaignStatusActive,
		ValueProposition: "help businesses grow",
		CallToAction:     "Worth a quick chat?",
		Sequence:         outreach.DefaultSequence(),
	}

	if err := store.CreateCampaign(context.Background(), &campaign); err != nil {
		t.Fatal(err)
	}

	return campaign
}

func TestUnsubscribeStopsAddressAcrossCampaignsAndSuppresses(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	campaignA := makeCampaign(t, store, "A")
	campaignB := makeCampaign(t, store, "B")

	const email = "owner@shared.example"

	contactA := seedContact(t, store, campaignA.ID, email)
	contactB := seedContact(t, store, campaignB.ID, email)

	// Unsubscribe arrives in campaign A.
	unsub := outreach.Message{
		CampaignID: contactA.CampaignID,
		ContactID:  contactA.ID,
		Direction:  outreach.DirectionIn,
		Kind:       outreach.InboundKindUnsubscribe,
		Subject:    "Re: hi",
		Body:       "Please unsubscribe me.",
		FromEmail:  email,
		ToEmail:    "me@example.com",
		CreatedAt:  time.Now().UTC(),
	}
	if err := store.RecordInbound(ctx, &contactA, &unsub); err != nil {
		t.Fatal(err)
	}

	// The same address in campaign B must also be stopped.
	updatedB, err := store.Contact(ctx, contactB.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updatedB.Status != outreach.ContactStatusUnsubscribed {
		t.Fatalf("campaign B contact not stopped: %s", updatedB.Status)
	}

	suppressed, err := store.IsSuppressed(ctx, email)
	if err != nil {
		t.Fatal(err)
	}

	if !suppressed {
		t.Fatal("address should be globally suppressed")
	}

	// Due contacts must exclude the suppressed address in every campaign.
	due, err := store.DueContacts(ctx, time.Now().UTC(), 50)
	if err != nil {
		t.Fatal(err)
	}

	if len(due) != 0 {
		t.Fatalf("suppressed address should not be due, got %d", len(due))
	}
}

func TestLaterReplyDoesNotOverrideUnsubscribe(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	campaign := makeCampaign(t, store, "A")
	contact := seedContact(t, store, campaign.ID, "optout@shared.example")

	unsub := outreach.Message{
		CampaignID: contact.CampaignID, ContactID: contact.ID,
		Direction: outreach.DirectionIn, Kind: outreach.InboundKindUnsubscribe,
		Subject: "stop", Body: "unsubscribe", FromEmail: contact.Email,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.RecordInbound(ctx, &contact, &unsub); err != nil {
		t.Fatal(err)
	}

	reply := outreach.Message{
		CampaignID: contact.CampaignID, ContactID: contact.ID,
		Direction: outreach.DirectionIn, Kind: outreach.InboundKindReply,
		Subject: "actually", Body: "wait, tell me more", FromEmail: contact.Email,
		CreatedAt: time.Now().UTC().Add(time.Hour),
	}
	if err := store.RecordInbound(ctx, &contact, &reply); err != nil {
		t.Fatal(err)
	}

	updated, err := store.Contact(ctx, contact.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updated.Status != outreach.ContactStatusUnsubscribed {
		t.Fatalf("unsubscribe must be terminal, got %s", updated.Status)
	}
}

func TestOverviewAggregatesAcrossCampaigns(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	campaignA := makeCampaign(t, store, "A")
	campaignB := makeCampaign(t, store, "B")

	replied := seedContact(t, store, campaignA.ID, "buyer@acme.example")
	seedContact(t, store, campaignA.ID, "cold@acme.example")
	seedContact(t, store, campaignB.ID, "lead@beta.example")

	reply := outreach.Message{
		CampaignID: replied.CampaignID, ContactID: replied.ID,
		Direction: outreach.DirectionIn, Kind: outreach.InboundKindReply,
		Subject: "Re: hi", Body: "Please send your price list.", FromEmail: replied.Email,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.RecordInbound(ctx, &replied, &reply); err != nil {
		t.Fatal(err)
	}

	if err := store.UpdateContactIntent(ctx, replied.ID, outreach.Intent{
		Score: 88, Label: outreach.IntentHigh, Reason: "asked for pricing",
	}); err != nil {
		t.Fatal(err)
	}

	overview, err := store.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if overview.Campaigns != 2 || overview.ActiveCampaigns != 2 {
		t.Fatalf("campaign counts wrong: %+v", overview)
	}

	if overview.Contacts != 3 || overview.Replied != 1 || overview.HighIntent != 1 {
		t.Fatalf("contact aggregates wrong: %+v", overview)
	}
}

func TestNoticeDoesNotStopContact(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	campaign := makeCampaign(t, store, "A")
	contact := seedContact(t, store, campaign.ID, "delay@shared.example")

	notice := outreach.Message{
		CampaignID: contact.CampaignID, ContactID: contact.ID,
		Direction: outreach.DirectionIn, Kind: outreach.InboundKindNotice,
		Subject: "Delivery delay", Body: "still trying", FromEmail: "mailer-daemon@mx.example",
		CreatedAt: time.Now().UTC(),
	}
	if err := store.RecordInbound(ctx, &contact, &notice); err != nil {
		t.Fatal(err)
	}

	updated, err := store.Contact(ctx, contact.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updated.Status != outreach.ContactStatusActive {
		t.Fatalf("a delivery-delay notice must not stop the contact, got %s", updated.Status)
	}
}
