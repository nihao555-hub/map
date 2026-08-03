package gmaps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gosom/scrapemate"

	"github.com/gosom/google-maps-scraper/exiter"
)

type PlaceJobOptions func(*PlaceJob)

type PlaceJob struct {
	scrapemate.Job

	UsageInResults          bool
	ExtractEmail            bool
	ExitMonitor             exiter.Exiter
	ExtractExtraReviews     bool
	WriterManagedCompletion bool
}

func NewPlaceJob(parentID, langCode, u string, extractEmail, extraExtraReviews bool, opts ...PlaceJobOptions) *PlaceJob {
	const (
		defaultPrio       = scrapemate.PriorityMedium
		defaultMaxRetries = 3
	)

	job := PlaceJob{
		Job: scrapemate.Job{
			ID:         uuid.New().String(),
			ParentID:   parentID,
			Method:     "GET",
			URL:        u,
			URLParams:  map[string]string{"hl": langCode},
			MaxRetries: defaultMaxRetries,
			Priority:   defaultPrio,
		},
	}

	job.UsageInResults = true
	job.ExtractEmail = extractEmail
	job.ExtractExtraReviews = extraExtraReviews

	for _, opt := range opts {
		opt(&job)
	}

	return &job
}

func WithPlaceJobExitMonitor(exitMonitor exiter.Exiter) PlaceJobOptions {
	return func(j *PlaceJob) {
		j.ExitMonitor = exitMonitor
	}
}

func WithPlaceJobWriterManagedCompletion() PlaceJobOptions {
	return func(j *PlaceJob) {
		j.WriterManagedCompletion = true
	}
}

func (j *PlaceJob) ProcessOnFetchError() bool {
	return true
}

func (j *PlaceJob) Process(_ context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
		resp.Meta = nil
	}()

	if resp.Error != nil {
		if j.ExitMonitor != nil {
			j.ExitMonitor.IncrPlacesCompleted(1)
		}

		return nil, nil, resp.Error
	}

	raw, ok := resp.Meta["json"].([]byte)
	if !ok {
		// 纯 HTTP 抓取路径：没有浏览器注入的 Meta，直接从 HTML 里的
		// window.APP_INITIALIZATION_STATE 字面量提取同样的地点 JSON
		var err error

		raw, err = extractJSONFromBody(resp.Body)
		if err != nil {
			if j.ExitMonitor != nil {
				j.ExitMonitor.IncrPlacesCompleted(1)
			}

			return nil, nil, fmt.Errorf("could not convert to []byte: %w", err)
		}
	}

	entry, err := EntryFromJSON(raw)
	if err != nil {
		if j.ExitMonitor != nil {
			j.ExitMonitor.IncrPlacesCompleted(1)
		}

		return nil, nil, err
	}

	entry.ID = j.ParentID

	if entry.Link == "" {
		entry.Link = j.GetURL()
	}

	// Handle RPC-based reviews
	allReviewsRaw, ok := resp.Meta["reviews_raw"].(FetchReviewsResponse)
	if ok && len(allReviewsRaw.pages) > 0 {
		entry.AddExtraReviews(allReviewsRaw.pages)
	}

	// Handle DOM-based reviews (fallback)
	domReviews, ok := resp.Meta["dom_reviews"].([]DOMReview)
	if ok && len(domReviews) > 0 {
		convertedReviews := ConvertDOMReviewsToReviews(domReviews)

		deduped := dedupeDOMReviewsAgainstPrimary(entry.UserReviews, convertedReviews)
		if len(deduped) != len(convertedReviews) {
			log.Printf("DOM reviews: dropped %d of %d already present in user_reviews",
				len(convertedReviews)-len(deduped), len(convertedReviews))
		}

		entry.UserReviewsExtended = append(entry.UserReviewsExtended, deduped...)
	}

	if j.ExtractEmail && entry.IsWebsiteValidForEmail() {
		opts := []EmailExtractJobOptions{}
		// SaaS writer 路径：完成计数由 writer 负责。
		// Web 路径：地点一经解析就算完成，邮箱异步 upsert，避免 ExitMonitor 被官网爬取拖死。
		if j.WriterManagedCompletion {
			opts = append(opts, WithEmailJobWriterManagedCompletion())
		} else if j.ExitMonitor != nil {
			j.ExitMonitor.IncrPlacesCompleted(1)
		}

		emailJob := NewEmailJob(j.ID, &entry, opts...)

		// 先写出地点详情，邮箱任务稍后 upsert 补联系方式。
		return &entry, []scrapemate.IJob{emailJob}, nil
	} else if j.ExitMonitor != nil && !j.WriterManagedCompletion {
		j.ExitMonitor.IncrPlacesCompleted(1)
	}

	return &entry, nil, err
}

