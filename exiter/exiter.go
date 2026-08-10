package exiter

import (
	"context"
	"sync"
	"time"
)

// Progress is a point-in-time snapshot of scrape/email completion counters.
type Progress struct {
	SeedCount        int
	SeedCompleted    int
	PlacesFound      int
	PlacesCompleted  int
	MapsPlacesDone   int
}

// Exiter cancels the scrape context once all seeds and place/email work finish.
type Exiter interface {
	SetSeedCount(int)
	SetCancelFunc(context.CancelFunc)
	IncrSeedCompleted(int)
	IncrPlacesFound(int)
	IncrPlacesCompleted(int)
	// IncrMapsPlacesDone counts a PlaceJob finished (CSV-ready) even when an
	// EmailJob is still enriching contacts. Used to free fair-admission slots
	// before website fetches complete.
	IncrMapsPlacesDone(int)
	// Snapshot returns current counters (for UI phase / early StatusOK).
	Snapshot() Progress
	// SeedsFinished reports whether every Maps search seed has completed
	// (place rows may already be on disk while website email jobs still run).
	SeedsFinished() bool
	// MapsPlacesFinished reports seeds done and every discovered place has
	// finished its Playwright PlaceJob (emails may still be in flight).
	MapsPlacesFinished() bool
	Run(context.Context)
}

type exiter struct {
	seedCount       int
	seedCompleted   int
	placesFound     int
	placesCompleted int
	mapsPlacesDone  int

	mu         *sync.Mutex
	cancelFunc context.CancelFunc
	doneCh     chan struct{}
	mapsDoneCh chan struct{}
}

// New returns an Exiter that signals cancel when seeds and place work are done.
func New() Exiter {
	return &exiter{
		mu:         &sync.Mutex{},
		doneCh:     make(chan struct{}, 1),
		mapsDoneCh: make(chan struct{}, 1),
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
	mapsDone := e.seedCount > 0 && e.seedCompleted >= e.seedCount && e.mapsPlacesDone >= e.placesFound
	e.mu.Unlock()

	if done {
		select {
		case e.doneCh <- struct{}{}:
		default:
		}
	}
	if mapsDone {
		select {
		case e.mapsDoneCh <- struct{}{}:
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

func (e *exiter) IncrMapsPlacesDone(val int) {
	e.mu.Lock()
	e.mapsPlacesDone += val
	mapsDone := e.seedCount > 0 && e.seedCompleted >= e.seedCount && e.mapsPlacesDone >= e.placesFound
	e.mu.Unlock()

	if mapsDone {
		select {
		case e.mapsDoneCh <- struct{}{}:
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
		MapsPlacesDone:  e.mapsPlacesDone,
	}
}

func (e *exiter) SeedsFinished() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.seedCount > 0 && e.seedCompleted >= e.seedCount
}

func (e *exiter) MapsPlacesFinished() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.seedCount > 0 && e.seedCompleted >= e.seedCount && e.mapsPlacesDone >= e.placesFound
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

// WaitMapsPlacesFinished blocks until Maps PlaceJobs finish (or ctx ends).
// Used by webrunner to free fair-admission slots while emails continue.
func WaitMapsPlacesFinished(ctx context.Context, e Exiter) bool {
	if e == nil {
		return false
	}
	if e.MapsPlacesFinished() {
		return true
	}
	impl, ok := e.(*exiter)
	if !ok {
		// Fallback poll for alternate implementations / mocks.
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return e.MapsPlacesFinished()
			case <-t.C:
				if e.MapsPlacesFinished() {
					return true
				}
			}
		}
	}
	select {
	case <-ctx.Done():
		return impl.MapsPlacesFinished()
	case <-impl.mapsDoneCh:
		return true
	}
}
