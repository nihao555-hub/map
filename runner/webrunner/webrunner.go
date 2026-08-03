package webrunner

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/exiter"
	"github.com/gosom/google-maps-scraper/grid"
	"github.com/gosom/google-maps-scraper/runner"
	"github.com/gosom/google-maps-scraper/tlmt"
	"github.com/gosom/google-maps-scraper/web"
	"github.com/gosom/google-maps-scraper/web/sqlite"
	"github.com/gosom/scrapemate"
	"github.com/gosom/scrapemate/scrapemateapp"
	"golang.org/x/sync/errgroup"
)

type webrunner struct {
	srv       *web.Server
	svc       *web.Service
	cfg       *runner.Config
	setupMate func(context.Context, io.Writer, *web.Job) (mateRunner, error)
}

type mateRunner interface {
	Start(context.Context, ...scrapemate.IJob) error
	Close() error
}

func New(cfg *runner.Config) (runner.Runner, error) {
	if cfg.DataFolder == "" {
		return nil, fmt.Errorf("data folder is required")
	}

	if err := os.MkdirAll(cfg.DataFolder, os.ModePerm); err != nil {
		return nil, err
	}

	const dbfname = "jobs.db"

	dbpath := filepath.Join(cfg.DataFolder, dbfname)

	repo, err := sqlite.New(dbpath)
	if err != nil {
		return nil, err
	}

	svc := web.NewService(repo, cfg.DataFolder)

	srv, err := web.New(svc, cfg.Addr)
	if err != nil {
		return nil, err
	}

	ans := webrunner{
		srv:       srv,
		svc:       svc,
		cfg:       cfg,
		setupMate: defaultSetupMate(cfg),
	}

	return &ans, nil
}

func (w *webrunner) Run(ctx context.Context) error {
	egroup, ctx := errgroup.WithContext(ctx)

	egroup.Go(func() error {
		return w.work(ctx)
	})

	egroup.Go(func() error {
		return w.srv.Start(ctx)
	})

	return egroup.Wait()
}

func (w *webrunner) Close(context.Context) error {
	return nil
}

func (w *webrunner) work(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			jobs, err := w.svc.SelectPending(ctx)
			if err != nil {
				return err
			}

			for i := range jobs {
				select {
				case <-ctx.Done():
					return nil
				default:
					t0 := time.Now().UTC()
					if err := w.scrapeJob(ctx, &jobs[i]); err != nil {
						params := map[string]any{
							"job_count": len(jobs[i].Data.Keywords),
							"duration":  time.Now().UTC().Sub(t0).String(),
							"error":     err.Error(),
						}

						evt := tlmt.NewEvent("web_runner", params)

						_ = runner.Telemetry().Send(ctx, evt)

						log.Printf("error scraping job %s: %v", jobs[i].ID, err)
					} else {
						params := map[string]any{
							"job_count": len(jobs[i].Data.Keywords),
							"duration":  time.Now().UTC().Sub(t0).String(),
						}

						_ = runner.Telemetry().Send(ctx, tlmt.NewEvent("web_runner", params))

						log.Printf("job %s scraped successfully", jobs[i].ID)
					}
				}
			}
		}
	}
}