func (j *PlaceJob) BrowserActions(ctx context.Context, page scrapemate.BrowserPage) scrapemate.Response {
	var resp scrapemate.Response

	pageResponse, err := page.Goto(j.GetURL(), scrapemate.WaitUntilDOMContentLoaded)
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

	raw, err := j.extractJSON(page)
	if err != nil {
		resp.Error = err

		return resp
	}

	if resp.Meta == nil {
		resp.Meta = make(map[string]any)
	}

	resp.Meta["json"] = raw

	if j.ExtractExtraReviews {
		reviewCount := j.getReviewCount(raw)
		if reviewCount > 0 { // download reviews for any place that has them
			params := fetchReviewsParams{
				page:        page,
				mapURL:      page.URL(),
				reviewCount: reviewCount,
			}

			// Use the new fallback mechanism that tries RPC first, then DOM
			rpcData, domReviews, err := FetchReviewsWithFallback(ctx, params)

			switch {
			case err != nil:
				fmt.Printf("Warning: review extraction failed: %v\n", err)
			case len(rpcData.pages) > 0:
				resp.Meta["reviews_raw"] = rpcData
			case len(domReviews) > 0:
				resp.Meta["dom_reviews"] = domReviews
			}
		}
	}

	return resp
}

func (j *PlaceJob) getRaw(ctx context.Context, page scrapemate.BrowserPage) (any, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout while getting raw data: %w", ctx.Err())
		default:
			raw, err := page.Eval(js)
			if err != nil {
				// 提速：轮询间隔从 200ms 降到 100ms，更快检测到数据就绪
				<-time.After(time.Millisecond * 100)
				continue
			}

			// Check for valid non-null result.
			// JS null may arrive as nil, and empty strings are not useful here.
			if raw == nil {
				<-time.After(time.Millisecond * 100)
				continue
			}

			// If it's a string, make sure it's not empty
			if str, ok := raw.(string); ok {
				if str == "" {
					<-time.After(time.Millisecond * 100)
					continue
				}
			}

			return raw, nil
		}
	}
}

func (j *PlaceJob) extractJSON(page scrapemate.BrowserPage) ([]byte, error) {
	const maxRetries = 2

	for attempt := range maxRetries {
		// 提速：超时从 30s 降到 15s，正常页面 3-5 秒就出数据了
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		rawI, err := j.getRaw(ctx, page)

		cancel()

		if err != nil {
			// On timeout, try reloading the page
			if attempt < maxRetries-1 {
				if reloadErr := page.Reload(scrapemate.WaitUntilDOMContentLoaded); reloadErr == nil {
					continue
				}
			}

			return nil, err
		}

		if rawI == nil {
			if attempt < maxRetries-1 {
				if reloadErr := page.Reload(scrapemate.WaitUntilDOMContentLoaded); reloadErr == nil {
					continue
				}
			}

			return nil, fmt.Errorf("APP_INITIALIZATION_STATE data not found")
		}

		raw, ok := rawI.(string)
		if !ok {
			return nil, fmt.Errorf("could not convert to string, got type %T", rawI)
		}

		const prefix = `)]}'`

		raw = strings.TrimSpace(strings.TrimPrefix(raw, prefix))

		return []byte(raw), nil
	}

	return nil, fmt.Errorf("APP_INITIALIZATION_STATE data not found after retries")
}

