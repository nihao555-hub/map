package web

import (
	"context"
	"errors"
	"time"
)

var jobs []Job

const (
	StatusPending = "pending"
	StatusWorking = "working"
	StatusOK      = "ok"
	StatusFailed  = "failed"
)

type SelectParams struct {
	Status string
	Limit  int
}

type JobRepository interface {
	Get(context.Context, string) (Job, error)
	Create(context.Context, *Job) error
	Delete(context.Context, string) error
	Select(context.Context, SelectParams) ([]Job, error)
	Update(context.Context, *Job) error
}

type Job struct {
	ID     string
	Name   string
	Date   time.Time
	Status string
	Data   JobData
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
	GridMode    bool    `json:"grid_mode"`
	GridBBox    string  `json:"grid_bbox"`    // "minLat,minLon,maxLat,maxLon"
	GridCellKm  float64 `json:"grid_cell_km"` // 每格边长（公里）
	Locations   string  `json:"locations"`    // 原始地点名，用于网格模式的地理编码
	// 结果列配置：逗号分隔的 CSV 列名；空 = 按模式默认（快速=必要列，深度/网格=全部列）
	Columns string `json:"columns"`
	// 目标客户数量上限：0 = 不限（抓到全域全量为止）
	MaxResults int `json:"max_results"`
	// 用户意图留痕：前端选的国家/原始关键词，方便核对「找什么/在哪/哪个国家」
	CountryCode  string   `json:"country_code,omitempty"`
	CountryName  string   `json:"country_name,omitempty"`
	RawKeywords  []string `json:"raw_keywords,omitempty"`
}

// GeoAnchor 返回用于展示的锚定坐标（如 "13.756331, 100.501765"），
// 无有效锚定（空值或表单默认的 0,0）时返回空串，模板据此决定是否展示
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
