package outreach_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gosom/google-maps-scraper/outreach"
)

func TestCreateCampaignFromJobsBatchDeduplicates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := outreach.NewStore(filepath.Join(dir, "outreach.db"))
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

	// Two map jobs that share one business email; batch import must keep it once.
	job1 := uuid.NewString()
	job2 := uuid.NewString()

	writeJobCSV(t, dir, job1, "title,emails,timezone\nAcme,owner@acme.example,Europe/Berlin\nBeta,shared@corp.example,Europe/Berlin\n")
	writeJobCSV(t, dir, job2, "title,emails,timezone\nGamma,shared@corp.example,Europe/Berlin\nDelta,sales@delta.example,Europe/Berlin\n")

	engine := outreach.NewEngine(store, &fakeSender{}, &emptyInbox{})
	svc := outreach.NewService(store, engine, dir)

	input := outreach.CampaignInput{
		Name:             "Batch blast",
		ValueProposition: "help exporters win more overseas buyers",
	}

	view, summary, err := svc.CreateCampaignFromJobs(ctx, &input, []string{job1, job2})
	if err != nil {
		t.Fatal(err)
	}

	if summary.Inserted != 3 {
		t.Fatalf("inserted = %d, want 3 (dedup shared@corp.example across jobs)", summary.Inserted)
	}

	if view.Stats.Contacts != 3 {
		t.Fatalf("campaign contacts = %d, want 3", view.Stats.Contacts)
	}
}

func TestCreateCampaignFromJobsRequiresAtLeastOneJob(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	store, err := outreach.NewStore(filepath.Join(dir, "outreach.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})

	engine := outreach.NewEngine(store, &fakeSender{}, &emptyInbox{})
	svc := outreach.NewService(store, engine, dir)

	input := outreach.CampaignInput{Name: "x", ValueProposition: "y"}

	if _, _, err := svc.CreateCampaignFromJobs(context.Background(), &input, nil); err == nil {
		t.Fatal("expected error when no jobs are selected")
	}
}

func writeJobCSV(t *testing.T, dir, jobID, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, jobID+".csv"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