// extractJSONFromBody 从纯 HTTP 拿到的 HTML 里提取地点 JSON，
// 与浏览器版 extractJSON 的 JS 逻辑等价：
// window.APP_INITIALIZATION_STATE=<literal>; 取 [3]，遍历其中的数组，
// 找下标 6/5 处由 ")]}'" 开头的 JSON 字符串。
func extractJSONFromBody(body []byte) ([]byte, error) {
	const marker = "window.APP_INITIALIZATION_STATE="

	start := bytes.Index(body, []byte(marker))
	if start < 0 {
		return nil, fmt.Errorf("APP_INITIALIZATION_STATE marker not found in body")
	}

	literal := body[start+len(marker):]

	// 字面量以 ;window. 结束
	end := bytes.Index(literal, []byte(";window."))
	if end < 0 {
		return nil, fmt.Errorf("APP_INITIALIZATION_STATE literal end not found")
	}

	literal = literal[:end]

	var state []any
	if err := json.Unmarshal(literal, &state); err != nil {
		return nil, fmt.Errorf("parse APP_INITIALIZATION_STATE: %w", err)
	}

	if len(state) <= 3 || state[3] == nil {
		return nil, fmt.Errorf("APP_INITIALIZATION_STATE[3] missing")
	}

	const prefix = `)]}'`

	findInArr := func(arr []any) []byte {
		for _, idx := range []int{6, 5} {
			if idx >= len(arr) {
				continue
			}

			if s, ok := arr[idx].(string); ok && strings.HasPrefix(s, prefix) {
				return []byte(strings.TrimSpace(strings.TrimPrefix(s, prefix)))
			}
		}

		return nil
	}

	switch appState := state[3].(type) {
	case map[string]any:
		for _, v := range appState {
			if arr, ok := v.([]any); ok {
				if raw := findInArr(arr); raw != nil {
					return raw, nil
				}
			}
		}
	case []any:
		// 原始 HTML 中地点 JSON 常直接挂在 state[3][5]/[6]
		if raw := findInArr(appState); raw != nil {
			return raw, nil
		}

		// 兜底：与浏览器 JS 一致，遍历嵌套数组
		for _, v := range appState {
			if arr, ok := v.([]any); ok {
				if raw := findInArr(arr); raw != nil {
					return raw, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("place JSON not found in APP_INITIALIZATION_STATE")
}

func (j *PlaceJob) getReviewCount(data []byte) int {
	tmpEntry, err := EntryFromJSON(data, true)
	if err != nil {
		return 0
	}

	return tmpEntry.ReviewCount
}

func (j *PlaceJob) UseInResults() bool {
	return j.UsageInResults
}

const js = `
(function() {
	if (!window.APP_INITIALIZATION_STATE || !window.APP_INITIALIZATION_STATE[3]) {
		return null;
	}
	const appState = window.APP_INITIALIZATION_STATE[3];

	// Collect all candidate JSON strings, then validate shape.
	// Google sometimes injects a compact variant first (jd[6] === null);
	// only the classic variant (jd[6] is an array) contains full place data.
	const candidates = [];
	for (const key of Object.keys(appState)) {
		const arr = appState[key];
		if (Array.isArray(arr)) {
			for (const idx of [6, 5]) {
				const item = arr[idx];
				if (typeof item === 'string' && item.startsWith(")]}'")) {
					candidates.push(item);
				}
			}
		}
	}
	for (const item of candidates) {
		try {
			const jd = JSON.parse(item.slice(4));
			if (Array.isArray(jd) && Array.isArray(jd[6])) {
				return item;
			}
		} catch (e) {}
	}
	return null;
})()
`
