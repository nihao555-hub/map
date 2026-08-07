package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

// workflowRepo is a minimal in-memory JobRepository for workflow tests.
type workflowRepo struct {
	jobs []*Job
}

func (r *workflowRepo) Get(_ context.Context, id string) (Job, error) {
	for _, j := range r.jobs {
		if j.ID == id {
			return *j, nil
		}
	}
	return Job{}, ErrJobNotFound
}

func (r *workflowRepo) Create(_ context.Context, job *Job) error {
	cp := *job
	r.jobs = append(r.jobs, &cp)
	return nil
}

func (r *workflowRepo) Delete(_ context.Context, id string) error {
	out := r.jobs[:0]
	for _, j := range r.jobs {
		if j.ID != id {
			out = append(out, j)
		}
	}
	r.jobs = out
	return nil
}

func (r *workflowRepo) Select(_ context.Context, p SelectParams) ([]Job, error) {
	var out []Job
	for _, j := range r.jobs {
		if p.Status != "" && j.Status != p.Status {
			continue
		}
		if p.Owner != "" && j.Owner != p.Owner {
			continue
		}
		out = append(out, *j)
		if p.Limit > 0 && len(out) >= p.Limit {
			break
		}
	}
	return out, nil
}

func (r *workflowRepo) Update(_ context.Context, job *Job) error {
	for i, j := range r.jobs {
		if j.ID == job.ID {
			cp := *job
			r.jobs[i] = &cp
			return nil
		}
	}
	return ErrJobNotFound
}

func (r *workflowRepo) ClaimPending(_ context.Context) (Job, error) {
	// Newest-first (match sqlite store).
	var best *Job
	for _, j := range r.jobs {
		if j.Status != StatusPending {
			continue
		}
		if best == nil || j.Date.After(best.Date) || (j.Date.Equal(best.Date) && j.ID > best.ID) {
			best = j
		}
	}
	if best == nil {
		return Job{}, ErrNoPending
	}
	best.Status = StatusWorking
	return *best, nil
}

