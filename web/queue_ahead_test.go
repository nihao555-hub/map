package web

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestQueueAheadPendingOrder(t *testing.T) {
	repo := &workflowRepo{}
	svc := NewService(repo, t.TempDir())
	ctx := context.Background()

	mk := func(id, name string, when time.Time) *Job {
		j := &Job{
			ID: id, Name: name, Date: when, Status: StatusPending,
			Data: JobData{
				Keywords: []string{"cafe"}, Lang: "en", Zoom: 15,
				Radius: 5000, Depth: 10, MaxTime: time.Hour, Email: true,
			},
		}
		if err := j.Validate(); err != nil {
			t.Fatal(err)
		}
		return j
	}

	older := mk("job-old", "older", time.Now().UTC().Add(-2*time.Minute))
	newer := mk("job-new", "newer", time.Now().UTC())
	if err := svc.Create(ctx, older); err != nil {
		t.Fatal(err)
	}
	if err := svc.Create(ctx, newer); err != nil {
		t.Fatal(err)
	}

	ahead, status, total, err := svc.QueueAhead(ctx, newer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusPending {
		t.Fatalf("status=%s", status)
	}
	if total != 2 {
		t.Fatalf("pendingTotal=%d", total)
	}
	if ahead != 1 {
		t.Fatalf("ahead=%d want 1", ahead)
	}

	aheadOld, _, _, err := svc.QueueAhead(ctx, older.ID)
	if err != nil {
		t.Fatal(err)
	}
	if aheadOld != 0 {
		t.Fatalf("older ahead=%d want 0", aheadOld)
	}
}

func TestListJobsHidesFromAgent(t *testing.T) {
	repo := &workflowRepo{}
	svc := NewService(repo, t.TempDir())
	srv, err := New(svc, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	agentJob := &Job{
		ID: "a1", Name: "Jakarta · panel", Date: time.Now().UTC(), Status: StatusPending,
		Data: JobData{
			Keywords: []string{"panel listrik"}, Lang: "id", Zoom: 15,
			Radius: 15000, Depth: 10, MaxTime: time.Hour, Email: true, FromAgent: true,
		},
	}
	mapJob := &Job{
		ID: "m1", Name: "map cafe", Date: time.Now().UTC(), Status: StatusPending,
		Data: JobData{
			Keywords: []string{"cafe"}, Lang: "en", Zoom: 15,
			Radius: 5000, Depth: 10, MaxTime: time.Hour, Email: true,
		},
	}
	for _, j := range []*Job{agentJob, mapJob} {
		if err := j.Validate(); err != nil {
			t.Fatal(err)
		}
		if err := svc.Create(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	jobs, err := srv.listJobsForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "m1" {
		t.Fatalf("got %+v want only map job", jobs)
	}

	legacy := &Job{
		ID: "legacy", Name: "泗水 · 配电柜", Date: time.Now().UTC(), Status: StatusPending,
		Data: JobData{
			Keywords: []string{"panel listrik"}, RawKeywords: []string{"配电柜"},
			Lang: "id", Zoom: 15, Radius: 15000, Depth: 10, MaxTime: time.Hour, Email: true,
			EnableIntel: true,
		},
	}
	if err := legacy.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := svc.Create(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	jobs, err = srv.listJobsForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "m1" {
		t.Fatalf("legacy agent job still visible: %+v", jobs)
	}
}
