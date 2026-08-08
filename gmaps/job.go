package gmaps

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
	"github.com/gosom/scrapemate"

	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/exiter"
)

type GmapJobOptions func(*GmapJob)

type GmapJob struct {
	scrapemate.Job

	MaxDepth     int
	LangCode     string
	ExtractEmail bool

	Deduper                 deduper.Deduper
	ExitMonitor             exiter.Exiter
	ExtractExtraReviews     bool
	WriterManagedCompletion bool

	// Keywords + job-level geo filter are plumbed into PlaceJobs so deep mode
	// can drop off-brief / out-of-radius hits before email spawn.
	Keywords      []string
	FilterLat     float64
	FilterLon     float64
	FilterRadiusM float64

	// streamedCount is PlaceJobs pushed mid-scroll (for ExitMonitor + logs).
	streamedCount int
	// seenLocal dedups when Deduper is nil (same seed only).
	seenLocal map[string]struct{}
}

type feedHit struct {
	Href  string
	Title string
}

const (
	// firstWaveScrolls: flush PlaceJobs after this many scrolls so free browser
	// workers start place details while the seed keeps scrolling.
	firstWaveScrolls = 5
	// streamEveryNScrolls: subsequent mid-scroll flushes.
	streamEveryNScrolls = 3
)

func NewGmapJob(
	id, langCode, query string,
	maxDepth int,
	_ bool,
	geoCoordinates string,
	zoom int,
	opts ...GmapJobOptions,
) *GmapJob {
	var mapURL string

	switch {
	case isGoogleMapsURL(query):
		mapURL = strings.TrimSpace(query)
	case geoCoordinates != "" && zoom > 0:
		query = url.QueryEscape(query)
		mapURL = fmt.Sprintf("https://www.google.com/maps/search/%s/@%s,%dz", query, strings.ReplaceAll(geoCoordinates, " ", ""), zoom)
	default:
		// Warning: geo and zoom MUST be both set or not
		query = url.QueryEscape(query)
		mapURL = fmt.Sprintf("https://www.google.com/maps/search/%s", query)
	}

	const (
		maxRetries = 3
		prio       = scrapemate.PriorityLow
	)

	if id == "" {
		id = uuid.New().String()
	}

	job := GmapJob{
		Job: scrapemate.Job{
			ID:         id,
			Method:     http.MethodGet,
			URL:        mapURL,
			URLParams:  map[string]string{"hl": langCode},
			MaxRetries: maxRetries,
			Priority:   prio,
		},
		MaxDepth: maxDepth,
		LangCode: langCode,
		// Product policy: contact enrichment is mandatory in every run mode.
		ExtractEmail: true,
	}

	for _, opt := range opts {
		opt(&job)
	}

	return &job
}

func WithDeduper(d deduper.Deduper) GmapJobOptions {
	return func(j *GmapJob) {
		j.Deduper = d
	}
}

func WithExitMonitor(e exiter.Exiter) GmapJobOptions {
	return func(j *GmapJob) {
		j.ExitMonitor = e
	}
}

func WithExtraReviews() GmapJobOptions {
	return func(j *GmapJob) {
		j.ExtractExtraReviews = true
	}
}

// WithGmapJobFilter attaches keyword + circle filters for spawned PlaceJobs.
func WithGmapJobFilter(keywords []string, lat, lon, radiusM float64) GmapJobOptions {
	return func(j *GmapJob) {
		j.Keywords = append([]string(nil), keywords...)
		j.FilterLat = lat
		j.FilterLon = lon
		j.FilterRadiusM = radiusM
	}
}

func WithWriterManagedCompletion() GmapJobOptions {
	return func(j *GmapJob) {
		j.WriterManagedCompletion = true
	}
}

func (j *GmapJob) UseInResults() bool {
	return false
}

func (j *GmapJob) ProcessOnFetchError() bool {
	return true
}

func (j *GmapJob) placeJobOpts() []PlaceJobOptions {
	jopts := []PlaceJobOptions{}
	if j.ExitMonitor != nil {
		jopts = append(jopts, WithPlaceJobExitMonitor(j.ExitMonitor))
	}
	if j.WriterManagedCompletion {
		jopts = append(jopts, WithPlaceJobWriterManagedCompletion())
	}
	if len(j.Keywords) > 0 || j.FilterRadiusM > 0 {
		jopts = append(jopts, WithPlaceJobFilter(j.Keywords, j.FilterLat, j.FilterLon, j.FilterRadiusM))
	}
	return jopts
}