func TestWorkflowDecisionsEnforceDeepFullVolume(t *testing.T) {
	dir := t.TempDir()
	repo := &workflowRepo{}
	svc := NewService(repo, dir)
	srv, err := New(svc, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate form scrape with fastmode=on + maxresults=20 — must be ignored.
	form := map[string][]string{
		"name":         {"test"},
		"keywords":     {"cafe"},
		"locations":    {"Jakarta"},
		"lang":         {"id"},
		"country_code": {"id"},
		"country_name": {"Indonesia"},
		"zoom":         {"15"},
		"depth":        {"5"},
		"maxtime":      {"120m"},
		"latitude":     {"-6.2088"},
		"longitude":    {"106.8456"},
		"fastmode":     {"on"},
		"gridmode":     {""},
		"radius_km":    {"5"},
		"maxresults":   {"20"},
		"proxies":      {"http://user:pass@evil.example:8080"},
		"email":        {""},
		"ui_lang":      {"zh"},
	}
	req := httptest.NewRequest(http.MethodPost, "/scrape", nil)
	req.PostForm = form
	req.Form = form
	rec := httptest.NewRecorder()
	srv.scrape(rec, req)
	if rec.Code >= 400 {
		t.Fatalf("scrape status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(repo.jobs) != 1 {
		t.Fatalf("jobs=%d want 1; body=%s", len(repo.jobs), rec.Body.String())
	}
	j := repo.jobs[0]
	if j.Data.FastMode {
		t.Fatal("FastMode must be forced off")
	}
	if !j.Data.GridMode {
		t.Fatal("GridMode must be on")
	}
	if j.Data.MaxResults != 0 {
		t.Fatalf("MaxResults=%d want 0 (unlimited)", j.Data.MaxResults)
	}
	if len(j.Data.Proxies) != 0 {
		t.Fatalf("user proxies must be stripped, got %v", j.Data.Proxies)
	}
	if !j.Data.Email {
		t.Fatal("email must be forced on")
	}
	if j.Data.Radius != 5000 {
		t.Fatalf("radius=%d want 5000", j.Data.Radius)
	}
	if j.Data.Depth < 20 {
		t.Fatalf("depth=%d should be raised for quality", j.Data.Depth)
	}
}

func TestWorkflowAgentUnderstandAndDispatch(t *testing.T) {
	dir := t.TempDir()
	repo := &workflowRepo{}
	svc := NewService(repo, dir)
	srv, err := New(svc, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	body := `{"goal":"在雅加达找咖啡馆和进口商，半径12公里","ui_lang":"zh"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/understand", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.apiAgentUnderstand(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("understand status=%d body=%s", rec.Code, rec.Body.String())
	}
	var plan AgentPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) < 1 {
		t.Fatalf("expected tasks, got %+v", plan)
	}
	if plan.Intent.RadiusKm != 12 {
		t.Fatalf("radius=%d want 12", plan.Intent.RadiusKm)
	}
	rolesOK := false
	for _, r := range plan.Roles {
		if r == "DispatcherAgent" {
			rolesOK = true
		}
	}
	if !rolesOK {
		t.Fatalf("roles=%v", plan.Roles)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/agent/dispatch", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	t0 := time.Now()
	srv.apiAgentDispatch(rec2, req2)
	elapsed := time.Since(t0)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("dispatch status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var result AgentDispatchResult
	if err := json.Unmarshal(rec2.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.JobIDs) == 0 {
		t.Fatalf("no jobs created: %+v", result)
	}
	t.Logf("dispatch created %d jobs in %s", len(result.JobIDs), elapsed)
	for _, id := range result.JobIDs {
		j, err := repo.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Data.FastMode || !j.Data.GridMode || j.Data.MaxResults != 0 {
			t.Fatalf("job %s not deep-full: fast=%v grid=%v max=%d",
				id, j.Data.FastMode, j.Data.GridMode, j.Data.MaxResults)
		}
		if j.Data.Radius != 12000 {
			t.Fatalf("job radius=%d want 12000", j.Data.Radius)
		}
	}
}

func TestConcurrencyCeilingReport(t *testing.T) {
	t.Setenv("GMS_WEB_JOB_CONCURRENCY", "8")
	t.Setenv("GMS_DEEP_WORKERS", "")
	capN := JobConcurrency()
	adaptive := AdaptiveJobConcurrency()
	perDeep := ReservedPerJobConcurrency(2, false)
	avail := AvailableMemoryMB()
	bloom := UseBloomDeduper()

	t.Logf("HOST availMemMB=%d jobCap=%d admitSlots=%d perJobDeep=%d bloom=%v gomaxprocs=%d",
		avail, capN, adaptive, perDeep, bloom, runtime.GOMAXPROCS(0))

	if capN != 8 {
		t.Fatalf("cap=%d want 8", capN)
	}
	if adaptive < 1 || adaptive > capN {
		t.Fatalf("adaptive=%d invalid", adaptive)
	}
	cpus := runtime.GOMAXPROCS(0)
	if avail >= highRAMPackMB {
		if perDeep != 2 {
			t.Fatalf("high-RAM per-job deep want 2 workers at -c=2, got %d", perDeep)
		}
		if got := ReservedPerJobConcurrency(16, false); got != 4 {
			t.Fatalf("raised -c should cap at 4 workers, got %d", got)
		}
		if adaptive > cpus+2 {
			t.Fatalf("admit slots %d should not exceed GOMAXPROCS+2=%d", adaptive, cpus+2)
		}
	} else {
		if perDeep != 2 {
			t.Fatalf("low-RAM per-job deep must stay 2, got %d", perDeep)
		}
		if adaptive*perDeep > cpus*deepPageWorkersPerCPU && cpus >= 2 {
			t.Fatalf("admit slots %d × workers %d should respect global page budget", adaptive, perDeep)
		}
	}
}
