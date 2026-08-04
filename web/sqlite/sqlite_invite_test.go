package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosom/google-maps-scraper/web"
	"github.com/gosom/google-maps-scraper/web/sqlite"
)

func TestInviteSeedRedeemAndGate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := sqlite.New(filepath.Join(dir, "jobs.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	created, err := store.EnsureSeed(context.Background(), 100)
	if err != nil {
		t.Fatalf("EnsureSeed: %v", err)
	}
	if len(created) != 100 {
		t.Fatalf("created=%d want 100", len(created))
	}

	again, err := store.EnsureSeed(context.Background(), 100)
	if err != nil {
		t.Fatalf("EnsureSeed again: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("expected 0 new codes, got %d", len(again))
	}

	total, used, unused, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if total != 100 || used != 0 || unused != 100 {
		t.Fatalf("stats total=%d used=%d unused=%d", total, used, unused)
	}

	token, expires, err := store.Redeem(context.Background(), created[0])
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if token == "" || expires.IsZero() {
		t.Fatal("empty token/expires")
	}

	ok, err := store.ValidSession(context.Background(), token)
	if err != nil || !ok {
		t.Fatalf("ValidSession: ok=%v err=%v", ok, err)
	}

	_, _, err = store.Redeem(context.Background(), created[0])
	if err != web.ErrInvalidInvite {
		t.Fatalf("reuse want ErrInvalidInvite, got %v", err)
	}

	exportPath := filepath.Join(dir, "invite_codes.txt")
	if err := store.ExportFile(context.Background(), exportPath); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
}

func TestClaimPending(t *testing.T) {
	t.Parallel()

	store, err := sqlite.New(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = store.ClaimPending(context.Background())
	if err != web.ErrNoPending {
		t.Fatalf("empty claim: %v", err)
	}

	job := &web.Job{
		ID:     "j1",
		Name:   "test",
		Status: web.StatusPending,
		Date:   time.Now().UTC(),
		Data: web.JobData{
			Keywords: []string{"coffee"},
			Lang:     "en",
			Zoom:     15,
			Lat:      "1",
			Lon:      "1",
			Depth:    1,
			MaxTime:  time.Minute,
		},
	}
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("Create: %v", err)
	}

	claimed, err := store.ClaimPending(context.Background())
	if err != nil {
		t.Fatalf("ClaimPending: %v", err)
	}
	if claimed.ID != "j1" || claimed.Status != web.StatusWorking {
		t.Fatalf("claimed=%+v", claimed)
	}

	_, err = store.ClaimPending(context.Background())
	if err != web.ErrNoPending {
		t.Fatalf("second claim: %v", err)
	}
}
