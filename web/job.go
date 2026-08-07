package web

import (
	"context"
	"errors"
	"strings"
	"time"
)

var jobs []Job

const (
	StatusPending  = "pending"
	StatusWorking  = "working"
	StatusOK       = "ok"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
)

// User-facing job lifecycle (Status + Phase):
//
//	排队中 pending  — waiting for an admission slot
//	采集中 working  — Maps grid scrape in progress
//	背调中 ok+intel — scrape rows ready; OSINT / AI intel still running
//	已完成 ok       — scrape done (and intel done, or intel disabled)
//	失败   failed   — scrape errored
//	已终止 canceled — stopped by user
//
// Phase is UI-only (not persisted). Persist only Status_*.

type SelectParams struct {
	Status string
	Limit  int
	// Owner filters by invite-code tenant. Empty means no owner filter (worker/admin).
	Owner string
}

type JobRepository interface {
	Get(context.Context, string) (Job, error)
	Create(context.Context, *Job) error
	Delete(context.Context, string) error
	Select(context.Context, SelectParams) ([]Job, error)
	Update(context.Context, *Job) error
	// ClaimPending atomically marks the oldest pending job as working.
	// Returns ErrNoPending when the queue is empty.
	ClaimPending(context.Context) (Job, error)
}

// ErrNoPending is returned by ClaimPending when no pending jobs exist.
var ErrNoPending = errors.New("no pending jobs")

// ErrJobNotFound is returned when a job is missing or not visible to the caller.
var ErrJobNotFound = errors.New("job not found")

type Job struct {
	ID     string
	Name   string
	Date   time.Time
	Status string
	// Phase is a UI-only annotation (not persisted): pending|working|intel|ok|failed|canceled.
	// When scrape rows have landed but website-email / OSINT backfill still runs, Status may
	// already be "ok" while Phase stays "intel" until背调 finishes.
	Phase string `json:"phase,omitempty"`
	// Owner is the invite code that owns this job (tenant isolation key).
	Owner string
	Data  JobData
}

func (j *Job) Validate() error {
	if j.ID == "" {
		return errors.New("missing id")
	}

	if j.Name == "" {
		return errors.New("missing name")
	}

	if j.Status == "" {
		return errors.New("missing status")
	}

	if j.Date.IsZero() {
		return errors.New("missing date")
	}

	if err := j.Data.Validate(); err != nil {
		return err
	}

	return nil
}

// normalizeUILang 规范化界面/AI 产出语言代码；不支持时回落 en。
func normalizeUILang(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	switch code {
	case "zh", "en", "id", "ms", "th", "vi", "tl", "fil", "km", "lo", "my":
		if code == "fil" {
			return "tl"
		}
		return code
	default:
		return "en"
	}
}

type JobData struct {
	Keywords     []string      `json:"keywords"`
	Lang         string        `json:"lang"`
	Zoom         int           `json:"zoom"`
	Lat          string        `json:"lat"`
	Lon          string        `json:"lon"`
	FastMode     bool          `json:"fast_mode"`
	Radius       int           `json:"radius"`
	Depth        int           `json:"depth"`
	Email        bool          `json:"email"`
	ExtraReviews bool          `json:"extra_reviews"`
	MaxTime      time.Duration `json:"max_time"`
	Proxies      []string      `json:"proxies"`
	// 网格全量模式：把区域切块搜索，突破 120 条上限
	GridMode   bool    `json:"grid_mode"`
	GridBBox   string  `json:"grid_bbox"`    // "minLat,minLon,maxLat,maxLon"
	GridCellKm float64 `json:"grid_cell_km"` // 每格边长（公里）
	Locations  string  `json:"locations"`    // 原始地点名，用于网格模式的地理编码
	// 结果列配置：逗号分隔的 CSV 列名；空 = 按模式默认（快速=必要列，深度/网格=全部列）
	Columns string `json:"columns"`
	// 目标客户数量上限：0 = 不限（在目标半径内尽量抓全）
	MaxResults int `json:"max_results"`
	// EnableIntel：抓取前由用户确认是否并发背调（theHarvester/SpiderFoot/OC/AI）
	EnableIntel bool `json:"enable_intel"`
	// FromAgent marks jobs created by the Agent workspace so they stay out of
	// the standard map-mode right-hand job list.
	FromAgent bool `json:"from_agent,omitempty"`
	// UILang：界面/AI 产出语言（与 Maps 搜索 hl/lang 独立）。如 zh、en、id…
	UILang string `json:"ui_lang,omitempty"`
	// 目标半径由 Radius（米）表达；前端以公里输入，上限见 MaxRadiusKm()
	CountryCode string   `json:"country_code,omitempty"`
	CountryName string   `json:"country_name,omitempty"`
	RawKeywords []string `json:"raw_keywords,omitempty"`
	// LastError records why a scrape was interrupted. When non-empty but Status
	// is still "ok", rows were salvaged after a mid-run crash/timeout.
	LastError string `json:"last_error,omitempty"`
}

// GeoAnchor 返回用于展示的锚定坐标（如 "13.756331, 100.501765"），
// 无有效锚定（空值或表单默认的 0,0）时返回空串，模板据此决定是否展示
//
//nolint:gocritic // 模板里以值形式访问 .Data.GeoAnchor，需要值接收者
func (d JobData) GeoAnchor() string {
	if !hasGeoAnchor(d.Lat, d.Lon) {
		return ""
	}

	return d.Lat + ", " + d.Lon
}

func (d *JobData) Validate() error {
	if len(d.Keywords) == 0 {
		return errors.New("missing keywords")
	}

	if d.Lang == "" {
		return errors.New("missing lang")
	}

	if len(d.Lang) != 2 {
		return errors.New("invalid lang")
	}

	if d.Depth == 0 {
		return errors.New("missing depth")
	}

	if d.MaxTime == 0 {
		return errors.New("missing max time")
	}

	if d.FastMode && !d.GridMode && (d.Lat == "" || d.Lon == "") {
		return errors.New("missing geo coordinates")
	}

	return nil
}
