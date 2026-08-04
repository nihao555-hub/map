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

	// Invite codes are reusable accounts: second login must succeed.
	token2, _, err := store.Redeem(context.Background(), created[0])
	if err != nil {
		t.Fatalf("reuse redeem: %v", err)
	}
	if token2 == "" || token2 == token {
		t.Fatalf("expected new session token, got %q", token2)
	}
	code1, err := store.SessionInviteCode(context.Background(), token)
	if err != nil || code1 != created[0] {
		t.Fatalf("old session code=%q err=%v", code1, err)
	}
	code2, err := store.SessionInviteCode(context.Background(), token2)
	if err != nil || code2 != created[0] {
		t.Fatalf("new session code=%q err=%v", code2, err)
	}

	exportPath := filepath.Join(dir, "invite_codes.txt")
	if err := store.ExportFile(context.Background(), exportPath); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
}

func TestOwnerIsolation(t *testing.T) {
	t.Parallel()

	store, err := sqlite.New(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	mk := func(id, owner string) *web.Job {
		return &web.Job{
			ID:     id,
			Name:   owner + "-" + id,
			Status: web.StatusPending,
			Owner:  owner,
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
	}
	if err := store.Create(context.Background(), mk("a1", "GMS-AAAA-AAAA")); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(context.Background(), mk("b1", "GMS-BBBB-BBBB")); err != nil {
		t.Fatal(err)
	}

	aJobs, err := store.Select(context.Background(), web.SelectParams{Owner: "GMS-AAAA-AAAA"})
	if err != nil || len(aJobs) != 1 || aJobs[0].ID != "a1" {
		t.Fatalf("owner A: %+v err=%v", aJobs, err)
	}
	bJobs, err := store.Select(context.Background(), web.SelectParams{Owner: "GMS-BBBB-BBBB"})
	if err != nil || len(bJobs) != 1 || bJobs[0].ID != "b1" {
		t.Fatalf("owner B: %+v err=%v", bJobs, err)
	}

	svc := web.NewService(store, t.TempDir())
	if _, err := svc.GetOwned(context.Background(), "a1", "GMS-BBBB-BBBB"); !web.IsJobNotFound(err) {
		t.Fatalf("cross-tenant get want not found, got %v", err)
	}
	got, err := svc.GetOwned(context.Background(), "a1", "GMS-AAAA-AAAA")
	if err != nil || got.ID != "a1" {
		t.Fatalf("same-tenant get: %+v err=%v", got, err)
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
