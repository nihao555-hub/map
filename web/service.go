package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Service struct {
	repo       JobRepository
	dataFolder string

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

func NewService(repo JobRepository, dataFolder string) *Service {
	return &Service{
		repo:       repo,
		dataFolder: dataFolder,
		cancels:    make(map[string]context.CancelFunc),
	}
}

// RegisterJobCancel stores the cancel func for a running scrape (called by webrunner).
func (s *Service) RegisterJobCancel(id string, cancel context.CancelFunc) {
	if s == nil || id == "" || cancel == nil {
		return
	}
	s.cancelMu.Lock()
	s.cancels[id] = cancel
	s.cancelMu.Unlock()
}

// UnregisterJobCancel removes a finished job's cancel func.
func (s *Service) UnregisterJobCancel(id string) {
	if s == nil || id == "" {
		return
	}
	s.cancelMu.Lock()
	delete(s.cancels, id)
	s.cancelMu.Unlock()
}

// signalCancel invokes a registered cancel func if present.
func (s *Service) signalCancel(id string) bool {
	s.cancelMu.Lock()
	cancel := s.cancels[id]
	s.cancelMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// CancelOwned marks a pending/working job canceled for the owner and stops the runner if active.
func (s *Service) CancelOwned(ctx context.Context, id, owner string) error {
	job, err := s.GetOwned(ctx, id, owner)
	if err != nil {
		return err
	}
	switch job.Status {
	case StatusPending, StatusWorking:
		s.signalCancel(id)
		job.Status = StatusCanceled
		return s.Update(ctx, &job)
	case StatusCanceled:
		return nil
	default:
		return fmt.Errorf("job %s is %s and cannot be canceled", id, job.Status)
	}
}

func (s *Service) Create(ctx context.Context, job *Job) error {
	if job != nil {
		// Server-side invariant: no UI/API path may disable contact enrichment.
		job.Data.Email = true
	}
	return s.repo.Create(ctx, job)
}

func (s *Service) All(ctx context.Context) ([]Job, error) {
	return s.repo.Select(ctx, SelectParams{})
}

// Phase constants for Job.Phase (UI annotation, not persisted).
const (
	PhasePending  = "pending"
	PhaseWorking  = "working"
	PhaseIntel    = "intel"
	PhaseOK       = "ok"
	PhaseFailed   = "failed"
	PhaseCanceled = "canceled"
)

// PhaseLabelZH returns the canonical Chinese badge for a job phase/status.
func PhaseLabelZH(phase, status string) string {
	switch {
	case phase == PhaseIntel:
		return "背调中"
	case status == StatusPending || phase == PhasePending:
		return "排队中"
	case status == StatusWorking || phase == PhaseWorking:
		return "采集中"
	case status == StatusFailed || phase == PhaseFailed:
		return "失败"
	case status == StatusCanceled || phase == PhaseCanceled:
		return "已终止"
	case status == StatusOK || phase == PhaseOK:
		return "已完成"
	default:
		return status
	}
}

// EnrichJobPhase sets Job.Phase for API/HTML so the dock can show「背调中」after scrape rows land.
func (s *Service) EnrichJobPhase(ctx context.Context, job *Job) {
	if job == nil {
		return
	}
	switch job.Status {
	case StatusPending:
		job.Phase = PhasePending
	case StatusWorking:
		job.Phase = PhaseWorking
	case StatusFailed:
		job.Phase = PhaseFailed
	case StatusCanceled:
		job.Phase = PhaseCanceled
	case StatusOK:
		job.Phase = PhaseOK
		if job.Data.EnableIntel {
			n := 0
			if places, err := s.GetPlacesLiteCached(ctx, job.ID); err == nil {
				n = len(FilterPlacesLiteForJob(places, job.Data))
			}
			st := s.GetJobIntelStatus(job.ID, n)
			if !st.Done {
				job.Phase = PhaseIntel
			}
		}
	default:
		job.Phase = job.Status
	}
}

// EnrichJobsPhase annotates a job list in place.
func (s *Service) EnrichJobsPhase(ctx context.Context, jobs []Job) {
	for i := range jobs {
		s.EnrichJobPhase(ctx, &jobs[i])
	}
}

// MarkScrapeComplete promotes a still-working job to ok and starts intel once.
// Used when Maps seeds finished (place rows on disk) while website-email jobs may still run.
// Returns true when this call performed the transition.
func (s *Service) MarkScrapeComplete(ctx context.Context, jobID string) (bool, error) {
	job, err := s.Get(ctx, jobID)
	if err != nil {
		return false, err
	}
	if job.Status != StatusWorking {
		return false, nil
	}
	job.Status = StatusOK
	if err := s.Update(ctx, &job); err != nil {
		return false, err
	}
	if job.Data.EnableIntel {
		s.StartJobIntel(ctx, jobID)
	}
	return true, nil
}

// QueueAhead returns how many pending jobs are ahead of jobID in claim order.
// ClaimPending takes newest first, so "ahead" = newer pending jobs.
// Non-pending jobs return ahead=0 with their current status.
func (s *Service) QueueAhead(ctx context.Context, jobID string) (ahead int, status string, pendingTotal int, err error) {
	job, err := s.Get(ctx, jobID)
	if err != nil {
		return 0, "", 0, err
	}
	pending, err := s.repo.Select(ctx, SelectParams{Status: StatusPending})
	if err != nil {
		return 0, job.Status, 0, err
	}
	pendingTotal = len(pending)
	if job.Status != StatusPending {
		return 0, job.Status, pendingTotal, nil
	}
	for _, j := range pending {
		if j.ID == jobID {
			continue
		}
		// Newest-first claim: jobs created later run before this one.
		if j.Date.After(job.Date) || (j.Date.Equal(job.Date) && j.ID > jobID) {
			ahead++
		}
	}
	return ahead, job.Status, pendingTotal, nil
}

// RequeueStaleWorking recovers jobs stuck in working after a process crash.
func (s *Service) RequeueStaleWorking(ctx context.Context, maxAge time.Duration) (int, error) {
	type requeuer interface {
		RequeueStaleWorking(context.Context, time.Duration) (int, error)
	}
	if r, ok := s.repo.(requeuer); ok {
		return r.RequeueStaleWorking(ctx, maxAge)
	}
	return 0, nil
}

// FailStaleWorking marks heartbeat-dead working jobs as failed (zombie cleanup).
// When the same process still holds those jobs, it also cancels their contexts
// so Playwright exits and BeginDeepJob/EndDeepJob admit slots are freed.
func (s *Service) FailStaleWorking(ctx context.Context, maxAge time.Duration) (int, error) {
	type failerIDs interface {
		FailStaleWorkingIDs(context.Context, time.Duration) ([]string, error)
	}
	type failer interface {
		FailStaleWorking(context.Context, time.Duration) (int, error)
	}

	var (
		ids []string
		n   int
		err error
	)
	if f, ok := s.repo.(failerIDs); ok {
		ids, err = f.FailStaleWorkingIDs(ctx, maxAge)
		n = len(ids)
	} else if f, ok := s.repo.(failer); ok {
		n, err = f.FailStaleWorking(ctx, maxAge)
	} else {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if s.signalCancel(id) {
			log.Printf("zombie: signaled cancel for stale working job %s", id)
		}
	}
	if n > 0 {
		if sn, serr := s.SalvageFailedJobsWithResults(ctx); serr == nil && sn > 0 {
			log.Printf("salvage: restored %d failed job(s) that already had CSV rows", sn)
		}
	}
	return n, nil
}

// CountCSVDataRows returns how many data rows a job CSV currently has (0 if missing).
func (s *Service) CountCSVDataRows(id string) int {
	datapath, err := s.csvPath(id)
	if err != nil {
		return 0
	}
	b, err := os.ReadFile(datapath)
	if err != nil || len(b) == 0 {
		return 0
	}
	lines := 0
	for _, c := range b {
		if c == '\n' {
			lines++
		}
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		lines++
	}
	if lines <= 1 {
		return 0
	}
	return lines - 1
}

// FinishJobWithOutcome marks a job failed, or ok when CSV already has rows
// (partial success after crash/timeout/browser error). Stores reason in LastError.
func (s *Service) FinishJobWithOutcome(ctx context.Context, job *Job, reason string) error {
	if job == nil {
		return fmt.Errorf("nil job")
	}
	reason = strings.TrimSpace(reason)
	if reason != "" {
		job.Data.LastError = reason
	}
	n := s.CountCSVDataRows(job.ID)
	if n > 0 {
		job.Status = StatusOK
		if reason != "" {
			job.Data.LastError = fmt.Sprintf("interrupted: %s (kept %d rows)", reason, n)
		}
		return s.Update(ctx, job)
	}
	job.Status = StatusFailed
	return s.Update(ctx, job)
}

// SalvageFailedJobsWithResults flips failed→ok for jobs that already wrote CSV rows.
func (s *Service) SalvageFailedJobsWithResults(ctx context.Context) (int, error) {
	if s.repo == nil {
		return 0, nil
	}
	jobs, err := s.repo.Select(ctx, SelectParams{Status: StatusFailed})
	if err != nil {
		return 0, err
	}
	restored := 0
	for i := range jobs {
		j := jobs[i]
		n := s.CountCSVDataRows(j.ID)
		if n <= 0 {
			continue
		}
		if strings.TrimSpace(j.Data.LastError) == "" {
			j.Data.LastError = fmt.Sprintf("recovered from failed status (%d rows on disk)", n)
		}
		j.Status = StatusOK
		if err := s.Update(ctx, &j); err != nil {
			continue
		}
		restored++
	}
	return restored, nil
}

// TouchJob refreshes the working heartbeat timestamp.
func (s *Service) TouchJob(ctx context.Context, id string) error {
	type toucher interface {
		TouchJob(context.Context, string) error
	}
	if t, ok := s.repo.(toucher); ok {
		return t.TouchJob(ctx, id)
	}
	return nil
}

// AllForOwner lists jobs belonging to one invite-code tenant.
// When owner is empty, returns an empty list (never falls back to global listing).
func (s *Service) AllForOwner(ctx context.Context, owner string) ([]Job, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return []Job{}, nil
	}
	return s.repo.Select(ctx, SelectParams{Owner: owner})
}

func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	if s.repo == nil {
		return Job{}, fmt.Errorf("job repository not configured")
	}

	return s.repo.Get(ctx, id)
}