func (j *GmapJob) claimPlaceURL(ctx context.Context, href string) bool {
	key := MapsURLDedupKey(href)
	if j.Deduper != nil {
		return j.Deduper.AddIfNotExists(ctx, key)
	}
	if j.seenLocal == nil {
		j.seenLocal = map[string]struct{}{}
	}
	if _, ok := j.seenLocal[key]; ok {
		return false
	}
	j.seenLocal[key] = struct{}{}
	return true
}

func (j *GmapJob) Process(ctx context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
	}()

	if resp.Error != nil {
		if j.ExitMonitor != nil {
			j.ExitMonitor.IncrSeedCompleted(1)
		}

		return nil, nil, resp.Error
	}

	log := scrapemate.GetLoggerFromContext(ctx)

	doc, ok := resp.Document.(*goquery.Document)
	if !ok {
		if j.ExitMonitor != nil {
			j.ExitMonitor.IncrSeedCompleted(1)
		}

		return nil, nil, fmt.Errorf("could not convert to goquery document")
	}

	var next []scrapemate.IJob
	opts := j.placeJobOpts()

	if strings.Contains(resp.URL, "/maps/place/") {
		if j.claimPlaceURL(ctx, resp.URL) {
			next = append(next, placeJobForURL(j.ID, j.LangCode, resp.URL, j.ExtractEmail, j.ExtractExtraReviews, opts))
		}
	} else {
		doc.Find(`div[role=feed] div[jsaction]>a`).Each(func(_ int, s *goquery.Selection) {
			if href := s.AttrOr("href", ""); href != "" {
				// Cheap feed-level skip: title from aria-label + coords in href.
				title := strings.TrimSpace(s.AttrOr("aria-label", ""))
				if feedHitShouldSkip(title, href, j.Keywords, j.FilterLat, j.FilterLon, j.FilterRadiusM) {
					return
				}
				if !j.claimPlaceURL(ctx, href) {
					return
				}
				next = append(next, placeJobForURL(j.ID, j.LangCode, href, j.ExtractEmail, j.ExtractExtraReviews, opts))
			}
		})
	}

	if j.ExitMonitor != nil {
		// Mid-scroll stream already counted streamedCount via IncrPlacesFound.
		j.ExitMonitor.IncrPlacesFound(len(next))
		j.ExitMonitor.IncrSeedCompleted(1)
	}

	total := j.streamedCount + len(next)
	log.Info(fmt.Sprintf("%d places found (%d streamed mid-scroll, %d at seed end)", total, j.streamedCount, len(next)))

	return nil, next, nil
}

func (j *GmapJob) BrowserActions(ctx context.Context, page scrapemate.BrowserPage) scrapemate.Response {
	var resp scrapemate.Response

	pageResponse, err := page.Goto(j.GetFullURL(), scrapemate.WaitUntilDOMContentLoaded)
	if err != nil {
		resp.Error = err

		return resp
	}

	clickRejectCookiesIfRequired(page)

	const defaultTimeout = 5 * time.Second

	// Ignore WaitForURL errors — Google Maps may redirect slowly especially via proxy
	_ = page.WaitForURL(page.URL(), defaultTimeout)

	resp.URL = pageResponse.URL
	resp.StatusCode = pageResponse.StatusCode
	resp.Headers = pageResponse.Headers

	// When Google Maps finds only 1 place, it slowly redirects to that place's URL
	// check element scroll
	sel := `div[role='feed']`

	err = page.WaitForSelector(sel, 10*time.Second)

	var singlePlace bool

	if err != nil {
		waitCtx, waitCancel := context.WithTimeout(ctx, time.Second*5)
		defer waitCancel()

		singlePlace = waitUntilURLContains(waitCtx, page, "/maps/place/")

		waitCancel()
	}

	if singlePlace {
		resp.URL = page.URL()

		var body string

		body, err = page.Content()
		if err != nil {
			resp.Error = err
			return resp
		}

		resp.Body = []byte(body)

		return resp
	}

	// First visible feed screen → stream PlaceJobs before any scroll (TTFP).
	if n := j.streamFeedPlaces(ctx, page); n > 0 {
		scrapemate.GetLoggerFromContext(ctx).Info(fmt.Sprintf("streamed %d places from first feed screen", n))
	}

	scrollSelector := `div[role='feed']`

	_, err = j.scrollAndStream(ctx, page, j.MaxDepth, scrollSelector)
	if err != nil {
		resp.Error = err

		return resp
	}

	// Catch links added on the last scroll that Process HTML may also see;
	// Deduper/claimPlaceURL makes a double-pass cheap.
	_ = j.streamFeedPlaces(ctx, page)

	body, err := page.Content()
	if err != nil {
		resp.Error = err
		return resp
	}

	resp.Body = []byte(body)

	return resp
}

