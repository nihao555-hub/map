package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type phaseRepo struct {
	job Job
}

func (r *phaseRepo) Get(_ context.Context, id string) (Job, error) {
	if r.job.ID != id {
		return Job{}, ErrJobNotFound
	}
	return r.job, nil
}
func (r *phaseRepo) Create(_ context.Context, j *Job) error { r.job = *j; return nil }
func (r *phaseRepo) Delete(context.Context, string) error   { return nil }
func (r *phaseRepo) Select(context.Context, SelectParams) ([]Job, error) {
	return []Job{r.job}, nil
}
func (r *phaseRepo) Update(_ context.Context, j *Job) error { r.job = *j; return nil }
func (r *phaseRepo) ClaimPending(context.Context) (Job, error) {
	return Job{}, ErrNoPending
}

func TestMarkScrapeCompleteStartsIntelPhase(t *testing.T) {
	dir := t.TempDir()
	repo := &phaseRepo{job: Job{
		ID:     "j1",
		Name:   "t",
		Date:   time.Now().UTC(),
		Status: StatusWorking,
		Data:   JobData{EnableIntel: true, Keywords: []string{"panel listrik"}},
	}}
	svc := NewService(repo, dir)
	// empty places → intel done immediately when ok with 0 places
	ok, err := svc.MarkScrapeComplete(context.Background(), "j1")
	if err != nil || !ok {
		t.Fatalf("mark: ok=%v err=%v", ok, err)
	}
	if repo.job.Status != StatusOK {
		t.Fatalf("status=%s", repo.job.Status)
	}
	job := repo.job
	svc.EnrichJobPhase(context.Background(), &job)
	// no places → PhaseOK (intel Done=true for 0 places)
	if job.Phase != PhaseOK {
		t.Fatalf("phase=%s want ok (no places)", job.Phase)
	}

	// Second call is no-op
	ok, err = svc.MarkScrapeComplete(context.Background(), "j1")
	if err != nil || ok {
		t.Fatalf("second mark should be no-op: ok=%v err=%v", ok, err)
	}
}

func TestPhaseLabelZH(t *testing.T) {
	cases := []struct{ phase, status, want string }{
		{"intel", "ok", "背调中"},
		{"", "working", "采集中"},
		{"", "pending", "排队中"},
		{"ok", "ok", "已完成"},
		{"", "failed", "失败"},
		{"", "canceled", "已终止"},
	}
	for _, c := range cases {
		if got := PhaseLabelZH(c.phase, c.status); got != c.want {
			t.Fatalf("phase=%q status=%q got %q want %q", c.phase, c.status, got, c.want)
		}
	}
}

func TestEnrichJobPhaseIntelWhenPending(t *testing.T) {
	dir := t.TempDir()
	jobID := "j2"
	// minimal CSV with one place so intel is not done
	csvPath := filepath.Join(dir, jobID+".csv")
	body := "title,category,address,latitude,longitude,place_id,link,phone,website\n" +
		"Toko Listrik A,Toko Alat Listrik,Jl 1,-6.2,106.8,ChIJ1,https://maps.google.com/,+621,http://a.test\n"
	if err := os.WriteFile(csvPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &phaseRepo{job: Job{
		ID: jobID, Name: "t", Date: time.Now().UTC(), Status: StatusOK,
		Data: JobData{EnableIntel: true, Keywords: []string{"panel listrik"}},
	}}
	svc := NewService(repo, dir)
	job := repo.job
	svc.EnrichJobPhase(context.Background(), &job)
	if job.Phase != PhaseIntel {
		t.Fatalf("phase=%s want intel", job.Phase)
	}
}