func (w *webrunner) scrapeJob(ctx context.Context, job *web.Job) error {
	job.Status = web.StatusWorking

	err := w.svc.Update(ctx, job)
	if err != nil {
		return err
	}

	if len(job.Data.Keywords) == 0 {
		job.Status = web.StatusFailed

		return w.svc.Update(ctx, job)
	}

	outpath := filepath.Join(w.cfg.DataFolder, job.ID+".csv")

	outfile, err := os.Create(outpath)
	if err != nil {
		return err
	}

	defer func() {
		_ = outfile.Close()
	}()

	setupMate := w.setupMate
	if setupMate == nil {
		setupMate = defaultSetupMate(w.cfg)
	}

	mate, err := setupMate(ctx, outfile, job)
	if err != nil {
		job.Status = web.StatusFailed

		err2 := w.svc.Update(ctx, job)
		if err2 != nil {
			log.Printf("failed to update job status: %v", err2)
		}

		return err
	}

	defer mate.Close()

	var dedup deduper.Deduper = deduper.New()
	if job.Data.MaxResults > 0 {
		// 用户设置了目标客户数量上限：去重计数达上限后不再播种新详情任务
		dedup = newLimitDeduper(dedup, job.Data.MaxResults)
	}

	exitMonitor := exiter.New()

	var seedJobs []scrapemate.IJob

	// 网格全量模式
	if job.Data.GridMode {
		var bbox grid.BoundingBox

		// 优先用手动传的 bbox
		if job.Data.GridBBox != "" {
			var err error
			bbox, err = grid.ParseBoundingBox(job.Data.GridBBox)
			if err != nil {
				log.Printf("failed to parse grid bbox %q: %v, falling back to geocoding", job.Data.GridBBox, err)
			}
		}

		// 用户已在地图上选点（带经纬度锚点）：直接以锚点为中心构造网格，
		// 不依赖外部地理编码服务，稳定且零延迟
		if bbox.MinLat == 0 && bbox.MaxLat == 0 {
			if alat, aerr := strconv.ParseFloat(job.Data.Lat, 64); aerr == nil {
				if alon, aerr2 := strconv.ParseFloat(job.Data.Lon, 64); aerr2 == nil && !(alat == 0 && alon == 0) {
					halfKm := float64(job.Data.Radius) / 2000 // radius=10000m → 半径5km（约10km×10km）
					bbox = anchorBBox(alat, alon, halfKm)
					log.Printf("grid mode: anchor bbox around %.4f,%.4f (±%.1fkm)", alat, alon, halfKm)
				}
			}
		}

		// 没有 bbox 就用地理编码从地点名生成
		if bbox.MinLat == 0 && bbox.MaxLat == 0 && job.Data.Locations != "" {
			log.Printf("geocoding location %q for grid mode", job.Data.Locations)

			var err error
			bbox, err = geocode(ctx, job.Data.Locations)
			if err != nil {
				log.Printf("geocoding failed: %v, falling back to single search", err)
				// 地理编码失败就退化成普通模式
				job.Data.GridMode = false
			} else {
				// 向外扩展 10%，确保覆盖完整
				bbox = expandBBox(bbox, 0.1)
				log.Printf("geocoded bbox: %.4f,%.4f -> %.4f,%.4f (%s)",
					bbox.MinLat, bbox.MinLon, bbox.MaxLat, bbox.MaxLon, job.Data.Locations)
			}
		}

		if job.Data.GridMode {
			cellKm := job.Data.GridCellKm
			if cellKm <= 0 {
				cellKm = 1.5 // 默认 1.5km 一格
			}

			// 估算格子数，打个日志
			estCells := grid.EstimateCellCount(bbox, cellKm)
			log.Printf("grid mode: ~%d cells at %.1fkm resolution", estCells, cellKm)

			var err error
			if job.Data.FastMode {
				// 快速模式：纯 HTTP 搜索接口按格取数，不启动浏览器，速度快数十倍
				seedJobs, err = runner.CreateGridSearchSeedJobs(
					job.Data.Lang,
					strings.NewReader(strings.Join(job.Data.Keywords, "\n")),
					job.Data.Email,
					bbox,
					cellKm,
					job.Data.Zoom,
					dedup,
					exitMonitor,
				)
			} else {
				seedJobs, err = runner.CreateGridSeedJobs(
					job.Data.Lang,
					strings.NewReader(strings.Join(job.Data.Keywords, "\n")),
					job.Data.Depth,
					job.Data.Email,
					bbox,
					cellKm,
					job.Data.Zoom,
					dedup,
					exitMonitor,
					w.cfg.ExtraReviews || job.Data.ExtraReviews,
				)
			}
			if err != nil {
				log.Printf("failed to create grid seed jobs: %v, falling back to single search", err)
				job.Data.GridMode = false
			}
		}
	}

	// 普通模式（快速 / 标准深度）
	if !job.Data.GridMode {
		var coords string
		if job.Data.Lat != "" && job.Data.Lon != "" {
			coords = job.Data.Lat + "," + job.Data.Lon
		}

		radius := float64(10000)
		if job.Data.Radius > 0 {
			radius = float64(job.Data.Radius)
		}

		var err error
		seedJobs, err = runner.CreateSeedJobs(
			job.Data.FastMode,
			job.Data.Lang,
			strings.NewReader(strings.Join(job.Data.Keywords, "\n")),
			job.Data.Depth,
			job.Data.Email,
			coords,
			job.Data.Zoom,
			radius,
			dedup,
			exitMonitor,
			w.cfg.ExtraReviews || job.Data.ExtraReviews,
		)
		if err != nil {
			err2 := w.svc.Update(ctx, job)
			if err2 != nil {
				log.Printf("failed to update job status: %v", err2)
			}

			return err
		}

		// 深度模式单点列表常卡在 ~120：目标≥100 且有坐标时，再开 4 个偏移点搜索扩量
		// （仍走详情页，质量不变；靠共享 deduper 去重）
		if !job.Data.FastMode && job.Data.MaxResults >= 100 && coords != "" {
			extra := deepSearchFanoutSeeds(
				job,
				radius,
				dedup,
				exitMonitor,
				w.cfg.ExtraReviews || job.Data.ExtraReviews,
			)
			if len(extra) > 0 {
				seedJobs = append(seedJobs, extra...)
				log.Printf("deep mode fan-out: +%d offset searches for max_results=%d", len(extra), job.Data.MaxResults)
			}
		}
	}

	if len(seedJobs) > 0 {
		exitMonitor.SetSeedCount(len(seedJobs))

		// 网格模式下格子多，需要更长超时；按格子数估算
		var allowedSeconds int
		if job.Data.GridMode {
			// 每格约 15 秒（含详情页），最少 3 分钟
			allowedSeconds = max(180, len(seedJobs)*15)
		} else {
			allowedSeconds = max(60, len(seedJobs)*10*job.Data.Depth/50+120)
		}

		if job.Data.MaxTime > 0 {
			if job.Data.GridMode {
				// 网格全量模式不允许用户侧的最大时间造成截断：取网格估算与设置值中的较大者
				allowedSeconds = max(allowedSeconds, int(job.Data.MaxTime.Seconds()))
			} else if job.Data.MaxTime.Seconds() < 180 {
				allowedSeconds = 180
			} else {
				allowedSeconds = int(job.Data.MaxTime.Seconds())
			}
		}

		// 深度+抓邮箱：预留联系方式补齐时间，避免 MaxTime 一到就砍掉邮箱队列
		if !job.Data.FastMode && job.Data.Email {
			contactBudget := 300 // 至少再留 5 分钟给官网补齐
			if job.Data.MaxResults > 0 {
				// 约每条 2s 官网（并发下），上限 20 分钟
				contactBudget = max(contactBudget, min(1200, job.Data.MaxResults*2))
			}
			allowedSeconds = max(allowedSeconds, int(job.Data.MaxTime.Seconds())+contactBudget)
			log.Printf("deep+email: extended time budget +%ds → %ds total", contactBudget, allowedSeconds)
		}

		log.Printf("running job %s with %d seed jobs and %d allowed seconds", job.ID, len(seedJobs), allowedSeconds)

		mateCtx, cancel := context.WithTimeout(ctx, time.Duration(allowedSeconds)*time.Second)
		defer cancel()

		exitMonitor.SetCancelFunc(cancel)

		go exitMonitor.Run(mateCtx)

		err = mate.Start(mateCtx, seedJobs...)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			cancel()

			err2 := w.svc.Update(ctx, job)
			if err2 != nil {
				log.Printf("failed to update job status: %v", err2)
			}

			return err
		}

		cancel()
	}

	job.Status = web.StatusOK

	return w.svc.Update(ctx, job)
}

