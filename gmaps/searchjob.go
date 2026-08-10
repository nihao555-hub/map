package gmaps

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/enrich/intel"
	"github.com/gosom/google-maps-scraper/exiter"
	"github.com/gosom/scrapemate"
)

type SearchJobOptions func(*SearchJob)

type MapLocation struct {
	Lat     float64
	Lon     float64
	ZoomLvl float64
	Radius  float64
}

type MapSearchParams struct {
	Location  MapLocation
	Query     string
	ViewportW int
	ViewportH int
	Hl        string
	// Offset 分页偏移（每页固定 20 条），0 为第一页
	Offset int
}

type SearchJob struct {
	scrapemate.Job

	params                  *MapSearchParams
	ExitMonitor             exiter.Exiter
	WriterManagedCompletion bool
	Deduper                 deduper.Deduper
	ExtractEmail            bool
	Research                ResearchOptions
}

func NewSearchJob(params *MapSearchParams, opts ...SearchJobOptions) *SearchJob {
	const (
		defaultPrio       = scrapemate.PriorityMedium
		defaultMaxRetries = 3
		baseURL           = "https://maps.google.com/search"
	)

	job := SearchJob{
		Job: scrapemate.Job{
			ID:         uuid.New().String(),
			Method:     http.MethodGet,
			URL:        baseURL,
			URLParams:  buildGoogleMapsParams(params),
			MaxRetries: defaultMaxRetries,
			Priority:   defaultPrio,
		},
	}

	job.params = params

	for _, opt := range opts {
		opt(&job)
	}

	return &job
}

func WithSearchJobExitMonitor(exitMonitor exiter.Exiter) SearchJobOptions {
	return func(j *SearchJob) {
		j.ExitMonitor = exitMonitor
	}
}

func WithSearchJobWriterManagedCompletion() SearchJobOptions {
	return func(j *SearchJob) {
		j.WriterManagedCompletion = true
	}
}

func WithSearchJobDeduper(d deduper.Deduper) SearchJobOptions {
	return func(j *SearchJob) {
		j.Deduper = d
	}
}

func WithSearchJobEmail() SearchJobOptions {
	return func(j *SearchJob) {
		j.ExtractEmail = true
	}
}

// WithSearchJobCompanyResearch replaces email-only extraction with a full
// background-research crawl of each business website.
func WithSearchJobCompanyResearch(maxPages int) SearchJobOptions {
	return func(j *SearchJob) {
		j.Research = ResearchOptions{Enabled: true, MaxPages: maxPages}
		j.ExtractEmail = true
	}
}

// WithSearchJobEnricher attaches the shared external-intel enricher.
func WithSearchJobEnricher(enricher *intel.Enricher) SearchJobOptions {
	return func(j *SearchJob) {
		j.Research.Enricher = enricher
		if enricher != nil {
			j.Research.Enabled = true
			j.ExtractEmail = true
		}
	}
}

func (j *SearchJob) ProcessOnFetchError() bool {
	return true
}

