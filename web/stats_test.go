package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type statsRepository struct {
	jobs []Job
}

func (r *statsRepository) Get(_ context.Context, id string) (Job, error) {
	for _, job := range r.jobs {
		if job.ID == id {
			return job, nil
		}
	}

	return Job{}, os.ErrNotExist
}

func (r *statsRepository) Create(_ context.Context, job *Job) error {
	r.jobs = append(r.jobs, *job)

	return nil
}

func (r *statsRepository) Delete(_ context.Context, _ string) error { return nil }
func (r *statsRepository) Update(_ context.Context, _ *Job) error   { return nil }
func (r *statsRepository) Select(_ context.Context, _ SelectParams) ([]Job, error) {
	return r.jobs, nil
}

func TestCachedCSVCountInvalidatesOnFileChange(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(&statsRepository{}, dir)
	path := filepath.Join(dir, "job.csv")

	cases := []struct {
		name    string
		content string
		want    int
	}{
		{name: "initial", content: "title\none\n", want: 1},
		{name: "changed", content: "title\none\ntwo\n", want: 2},
	}

	for i, tc := range cases {
		if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			now := time.Now().Add(time.Duration(i) * time.Second)
			if err := os.Chtimes(path, now, now); err != nil {
				t.Fatal(err)
			}
		}

		got, err := svc.cachedCSVCount("job")
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestStatsAggregatesJobsAndRecords(t *testing.T) {
	now := time.Now()
	repo := &statsRepository{jobs: []Job{
		{ID: "today-ok", Date: now, Status: StatusOK},
		{ID: "today-running", Date: now, Status: StatusWorking},
		{ID: "today-failed", Date: now, Status: StatusFailed},
		{ID: "old-ok", Date: now.AddDate(0, 0, -1), Status: StatusOK},
	}}
	svc := NewService(repo, t.TempDir())

	for id, content := range map[string]string{
		"today-ok":      "title\na\nb\n",
		"today-running": "title\nc\n",
		"today-failed":  "title\n",
		"old-ok":        "title\nd\ne\nf\n",
	} {
		if err := os.WriteFile(filepath.Join(svc.dataFolder, id+".csv"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := svc.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := UsageStats{
		TotalJobs:    4,
		TotalRecords: 6,
		JobsToday:    3,
		RecordsToday: 3,
		RunningJobs:  1,
		FailedJobs:   1,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
