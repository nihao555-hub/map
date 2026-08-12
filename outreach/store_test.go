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
	if err := store.SaveSettings(ctx, settings); err != nil {
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