func (j *SearchJob) Process(_ context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
		resp.Meta = nil
	}()

	if resp.Error != nil {
		if j.ExitMonitor != nil {
			j.ExitMonitor.IncrSeedCompleted(1)
		}

		return nil, nil, resp.Error
	}

	body := removeFirstLine(resp.Body)
	if len(body) == 0 {
		if j.ExitMonitor != nil {
			j.ExitMonitor.IncrSeedCompleted(1)
		}

		return nil, nil, fmt.Errorf("empty response body")
	}

	entries, err := ParseSearchResults(body)
	if err != nil {
		if j.ExitMonitor != nil {
			j.ExitMonitor.IncrSeedCompleted(1)
		}

		return nil, nil, fmt.Errorf("failed to parse search results: %w", err)
	}

	// 分页：该接口每页固定 20 条。本页抓满且未到页数上限时派生下一页任务；
	// 种子完成计数只在翻页链结束（不满页 / 到上限 / 出错）时累加，
	// 避免退出监控在翻页中途误判任务完成。
	const (
		searchPageSize = 20
		maxSearchPages = 5
	)

	rawCount := len(entries)
	spawnNext := rawCount >= searchPageSize && j.params.Offset < (maxSearchPages-1)*searchPageSize

	var nextJobs []scrapemate.IJob

	if spawnNext {
		nextParams := *j.params
		nextParams.Offset += searchPageSize

		next := NewSearchJob(&nextParams)
		next.ExitMonitor = j.ExitMonitor
		next.WriterManagedCompletion = j.WriterManagedCompletion
		next.Deduper = j.Deduper
		next.ExtractEmail = j.ExtractEmail
		next.Research = j.Research

		nextJobs = append(nextJobs, next)
	}

	entries = filterAndSortEntriesWithinRadius(entries,
		j.params.Location.Lat,
		j.params.Location.Lon,
		j.params.Location.Radius,
	)

	// 去重：网格单元重叠 / 数量上限共用 deduper（达到上限时 AddIfNotExists 返回 false）
	if j.Deduper != nil {
		ctx := context.Background()
		uniq := make([]*Entry, 0, len(entries))

		for _, e := range entries {
			key := e.Link
			if key == "" {
				key = e.ID
			}
			if key == "" {
				key = fmt.Sprintf("%s|%.6f,%.6f", e.Title, e.Latitude, e.Longtitude)
			}
			if j.Deduper.AddIfNotExists(ctx, key) {
				uniq = append(uniq, e)
			}
		}

		entries = uniq
	}

	if j.ExitMonitor != nil {
		j.ExitMonitor.IncrPlacesFound(len(entries))
		if !spawnNext {
			j.ExitMonitor.IncrSeedCompleted(1)
		}
	}

	// 官网挖掘：有官网的商户派生轻量 HTTP 任务（不走浏览器）——
	// 开启背调时是多页背调任务，否则只抓邮箱
	if j.ExtractEmail {
		direct := make([]*Entry, 0, len(entries))

		var websiteJobs []scrapemate.IJob

		for _, e := range entries {
			websiteJob := newWebsiteJob(j.ID, e, j.Research, j.ExitMonitor, j.WriterManagedCompletion)
			if websiteJob == nil {
				direct = append(direct, e)

				continue
			}

			websiteJobs = append(websiteJobs, websiteJob)
		}

		if j.ExitMonitor != nil && !j.WriterManagedCompletion {
			j.ExitMonitor.IncrPlacesCompleted(len(direct))
		}

		return direct, append(nextJobs, websiteJobs...), nil
	}

	if j.ExitMonitor != nil && !j.WriterManagedCompletion {
		j.ExitMonitor.IncrPlacesCompleted(len(entries))
	}

	return entries, nextJobs, nil
}

func removeFirstLine(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	index := bytes.IndexByte(data, '\n')
	if index == -1 {
		return []byte{}
	}

	return data[index+1:]
}

func buildGoogleMapsParams(params *MapSearchParams) map[string]string {
	params.ViewportH = 800
	params.ViewportW = 600

	ans := map[string]string{
		"tbm":      "map",
		"authuser": "0",
		"hl":       params.Hl,
		"q":        params.Query,
	}

	pb := fmt.Sprintf("!4m12!1m3!1d3826.902183192154!2d%.4f!3d%.4f!2m3!1f0!2f0!3f0!3m2!1i%d!2i%d!4f%.1f!7i20!8i%d"+
		"!10b1!12m22!1m3!18b1!30b1!34e1!2m3!5m1!6e2!20e3!4b0!10b1!12b1!13b1!16b1!17m1!3e1!20m3!5e2!6b1!14b1!46m1!1b0"+
		"!96b1!19m4!2m3!1i360!2i120!4i8",
		params.Location.Lon,
		params.Location.Lat,
		params.ViewportW,
		params.ViewportH,
		params.Location.ZoomLvl,
		params.Offset,
	)

	ans["pb"] = pb

	return ans
}
