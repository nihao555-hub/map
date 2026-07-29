package web_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosom/google-maps-scraper/web"
)

type statsRepository struct {
	jobs []web.Job
}

func (r *statsRepository) Get(_ context.Context, id string) (web.Job, error) {
	for i := range r.jobs {
		if r.jobs[i].ID == id {
			return r.jobs[i], nil
		}
	}

	return web.Job{}, os.ErrNotExist
}

func (r *statsRepository) Create(_ context.Context, job *web.Job) error {
	r.jobs = append(r.jobs, *job)

	return nil
}

func (r *statsRepository) Delete(_ context.Context, _ string) error { return nil }
func (r *statsRepository) Update(_ context.Context, _ *web.Job) error {
	return nil
}
func (r *statsRepository) Select(_ context.Context, _ web.SelectParams) ([]web.Job, error) {
	return r.jobs, nil
}

func TestStatsCacheInvalidatesOnFileChange(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	repo := &statsRepository{jobs: []web.Job{{ID: "job", Date: now, Status: web.StatusOK}}}
	svc := web.NewService(repo, dir)
	path := filepath.Join(dir, "job.csv")

	cases := []struct {
		name    string
		content string
		update  bool
		want    int
	}{
		{name: "initial", content: "title\none\n", want: 1},
		{name: "cached", content: "title\ntwo\n", want: 1},
		{name: "changed", content: "title\none\ntwo\n", update: true, want: 2},
	}

	var originalModTime time.Time

	for _, tc := range cases {
		if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
			t.Fatal(err)
		}

		if originalModTime.IsZero() {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}

			originalModTime = info.ModTime()
		}

		if !tc.update {
			if err := os.Chtimes(path, originalModTime, originalModTime); err != nil {
				t.Fatal(err)
			}
		} else {
			changed := originalModTime.Add(time.Second)
			if err := os.Chtimes(path, changed, changed); err != nil {
				t.Fatal(err)
			}
		}

		stats, err := svc.Stats(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if stats.TotalRecords != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.name, stats.TotalRecords, tc.want)
		}
	}
}

func TestStatsAggregatesJobsAndRecords(t *testing.T) {
	now := time.Now()
	repo := &statsRepository{jobs: []web.Job{
		{ID: "today-ok", Date: now, Status: web.StatusOK},
		{ID: "today-running", Date: now, Status: web.StatusWorking},
		{ID: "today-failed", Date: now, Status: web.StatusFailed},
		{ID: "old-ok", Date: now.AddDate(0, 0, -1), Status: web.StatusOK},
	}}
	dir := t.TempDir()
	svc := web.NewService(repo, dir)

	for id, content := range map[string]string{
		"today-ok":      "title\na\nb\n",
		"today-running": "title\nc\n",
		"today-failed":  "title\n",
		"old-ok":        "title\nd\ne\nf\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, id+".csv"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := svc.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := struct {
		totalJobs, totalRecords, jobsToday, recordsToday, runningJobs, failedJobs int
	}{4, 6, 3, 3, 1, 1}
	if stats.TotalJobs != want.totalJobs || stats.TotalRecords != want.totalRecords ||
		stats.JobsToday != want.jobsToday || stats.RecordsToday != want.recordsToday ||
		stats.RunningJobs != want.runningJobs || stats.FailedJobs != want.failedJobs {
		t.Fatalf("got %+v, want %+v", stats, want)
	}
}