// streamFeedPlaces enqueues new PlaceJobs from the current feed DOM via JobPusher.
// Returns how many jobs were newly pushed. Safe no-op without a pusher.
func (j *GmapJob) streamFeedPlaces(ctx context.Context, page scrapemate.BrowserPage) int {
	push := scrapemate.GetJobPusherFromContext(ctx)
	if push == nil {
		return 0
	}
	hits, err := collectFeedHits(page)
	if err != nil || len(hits) == 0 {
		return 0
	}
	opts := j.placeJobOpts()
	n := 0
	for _, hit := range hits {
		if feedHitShouldSkip(hit.Title, hit.Href, j.Keywords, j.FilterLat, j.FilterLon, j.FilterRadiusM) {
			continue
		}
		if !j.claimPlaceURL(ctx, hit.Href) {
			continue
		}
		placeJob := placeJobForURL(j.ID, j.LangCode, hit.Href, j.ExtractEmail, j.ExtractExtraReviews, opts)
		// Beat remaining Low-priority grid seeds so free browser workers paint
		// the first rows instead of starting another Maps list scroll.
		if pj, ok := placeJob.(*PlaceJob); ok {
			pj.Priority = scrapemate.PriorityHigh
		}
		if cj, ok := placeJob.(*CachedPlaceJob); ok {
			cj.Priority = scrapemate.PriorityHigh
		}
		if err := push(ctx, placeJob); err != nil {
			// Release claim so Process can retry this URL later.
			j.releasePlaceURL(hit.Href)
			break
		}
		n++
	}
	if n > 0 {
		j.streamedCount += n
		if j.ExitMonitor != nil {
			j.ExitMonitor.IncrPlacesFound(n)
		}
	}
	return n
}

func (j *GmapJob) releasePlaceURL(href string) {
	key := MapsURLDedupKey(href)
	if j.Deduper != nil {
		// Deduper has no remove API; rare Push failure — Process may miss this
		// URL for the rest of the run. Acceptable vs blocking the scroll loop.
		_ = key
		return
	}
	if j.seenLocal != nil {
		delete(j.seenLocal, key)
	}
}

func collectFeedHits(page scrapemate.BrowserPage) ([]feedHit, error) {
	raw, err := page.Eval(`() => {
		const out = [];
		const nodes = document.querySelectorAll("div[role='feed'] div[jsaction] > a");
		for (const a of nodes) {
			const href = a.getAttribute("href") || "";
			if (!href) continue;
			out.push({
				href: href,
				title: (a.getAttribute("aria-label") || "").trim(),
			});
		}
		return out;
	}`)
	if err != nil {
		return nil, err
	}
	return parseFeedHits(raw), nil
}

func parseFeedHits(raw any) []feedHit {
	arr, ok := raw.([]any)
	if !ok {
		// playwright-go sometimes returns []interface{} under a different alias
		if slice, ok2 := raw.([]interface{}); ok2 {
			arr = slice
		} else {
			return nil
		}
	}
	out := make([]feedHit, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			if m2, ok2 := item.(map[string]interface{}); ok2 {
				m = m2
			} else {
				continue
			}
		}
		href, _ := m["href"].(string)
		title, _ := m["title"].(string)
		href = strings.TrimSpace(href)
		if href == "" {
			continue
		}
		out = append(out, feedHit{Href: href, Title: strings.TrimSpace(title)})
	}
	return out
}