func defaultSetupMate(cfg *runner.Config) func(context.Context, io.Writer, *web.Job) (mateRunner, error) {
	return func(_ context.Context, writer io.Writer, job *web.Job) (mateRunner, error) {
		// 提速：并发 = 配置的并发数；页面复用从 2 提到 20，浏览器复用从 200 提到 1000
		opts := []func(*scrapemateapp.Config) error{
			scrapemateapp.WithConcurrency(cfg.Concurrency),
		}
		// 快速：HTTP 搜索，空闲可短收尾；深度：浏览器冷启动+滚动常 >45s，过短会误杀整单。
		if job.Data.FastMode {
			opts = append(opts, scrapemateapp.WithExitOnInactivity(90*time.Second))
		} else {
			// 深度：浏览器冷启动 + 官网联系方式补齐，需要更长空闲窗口
			opts = append(opts, scrapemateapp.WithExitOnInactivity(5*time.Minute))
		}

		if !job.Data.FastMode {
			opts = append(opts,
				scrapemateapp.WithJS(scrapemateapp.DisableImages()),
			)
		} else {
			opts = append(opts,
				scrapemateapp.WithStealth("firefox"),
			)
		}

		// 提速：多页面复用 + 更大的浏览器池（如果配置了）
		opts = runner.AppendBrowserCapacityOptions(opts, cfg)

		// 提速：如果没配置浏览器容量，给一个默认的优化值
		// 一个浏览器开 4 个页面，省内存换并发
		if cfg.MaxPagesPerBrowser <= 1 && cfg.BrowserPoolSize <= 0 {
			opts = append(opts,
				scrapemateapp.WithMaxPagesPerBrowser(4),
			)
		}

		hasProxy := false

		if len(cfg.Proxies) > 0 {
			opts = append(opts, scrapemateapp.WithProxies(cfg.Proxies))
			hasProxy = true
		} else if len(job.Data.Proxies) > 0 {
			opts = append(opts,
				scrapemateapp.WithProxies(job.Data.Proxies),
			)
			hasProxy = true
		}

		if !cfg.DisablePageReuse {
			// 提速：页面复用从 2 提到 20，减少页面创建开销
			// 浏览器复用从 200 提到 1000，减少浏览器重启开销
			opts = append(opts,
				scrapemateapp.WithPageReuseLimit(20),
				scrapemateapp.WithBrowserReuseLimit(1000),
			)
		}

		log.Printf("job %s has proxy: %v", job.ID, hasProxy)

		// 按任务配置过滤输出列：快速=必要列，深度=用户自选列（内部列强制保留）
		// 传入 *os.File 以支持「地点先写、邮箱后补」的 upsert，避免超时丢行/重复行
		var outFile *os.File
		if f, ok := writer.(*os.File); ok {
			outFile = f
		}
		csvWriter := newColumnWriter(csv.NewWriter(writer), job.Data.FastMode, job.Data.Columns, outFile)

		writers := []scrapemate.ResultWriter{csvWriter}

		matecfg, err := scrapemateapp.NewConfig(
			writers,
			opts...,
		)
		if err != nil {
			return nil, err
		}

		return scrapemateapp.NewScrapeMateApp(matecfg)
	}
}