// GetOwned returns a job only if it belongs to owner.
func (s *Service) GetOwned(ctx context.Context, id, owner string) (Job, error) {
	job, err := s.Get(ctx, id)
	if err != nil {
		return Job{}, ErrJobNotFound
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || job.Owner != owner {
		return Job{}, ErrJobNotFound
	}
	return job, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	datapath, err := s.csvPath(id)
	if err != nil {
		return err
	}

	if _, err := os.Stat(datapath); err == nil {
		if err := os.Remove(datapath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return s.repo.Delete(ctx, id)
}

// DeleteOwned deletes a job only if it belongs to owner.
func (s *Service) DeleteOwned(ctx context.Context, id, owner string) error {
	if _, err := s.GetOwned(ctx, id, owner); err != nil {
		return err
	}
	return s.Delete(ctx, id)
}

func (s *Service) Update(ctx context.Context, job *Job) error {
	return s.repo.Update(ctx, job)
}

func (s *Service) SelectPending(ctx context.Context) ([]Job, error) {
	return s.repo.Select(ctx, SelectParams{Status: StatusPending, Limit: 1})
}

// ClaimPending atomically claims the next pending job (status → working).
func (s *Service) ClaimPending(ctx context.Context) (Job, error) {
	if s.repo == nil {
		return Job{}, fmt.Errorf("job repository not configured")
	}
	return s.repo.ClaimPending(ctx)
}

// csvPath returns the on-disk path of a job's CSV output, rejecting ids that
// could escape the data folder.
func (s *Service) csvPath(id string) (string, error) {
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return "", fmt.Errorf("invalid file name")
	}

	return filepath.Join(s.dataFolder, id+".csv"), nil
}

func (s *Service) GetCSV(_ context.Context, id string) (string, error) {
	datapath, err := s.csvPath(id)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(datapath); os.IsNotExist(err) {
		return "", fmt.Errorf("csv file not found for job %s", id)
	}

	return datapath, nil
}

// GetOwnedCSV returns the CSV path only when the job belongs to owner.
func (s *Service) GetOwnedCSV(ctx context.Context, id, owner string) (string, error) {
	if _, err := s.GetOwned(ctx, id, owner); err != nil {
		return "", err
	}
	path, err := s.GetCSV(ctx, id)
	if err != nil {
		return "", err
	}
	return path, nil
}

// IsJobNotFound reports whether err is a missing/forbidden job.
func IsJobNotFound(err error) bool {
	return errors.Is(err, ErrJobNotFound)
}
