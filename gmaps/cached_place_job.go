package gmaps

import (
	"context"

	"github.com/google/uuid"
	"github.com/gosom/scrapemate"

	"github.com/gosom/google-maps-scraper/exiter"
	"github.com/gosom/google-maps-scraper/placecache"
)

// CachedPlaceJob materializes a place from the cross-job cache without Playwright.
// URL is intentionally NOT a google.com/maps link so scrapemate routes it to the
// HTTP worker pool (see jobNeedsBrowser).
type CachedPlaceJob struct {
	scrapemate.Job

	Cached                  *placecache.CachedPlace
	UsageInResults          bool
	ExtractEmail            bool
	ExitMonitor             exiter.Exiter
	WriterManagedCompletion bool
	Keywords                []string
	FilterLat               float64
	FilterLon               float64
	FilterRadiusM           float64
	MapsURL                 string
}

// NewCachedPlaceJob builds an HTTP-pool job from a cache hit.
func NewCachedPlaceJob(parentID, mapsURL string, cached *placecache.CachedPlace, extractEmail bool, opts ...PlaceJobOptions) *CachedPlaceJob {
	j := &CachedPlaceJob{
		Job: scrapemate.Job{
			ID:       uuid.New().String(),
			ParentID: parentID,
			Method:   "GET",
			// Non-Maps URL → HTTP pool; real Maps link kept in MapsURL/Cached.Link.
			URL:        "https://place-cache.local/hit",
			MaxRetries: 1,
			Priority:   scrapemate.PriorityHigh,
		},
		Cached:         cached,
		UsageInResults: true,
		ExtractEmail:   extractEmail,
		MapsURL:        mapsURL,
	}
	// Apply PlaceJob options that overlap (keywords/filter/exit monitor).
	tmp := &PlaceJob{}
	for _, opt := range opts {
		opt(tmp)
	}
	j.ExitMonitor = tmp.ExitMonitor
	j.WriterManagedCompletion = tmp.WriterManagedCompletion
	j.Keywords = tmp.Keywords
	j.FilterLat = tmp.FilterLat
	j.FilterLon = tmp.FilterLon
	j.FilterRadiusM = tmp.FilterRadiusM
	return j
}

func (j *CachedPlaceJob) Process(_ context.Context, _ *scrapemate.Response) (any, []scrapemate.IJob, error) {
	entry := cachedToEntry(j.Cached)
	if entry == nil {
		if j.ExitMonitor != nil && !j.WriterManagedCompletion {
			j.ExitMonitor.IncrMapsPlacesDone(1)
			j.ExitMonitor.IncrPlacesCompleted(1)
		}
		return nil, nil, nil
	}
	entry.ID = j.ParentID
	if entry.Link == "" {
		entry.Link = j.MapsURL
	}
	entry.EnrichContactsFromMapsFields()
	if placeEntryShouldDrop(entry, j.Keywords, j.FilterLat, j.FilterLon, j.FilterRadiusM) {
		if j.ExitMonitor != nil && !j.WriterManagedCompletion {
			j.ExitMonitor.IncrMapsPlacesDone(1)
			j.ExitMonitor.IncrPlacesCompleted(1)
		}
		j.UsageInResults = false
		return nil, nil, nil
	}
	if j.ExitMonitor != nil && !j.WriterManagedCompletion {
		j.ExitMonitor.IncrMapsPlacesDone(1)
	}
	if j.ExtractEmail && entry.IsWebsiteValidForEmail() && len(entry.Emails) == 0 {
		opts := []EmailExtractJobOptions{}
		if j.WriterManagedCompletion {
			opts = append(opts, WithEmailJobWriterManagedCompletion())
		} else if j.ExitMonitor != nil {
			opts = append(opts, WithEmailJobExitMonitor(j.ExitMonitor))
		}
		entryCopy := *entry
		return entry, []scrapemate.IJob{NewEmailJob(j.ID, &entryCopy, opts...)}, nil
	}
	if j.ExitMonitor != nil && !j.WriterManagedCompletion {
		j.ExitMonitor.IncrPlacesCompleted(1)
	}
	return entry, nil, nil
}

// BrowserActions is unused (HTTP pool) but required by scrapemate.IJob.
func (j *CachedPlaceJob) BrowserActions(_ context.Context, _ scrapemate.BrowserPage) scrapemate.Response {
	return scrapemate.Response{StatusCode: 200, URL: j.GetURL()}
}

func (j *CachedPlaceJob) UseInResults() bool { return j.UsageInResults }

// ProcessOnFetchError lets Process run even if the dummy cache URL fails DNS/HTTP.
func (j *CachedPlaceJob) ProcessOnFetchError() bool { return true }

// placeJobForURL returns a cache-backed HTTP job when possible, else Playwright PlaceJob.
func placeJobForURL(parentID, langCode, href string, extractEmail, extraReviews bool, opts []PlaceJobOptions) scrapemate.IJob {
	if cached := placecache.LookupURL(href); cached != nil && cached.Title != "" {
		return NewCachedPlaceJob(parentID, href, cached, extractEmail, opts...)
	}
	return NewPlaceJob(parentID, langCode, href, extractEmail, extraReviews, opts...)
}