func waitUntilURLContains(ctx context.Context, page scrapemate.BrowserPage, s string) bool {
	ticker := time.NewTicker(time.Millisecond * 150)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if strings.Contains(page.URL(), s) {
				return true
			}
		}
	}
}

func clickRejectCookiesIfRequired(page scrapemate.BrowserPage) {
	// Use JavaScript to find and click - faster than multiple locator calls
	_, _ = page.Eval(`() => {
		// Try consent form buttons first
		const consentForm = document.querySelector('form[action*="consent.google"]');
		if (consentForm) {
			const btn = consentForm.querySelector('button, input[type="submit"]');
			if (btn) {
				btn.click();
				return true;
			}
		}
		// Try reject/decline buttons
		const buttons = document.querySelectorAll('button, input[type="submit"]');
		for (const btn of buttons) {
			const text = (btn.textContent || btn.value || '').toLowerCase();
			if (text.includes('reject') || text.includes('decline') || text.includes('ablehnen')) {
				btn.click();
				return true;
			}
		}
		return false;
	}`)
}

func scroll(ctx context.Context,
	page scrapemate.BrowserPage,
	maxDepth int,
	scrollSelector string,
) (int, error) {
	// Kept for tests / callers that only need scrolling without streaming.
	dummy := &GmapJob{}
	return dummy.scrollAndStream(ctx, page, maxDepth, scrollSelector)
}

func (j *GmapJob) scrollAndStream(ctx context.Context,
	page scrapemate.BrowserPage,
	maxDepth int,
	scrollSelector string,
) (int, error) {
	expr := `async () => {
		const el = document.querySelector("` + scrollSelector + `");
		el.scrollTop = el.scrollHeight;

		return new Promise((resolve, reject) => {
  			setTimeout(() => {
    			resolve(el.scrollHeight);
  			}, %d);
		});
	}`

	var currentScrollHeight int
	// 提速：初始等待时间从 100ms 降到 50ms，更快开始滚动
	waitTime := 50.
	cnt := 0

	const (
		timeout  = 400
		maxWait2 = 1200 // 滚动等待上限：不影响列表质量，缩短空等
	)

	for i := 0; i < maxDepth; i++ {
		cnt++
		waitTime2 := timeout * cnt

		if waitTime2 > timeout {
			waitTime2 = maxWait2
		}

		// Scroll to the bottom of the page.
		scrollHeight, err := page.Eval(fmt.Sprintf(expr, waitTime2))
		if err != nil {
			return cnt, err
		}

		// Handle both int and float64 because browser-evaluated numbers may arrive as either type.
		var height int
		switch v := scrollHeight.(type) {
		case int:
			height = v
		case float64:
			height = int(v)
		default:
			return cnt, fmt.Errorf("scrollHeight is not a number, got %T", scrollHeight)
		}

		if height == currentScrollHeight {
			break
		}

		currentScrollHeight = height

		// First-wave + periodic mid-scroll PlaceJob flush (TTFP).
		if cnt == firstWaveScrolls || (cnt > firstWaveScrolls && (cnt-firstWaveScrolls)%streamEveryNScrolls == 0) {
			if n := j.streamFeedPlaces(ctx, page); n > 0 {
				scrapemate.GetLoggerFromContext(ctx).Info(
					fmt.Sprintf("streamed %d places after scroll %d (total streamed %d)", n, cnt, j.streamedCount),
				)
			}
		}

		select {
		case <-ctx.Done():
			return currentScrollHeight, nil
		default:
		}

		// 提速：增长系数从 1.5 降到 1.3，更快达到最大等待时间
		waitTime *= 1.3

		if waitTime > maxWait2 {
			waitTime = maxWait2
		}

		page.WaitForTimeout(time.Duration(waitTime) * time.Millisecond)
	}

	return cnt, nil
}

func isGoogleMapsURL(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil {
			return false
		}

		host := strings.ToLower(u.Hostname())
		if host == "maps.app.goo.gl" {
			return true
		}

		return (host == "google.com" || strings.HasSuffix(host, ".google.com")) &&
			(strings.Contains(u.EscapedPath(), "/maps") || strings.Contains(u.Path, "/maps"))
	}

	if strings.HasPrefix(s, "maps.app.goo.gl") {
		return true
	}

	return false
}
