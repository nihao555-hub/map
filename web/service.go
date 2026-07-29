package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Service struct {
	repo       JobRepository
	dataFolder string
	progressMu sync.RWMutex
	started    map[string]time.Time
	finished   map[string]time.Time
}

func NewService(repo JobRepository, dataFolder string) *Service {
	return &Service{
		repo:       repo,
		dataFolder: dataFolder,
		started:    make(map[string]time.Time),
		finished:   make(map[string]time.Time),
	}
}

func (s *Service) Create(ctx context.Context, job *Job) error {
	return s.repo.Create(ctx, job)
}

func (s *Service) All(ctx context.Context) ([]Job, error) {
	return s.repo.Select(ctx, SelectParams{})
}

func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	return s.repo.Get(ctx, id)
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

	err = s.repo.Delete(ctx, id)
	if err == nil {
		s.progressMu.Lock()
		delete(s.started, id)
		delete(s.finished, id)
		s.progressMu.Unlock()
	}

	return err
}

func (s *Service) Update(ctx context.Context, job *Job) error {
	return s.repo.Update(ctx, job)
}

func (s *Service) SelectPending(ctx context.Context) ([]Job, error) {
	return s.repo.Select(ctx, SelectParams{Status: StatusPending, Limit: 1})
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

func (s *Service) MarkStarted(id string, at time.Time) {
	s.progressMu.Lock()
	s.started[id] = at.UTC()
	delete(s.finished, id)
	s.progressMu.Unlock()
}

func (s *Service) MarkFinished(id string, at time.Time) {
	s.progressMu.Lock()
	s.finished[id] = at.UTC()
	s.progressMu.Unlock()
}

func (s *Service) Progress(ctx context.Context, id string) (JobProgress, error) {
	job, err := s.Get(ctx, id)
	if err != nil {
		return JobProgress{}, err
	}

	count, latest, err := s.readProgressCSV(id)
	if err != nil {
		return JobProgress{}, err
	}

	s.progressMu.RLock()
	started := s.started[id]
	finished := s.finished[id]
	s.progressMu.RUnlock()

	if started.IsZero() && !job.Date.IsZero() {
		started = job.Date
	}

	if finished.IsZero() && job.Status != StatusWorking {
		if path, pathErr := s.csvPath(id); pathErr == nil {
			if info, statErr := os.Stat(path); statErr == nil {
				finished = info.ModTime().UTC()
			}
		}
	}

	progress := JobProgress{
		Status:      job.Status,
		Count:       count,
		LatestNames: latest,
	}

	if !started.IsZero() {
		progress.StartedAt = started.Format(time.RFC3339)
		end := time.Now().UTC()

		if !finished.IsZero() {
			end = finished
		}

		if end.After(started) {
			progress.ElapsedSeconds = int64(end.Sub(started).Seconds())
		}
	}

	return progress, nil
}

func (s *Service) readProgressCSV(id string) (resultCount int, resultLatest []string, err error) {
	path, err := s.csvPath(id)
	if err != nil {
		return 0, nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil, nil
		}

		return 0, nil, err
	}
	defer file.Close()

	return countCSVResults(file)
}