// deepSearchFanoutSeeds 在中心点四周再开偏移搜索，突破 Google 单列表 ~120 的上限。
// 偏移约 3km，详情仍走浏览器 PlaceJob，质量与单点深度一致。
func deepSearchFanoutSeeds(
	job *web.Job,
	radius float64,
	dedup deduper.Deduper,
	exitMonitor exiter.Exiter,
	extraReviews bool,
) []scrapemate.IJob {
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(job.Data.Lat), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(job.Data.Lon), 64)
	if err1 != nil || err2 != nil {
		return nil
	}

	const delta = 0.028 // ≈3km
	offsets := [][2]float64{
		{delta, 0}, {-delta, 0}, {0, delta}, {0, -delta},
	}

	var out []scrapemate.IJob
	for _, o := range offsets {
		coords := fmt.Sprintf("%.6f,%.6f", lat+o[0], lon+o[1])
		jobs, err := runner.CreateSeedJobs(
			false,
			job.Data.Lang,
			strings.NewReader(strings.Join(job.Data.Keywords, "\n")),
			job.Data.Depth,
			job.Data.Email,
			coords,
			job.Data.Zoom,
			radius,
			dedup,
			exitMonitor,
			extraReviews,
		)
		if err != nil {
			log.Printf("deep fan-out seed at %s failed: %v", coords, err)
			continue
		}
		out = append(out, jobs...)
	}

	return out
}
