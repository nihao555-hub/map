package web

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	return s.repo.Create(ctx, job)
}

func (s *Service) All(ctx context.Context) ([]Job, error) {
	return s.repo.Select(ctx, SelectParams{})
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
