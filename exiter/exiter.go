package exiter

import (
	"context"
	"sync"
)

// Progress is a point-in-time snapshot of scrape/email completion counters.
type Progress struct {
	SeedCount       int
	SeedCompleted   int
	PlacesFound     int
	PlacesCompleted int
}

// Exiter cancels the scrape context once all seeds and place/email work finish.
type Exiter interface {
	SetSeedCount(int)
	SetCancelFunc(context.CancelFunc)
	IncrSeedCompleted(int)
	IncrPlacesFound(int)
	IncrPlacesCompleted(int)
	// Snapshot returns current counters (for UI phase / early StatusOK).
	Snapshot() Progress
	// SeedsFinished reports whether every Maps search seed has completed
	// (place rows may already be on disk while website email jobs still run).
	SeedsFinished() bool
	Run(context.Context)
}

type exiter struct {
	seedCount       int
	seedCompleted   int
	placesFound     int
	placesCompleted int

	mu         *sync.Mutex
	cancelFunc context.CancelFunc
	doneCh     chan struct{}
}

// New returns an Exiter that signals cancel when seeds and place work are done.
func New() Exiter {
	return &exiter{
		mu:     &sync.Mutex{},
		doneCh: make(chan struct{}, 1),
	}
}

func (e *exiter) SetSeedCount(val int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.seedCount = val
}

func (e *exiter) SetCancelFunc(fn context.CancelFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.cancelFunc = fn
}

func (e *exiter) IncrSeedCompleted(val int) {
	e.mu.Lock()
	e.seedCompleted += val
	done := e.seedCompleted >= e.seedCount && e.placesCompleted >= e.placesFound
	e.mu.Unlock()

	if done {
		select {
		case e.doneCh <- struct{}{}:
		default:
		}
	}
}

func (e *exiter) IncrPlacesFound(val int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.placesFound += val
}

func (e *exiter) IncrPlacesCompleted(val int) {
	e.mu.Lock()
	e.placesCompleted += val
	done := e.seedCompleted >= e.seedCount && e.placesCompleted >= e.placesFound
	e.mu.Unlock()

	if done {
		select {
		case e.doneCh <- struct{}{}:
		default:
		}
	}
}

func (e *exiter) Snapshot() Progress {
	e.mu.Lock()
	defer e.mu.Unlock()

	return Progress{
		SeedCount:       e.seedCount,
		SeedCompleted:   e.seedCompleted,
		PlacesFound:     e.placesFound,
		PlacesCompleted: e.placesCompleted,
	}
}

func (e *exiter) SeedsFinished() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.seedCount > 0 && e.seedCompleted >= e.seedCount
}

func (e *exiter) Run(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-e.doneCh:
		if e.cancelFunc != nil {
			e.cancelFunc()
		}
	}
}
