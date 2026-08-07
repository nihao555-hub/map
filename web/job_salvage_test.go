package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type memRepo struct {
	jobs map[string]Job
}

func (m *memRepo) Get(_ context.Context, id string) (Job, error) {
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	return j, nil
}
func (m *memRepo) Create(_ context.Context, job *Job) error {
	m.jobs[job.ID] = *job
	return nil
}
func (m *memRepo) Delete(_ context.Context, id string) error {
	delete(m.jobs, id)
	return nil
}
func (m *memRepo) Select(_ context.Context, p SelectParams) ([]Job, error) {
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		if p.Status != "" && j.Status != p.Status {
			continue
		}
		out = append(out, j)
	}
	return out, nil
}
func (m *memRepo) Update(_ context.Context, job *Job) error {
	m.jobs[job.ID] = *job
	return nil
}
func (m *memRepo) ClaimPending(context.Context) (Job, error) {
	return Job{}, ErrNoPending
}

func TestFinishJobWithOutcomeSalvagesRows(t *testing.T) {
	dir := t.TempDir()
	id := "job-salvage-1"
	csv := "title,phone\nA,1\nB,2\n"
	if err := os.WriteFile(filepath.Join(dir, id+".csv"), []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := &memRepo{jobs: map[string]Job{}}
	svc := NewService(repo, dir)
	job := &Job{ID: id, Name: "t", Status: StatusWorking, Data: JobData{Keywords: []string{"x"}, Lang: "id", Depth: 1, MaxTime: 1}}
	if err := svc.FinishJobWithOutcome(context.Background(), job, "browser crash"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(context.Background(), id)
	if got.Status != StatusOK {
		t.Fatalf("want ok, got %s", got.Status)
	}
	if got.Data.LastError == "" {
		t.Fatal("expected last_error")
	}
}

func TestFinishJobWithOutcomeFailsWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	repo := &memRepo{jobs: map[string]Job{}}
	svc := NewService(repo, dir)
	job := &Job{ID: "empty-1", Name: "t", Status: StatusWorking, Data: JobData{}}
	if err := svc.FinishJobWithOutcome(context.Background(), job, "no seeds"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(context.Background(), "empty-1")
	if got.Status != StatusFailed {
		t.Fatalf("want failed, got %s", got.Status)
	}
}

func TestServiceCreateForcesEmail(t *testing.T) {
	repo := &memRepo{jobs: map[string]Job{}}
	svc := NewService(repo, t.TempDir())
	job := &Job{ID: "email-required", Data: JobData{Email: false}}

	if err := svc.Create(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if !repo.jobs[job.ID].Data.Email {
		t.Fatal("service must force email extraction")
	}
}
