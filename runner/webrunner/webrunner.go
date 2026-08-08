package webrunner

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/exiter"
	"github.com/gosom/google-maps-scraper/gmaps"
	"github.com/gosom/google-maps-scraper/grid"
	"github.com/gosom/google-maps-scraper/placecache"
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
	if err := placecache.Init(cfg.DataFolder); err != nil {
		log.Printf("placecache init: %v (continuing without cross-job cache)", err)
	}

	const dbfname = "jobs.db"

	dbpath := filepath.Join(cfg.DataFolder, dbfname)

	store, err := sqlite.New(dbpath)
	if err != nil {
		return nil, err
	}

	svc := web.NewService(store, cfg.DataFolder)

	inviteOn := web.InviteRequired()
	if inviteOn {
		exportPath := filepath.Join(cfg.DataFolder, "invite_codes.txt")
		if err := web.SeedAndExport(context.Background(), store, web.InviteSeedCount(), exportPath); err != nil {
			return nil, fmt.Errorf("seed invite codes: %w", err)
		}
	}

	srv, err := web.New(svc, cfg.Addr, web.WithInvite(store, inviteOn))
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

	staleAge := web.StaleWorkingAge()
	// Crash / hung-browser recovery: no heartbeat → fail so UI/queue stay honest.
	// Jobs that already wrote CSV rows are salvaged back to ok (partial success).
	if n, err := w.svc.FailStaleWorking(ctx, staleAge); err != nil {
		log.Printf("fail stale working: %v", err)
	} else if n > 0 {
		log.Printf("watchdog: failed %d zombie working job(s) with no heartbeat for %s", n, staleAge)
	}
	if sn, err := w.svc.SalvageFailedJobsWithResults(ctx); err != nil {
		log.Printf("salvage failed jobs: %v", err)
	} else if sn > 0 {
		log.Printf("startup salvage: restored %d failed job(s) with on-disk results", sn)
	}

	maxJobs := web.AdaptiveJobConcurrency()
	web.LogMemoryPressure("web runner start")
	log.Printf("web runner: fair-admission slots=%d (env cap GMS_WEB_JOB_CONCURRENCY=%d); newest pending first; stale zombie=%s",
		maxJobs, web.JobConcurrency(), staleAge)

	var eg errgroup.Group
	var watchdogAt time.Time

	for {
		select {
		case <-ctx.Done():
			_ = eg.Wait()
			return nil
		case <-ticker.C:
			// Periodic zombie sweep (every ~30s): frees DB "working" ghosts after crash.
			if time.Since(watchdogAt) >= 30*time.Second {
				watchdogAt = time.Now()
				if n, err := w.svc.FailStaleWorking(ctx, web.StaleWorkingAge()); err != nil {
					log.Printf("watchdog fail stale: %v", err)
				} else if n > 0 {
					log.Printf("watchdog: failed %d zombie working job(s)", n)
				}
			}

			// Re-evaluate slots each tick (memory/CPU change); never oversubscribe.
			for web.CanAdmitDeepJob() {
				select {
				case <-ctx.Done():
					_ = eg.Wait()
					return nil
				default:
				}

				job, err := w.svc.ClaimPending(ctx)
				if err != nil {
					if errors.Is(err, web.ErrNoPending) {
						break
					}
					_ = eg.Wait()
					return err
				}

				j := job
				wall := web.JobWallClock(j.Data.MaxTime)
				web.BeginDeepJob()
				slots := web.AdaptiveJobConcurrency()
				log.Printf("claimed job %s name=%q (active=%d/%d fair-admission wall=%s)",
					j.ID, j.Name, web.ActiveDeepJobs(), slots, wall)
				eg.Go(func() error {
					// Release admit as soon as Maps PlaceJobs finish (emails may
					// still run). Once prevents double-free with the defer.
					var admitOnce sync.Once
					releaseAdmit := func(reason string) {
						admitOnce.Do(func() {
							web.EndDeepJob()
							log.Printf("job %s: released admit slot early (%s); active=%d/%d",
								j.ID, reason, web.ActiveDeepJobs(), web.AdaptiveJobConcurrency())
						})
					}
					defer releaseAdmit("scrape goroutine exit")

					t0 := time.Now().UTC()
					jobCtx, cancel := context.WithTimeout(ctx, wall)
					defer cancel()
					w.svc.RegisterJobCancel(j.ID, cancel)
					defer w.svc.UnregisterJobCancel(j.ID)

					done := make(chan error, 1)
					go func() {
						done <- w.scrapeJob(jobCtx, &j, releaseAdmit)
					}()

					var err error
					select {
					case err = <-done:
					case <-jobCtx.Done():
						select {
						case err = <-done:
						case <-time.After(90 * time.Second):
							log.Printf("job %s zombie: wall clock %s exceeded — force finish, free admit slot", j.ID, wall)
							_ = w.svc.FinishJobWithOutcome(context.Background(), &j, fmt.Sprintf("wall clock exceeded (%s)", wall))
							err = fmt.Errorf("job %s wall clock exceeded (%s)", j.ID, wall)
						}
					}

					params := map[string]any{
						"job_count": len(j.Data.Keywords),
						"duration":  time.Now().UTC().Sub(t0).String(),
					}
					if err != nil {
						params["error"] = err.Error()
						_ = runner.Telemetry().Send(ctx, tlmt.NewEvent("web_runner", params))
						log.Printf("error scraping job %s: %v", j.ID, err)
					} else {
						_ = runner.Telemetry().Send(ctx, tlmt.NewEvent("web_runner", params))
						log.Printf("job %s scraped successfully", j.ID)
					}
					return nil
				})
			}
		}
	}
}

func (w *webrunner) scrapeJob(ctx context.Context, job *web.Job, releaseAdmit func(string)) error {
	// ctx already carries the wall-clock timeout + cancel registered by work().
	jobCtx := ctx
	if releaseAdmit == nil {
		releaseAdmit = func(string) {}
	}
	// Also covers old queued jobs created before email enrichment became mandatory.
	job.Data.Email = true

	// Re-read status: user may have canceled while queued.
	if latest, err := w.svc.Get(jobCtx, job.ID); err == nil && latest.Status == web.StatusCanceled {
		log.Printf("job %s canceled before start", job.ID)
		return nil
	}

	job.Status = web.StatusWorking

	err := w.svc.Update(jobCtx, job)
	if err != nil {
		return err
	}

	// Heartbeat so FailStaleWorking does not kill a healthy long scrape.
	stopHB := make(chan struct{})
	defer close(stopHB)
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopHB:
				return
			case <-jobCtx.Done():
				return
			case <-t.C:
				if terr := w.svc.TouchJob(context.Background(), job.ID); terr != nil {
					log.Printf("job %s heartbeat: %v", job.ID, terr)
				}
			}
		}
	}()

	if len(job.Data.Keywords) == 0 {
		return w.svc.FinishJobWithOutcome(jobCtx, job, "missing keywords")
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

	// Cap browser setup (download/launch) so a hang cannot hold the admit slot forever.
	setupCtx, setupCancel := context.WithTimeout(jobCtx, 3*time.Minute)
	mate, err := setupMate(setupCtx, outfile, job)
	setupCancel()
	if err != nil {
		if err2 := w.svc.FinishJobWithOutcome(jobCtx, job, fmt.Sprintf("browser setup: %v", err)); err2 != nil {
			log.Printf("failed to update job status: %v", err2)
		}

		return err
	}

	defer func() {
		done := make(chan struct{})
		go func() {
			_ = mate.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			log.Printf("job %s mate.Close hung — abandoning browser cleanup", job.ID)
		}
	}()

	var dedup deduper.Deduper
	if web.UseBloomDeduper() {
		// Google Bigtable/LevelDB 风格：海量网格去重时用 Bloom 压内存；前段仍 exact
		dedup = deduper.NewBloom(web.BloomExpectedKeys(), 0.001)
		log.Printf("job %s: bloom deduper (expected≈%d)", job.ID, web.BloomExpectedKeys())
	} else {
		dedup = deduper.New()
	}
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
					halfKm := float64(job.Data.Radius) / 1000 // Radius 为米 → 真实目标半径（公里）
					if halfKm <= 0 {
						halfKm = 10
					}
					bbox = anchorBBox(alat, alon, halfKm)
					log.Printf("grid mode: anchor bbox around %.4f,%.4f (±%.1fkm)", alat, alon, halfKm)
				}
			}
		}

		// 没有 bbox 就用地理编码从地点名生成（含中文→英文、离线城市回退）
		if bbox.MinLat == 0 && bbox.MaxLat == 0 && job.Data.Locations != "" {
			log.Printf("geocoding location %q for grid mode (country=%s)", job.Data.Locations, job.Data.CountryCode)

			var err error
			bbox, err = geocodeInCountry(ctx, job.Data.Locations, job.Data.CountryCode)
			if err != nil {
				log.Printf("geocoding failed: %v", err)
				// 全量网格不能静默退化成单点 ~20：再用半径锚点最后一搏
				if alat, aerr := strconv.ParseFloat(job.Data.Lat, 64); aerr == nil {
					if alon, aerr2 := strconv.ParseFloat(job.Data.Lon, 64); aerr2 == nil && (alat != 0 || alon != 0) {
						halfKm := float64(job.Data.Radius) / 1000
						if halfKm <= 0 {
							halfKm = 10
						}
						bbox = anchorBBox(alat, alon, halfKm)
						log.Printf("grid mode: recovered anchor bbox %.4f,%.4f (±%.1fkm) after geocode failure", alat, alon, halfKm)
					}
				}
				if bbox.MinLat == 0 && bbox.MaxLat == 0 {
					failErr := fmt.Errorf("grid mode requires a valid location anchor: %w", err)
					_ = w.svc.FinishJobWithOutcome(context.Background(), job, failErr.Error())
					return failErr
				}
			} else {
				// 用户给了目标半径：以解析中心为圆心覆盖半径（全量按半径，不按 Nominatim 城市框）
				clat := (bbox.MinLat + bbox.MaxLat) / 2
				clon := (bbox.MinLon + bbox.MaxLon) / 2
				halfKm := float64(job.Data.Radius) / 1000
				if halfKm <= 0 {
					halfKm = 10
					bbox = expandBBox(bbox, 0.1)
					log.Printf("geocoded bbox: %.4f,%.4f -> %.4f,%.4f (%s)",
						bbox.MinLat, bbox.MinLon, bbox.MaxLat, bbox.MaxLon, job.Data.Locations)
				} else {
					bbox = anchorBBox(clat, clon, halfKm)
					log.Printf("grid mode: radius-shaped bbox around %.4f,%.4f (±%.1fkm from %q)",
						clat, clon, halfKm, job.Data.Locations)
				}
			}
		}

		if job.Data.GridMode {
			// Persist the resolved center so API/intel can enforce the exact
			// user radius instead of exposing Maps spillover from edge pins.
			if bbox.MinLat != 0 || bbox.MaxLat != 0 || bbox.MinLon != 0 || bbox.MaxLon != 0 {
				clat := (bbox.MinLat + bbox.MaxLat) / 2
				clon := (bbox.MinLon + bbox.MaxLon) / 2
				if alat, aerr := strconv.ParseFloat(job.Data.Lat, 64); aerr != nil || alat == 0 {
					job.Data.Lat = strconv.FormatFloat(clat, 'f', 6, 64)
				}
				if alon, aerr := strconv.ParseFloat(job.Data.Lon, 64); aerr != nil || alon == 0 {
					job.Data.Lon = strconv.FormatFloat(clon, 'f', 6, 64)
				}
				_ = w.svc.Update(context.Background(), job)
			}
			cellKm := job.Data.GridCellKm
			if cellKm <= 0 {
				cellKm = 1.5 // 默认 1.5km 一格
			}

			// 深度模式每格要开浏览器：半径大时自动加粗格子，避免上千格跑不完。
			// Cap deep grid so 2 workers finish faster; relevance filter keeps quality.
			// ~10×10=100 (was 12×12=144 which starved 1-worker jobs for 30–40m).
			const maxDeepGridCells = 100
			estCells := grid.EstimateCellCount(bbox, cellKm)
			if !job.Data.FastMode && estCells > maxDeepGridCells {
				halfKm := float64(job.Data.Radius) / 1000
				if halfKm <= 0 {
					halfKm = 10
				}
				// Side length ≈ diameter / sqrt(cap) so cell count ≈ cap.
				side := math.Sqrt(float64(maxDeepGridCells))
				target := (2 * halfKm) / side
				if target < cellKm {
					target = cellKm
				}
				if target > 10 {
					target = 10
				}
				// Nudge up until under cap (float rounding can leave 101–120).
				for grid.EstimateCellCount(bbox, target) > maxDeepGridCells && target < 10 {
					target += 0.25
				}
				log.Printf("grid mode: deep auto-coarsen cell %.1fkm -> %.1fkm (was ~%d cells, cap=%d)", cellKm, target, estCells, maxDeepGridCells)
				cellKm = target
				job.Data.GridCellKm = cellKm
				estCells = grid.EstimateCellCount(bbox, cellKm)
			}
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
				var filterOpts []gmaps.GmapJobOptions
				if alat, aerr := strconv.ParseFloat(job.Data.Lat, 64); aerr == nil {
					if alon, aerr2 := strconv.ParseFloat(job.Data.Lon, 64); aerr2 == nil && job.Data.Radius > 0 {
						filterOpts = append(filterOpts, gmaps.WithGmapJobFilter(
							job.Data.Keywords, alat, alon, float64(job.Data.Radius),
						))
					}
				}
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
					filterOpts...,
				)
			}
			if err != nil {
				failErr := fmt.Errorf("create grid seed jobs: %w", err)
				_ = w.svc.FinishJobWithOutcome(context.Background(), job, failErr.Error())
				return failErr
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

		log.Printf("running job %s with %d seed jobs and %d allowed seconds (job concurrency=%d)",
			job.ID, len(seedJobs), allowedSeconds, web.JobConcurrency())

		mateCtx, mateCancel := context.WithTimeout(jobCtx, time.Duration(allowedSeconds)*time.Second)
		defer mateCancel()

		exitMonitor.SetCancelFunc(mateCancel)

		go exitMonitor.Run(mateCtx)

		// Promote StatusOK (+ start intel) as soon as Maps seeds finish, even while
		// website-email enrichment still runs. Flat place counts with status=working
		// confused users into thinking the job was stuck.
		go w.watchScrapePhaseComplete(mateCtx, job.ID, exitMonitor, releaseAdmit)

		// Free fair-admission as soon as Playwright PlaceJobs finish — emails keep
		// running on the HTTP pool without blocking the next deep job.
		go w.watchAdmitRelease(mateCtx, job.ID, exitMonitor, releaseAdmit)

		// Progress stall: a hung Chromium can keep heartbeats alive while writing
		// zero new rows — free the admit slot so pending users are not blocked.
		if stall := web.ProgressStallAge(); stall > 0 {
			go w.watchProgressStall(mateCtx, mateCancel, job.ID, stall)
		}

		// 抓取阶段不做背调：先尽快把结果落盘给用户看。
		// 背调在 StatusOK（下方或 watchScrapePhaseComplete）后统一启动。
		err = mate.Start(mateCtx, seedJobs...)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			mateCancel()
			if err2 := w.svc.FinishJobWithOutcome(ctx, job, fmt.Sprintf("scrape engine: %v", err)); err2 != nil {
				log.Printf("failed to update job status: %v", err2)
			}

			return err
		}

		mateCancel()
	}

	// If we were canceled due to progress stall / wall / user, salvage any rows
	// already on disk instead of leaving a forever-"working" ghost.
	if jobCtx.Err() != nil {
		if latest, gerr := w.svc.Get(context.Background(), job.ID); gerr == nil {
			if latest.Status == web.StatusCanceled {
				log.Printf("job %s canceled by user", job.ID)
				return nil
			}
			if latest.Status == web.StatusOK {
				log.Printf("job %s canceled after results already ready", job.ID)
				return nil
			}
			reason := "interrupted"
			if errors.Is(jobCtx.Err(), context.DeadlineExceeded) {
				reason = "time budget exceeded"
			} else if jobCtx.Err() != nil {
				reason = jobCtx.Err().Error()
			}
			_ = w.svc.FinishJobWithOutcome(context.Background(), job, reason)
		}
		return nil
	}

	// Idempotent: watchScrapePhaseComplete may already have set StatusOK + intel.
	if latest, gerr := w.svc.Get(ctx, job.ID); gerr == nil && latest.Status == web.StatusOK {
		log.Printf("job %s scrape finished (already marked ok)", job.ID)
		return nil
	}

	job.Status = web.StatusOK
	if err := w.svc.Update(ctx, job); err != nil {
		return err
	}

	// 结果已落盘：此时再开背调，不与地图抓取抢代理/CPU
	if job.Data.EnableIntel {
		log.Printf("job %s: scrape done, starting intel in background", job.ID)
		w.svc.StartJobIntel(ctx, job.ID)
	}

	return nil
}

// watchProgressStall cancels a scrape when CSV row count has not grown for stall.
// This recovers admit slots when Chromium hangs but heartbeats still succeed.
func (w *webrunner) watchProgressStall(ctx context.Context, cancel context.CancelFunc, jobID string, stall time.Duration) {
	if cancel == nil || stall <= 0 {
		return
	}
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	lastN := w.svc.CountCSVDataRows(jobID)
	lastChange := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n := w.svc.CountCSVDataRows(jobID)
			if n > lastN {
				lastN = n
				lastChange = time.Now()
				_ = w.svc.TouchJob(context.Background(), jobID)
				continue
			}
			// No rows yet: rely on seed exit / wall clock. Stall recovery is for
			// hung browsers that already wrote some results then froze.
			if lastN == 0 {
				continue
			}
			if time.Since(lastChange) < stall {
				continue
			}
			log.Printf("job %s progress stall: no new CSV rows for %s (rows=%d) — cancel scrape to free admit slot",
				jobID, stall, lastN)
			job, err := w.svc.Get(context.Background(), jobID)
			if err == nil && (job.Status == web.StatusWorking || job.Status == web.StatusPending) {
				_ = w.svc.FinishJobWithOutcome(context.Background(), &job,
					fmt.Sprintf("progress stall: no new rows for %s", stall))
			}
			cancel()
			return
		}
	}
}

// watchAdmitRelease frees the deep-job admit slot once Maps PlaceJobs finish
// (seeds done + every discovered place processed). Email enrichment may continue.
// Fallback: StatusOK + CSV plateau also releases, so a stuck place counter cannot
// pin the admit slot until the whole email phase ends.
func (w *webrunner) watchAdmitRelease(ctx context.Context, jobID string, mon exiter.Exiter, release func(string)) {
	if release == nil {
		return
	}
	if mon != nil && mon.MapsPlacesFinished() {
		release("maps places finished")
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if mon != nil {
			_ = exiter.WaitMapsPlacesFinished(ctx, mon)
		} else {
			<-ctx.Done()
		}
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	lastN := -1
	lastChange := time.Now()
	okSince := time.Time{}

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			if mon != nil && mon.MapsPlacesFinished() {
				release("maps places finished")
			}
			return
		case <-ticker.C:
			if mon != nil && mon.MapsPlacesFinished() {
				release("maps places finished")
				return
			}
			job, err := w.svc.Get(context.Background(), jobID)
			if err != nil {
				continue
			}
			n := w.svc.CountCSVDataRows(jobID)
			if n != lastN {
				lastN = n
				lastChange = time.Now()
			}
			if job.Status == web.StatusOK {
				if okSince.IsZero() {
					okSince = time.Now()
				}
				// Results already exposed + no new rows for 2m → free slot for queue.
				if n > 0 && time.Since(lastChange) >= 2*time.Minute && time.Since(okSince) >= 2*time.Minute {
					release("status=ok csv plateau")
					return
				}
			}
		}
	}
}

// watchScrapePhaseComplete marks the job ok (+ starts intel) when all Maps seeds
// finish, or when the visible place count plateaus while seeds are nearly done.
// Email website fetches may still be in flight; StatusOK means "results ready".
func (w *webrunner) watchScrapePhaseComplete(ctx context.Context, jobID string, mon exiter.Exiter, releaseAdmit func(string)) {
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()

	const (
		plateauIdle   = 100 * time.Second
		plateauMinRun = 2 * time.Minute
		seedNearDone  = 0.92 // 92% of grid cells finished
	)

	started := time.Now()
	lastCount := -1
	lastChange := time.Now()
	marked := false

	tryMark := func(reason string, plateau bool) {
		if marked {
			return
		}
		ok, err := w.svc.MarkScrapeComplete(context.Background(), jobID)
		if err != nil {
			log.Printf("job %s mark scrape complete: %v", jobID, err)
			return
		}
		if ok {
			marked = true
			log.Printf("job %s: %s → status=ok (intel if enabled); email enrichment may continue", jobID, reason)
			// Plateau: results are good enough — free admit without waiting for
			// empty outer grid cells. Full seed completion still uses MapsPlacesFinished.
			if plateau && releaseAdmit != nil {
				releaseAdmit("place count plateau")
			}
		} else {
			// Already ok/canceled/failed
			marked = true
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if marked {
				return
			}
			if mon.SeedsFinished() {
				// Avoid flashing status=ok with an empty table while streamed
				// PlaceJobs are still draining on browser workers.
				prog := mon.Snapshot()
				n := 0
				if cnt, err := w.svc.CountPlacesCached(context.Background(), jobID); err == nil {
					n = cnt
				}
				if n == 0 && prog.PlacesFound > 0 && prog.PlacesCompleted < prog.PlacesFound {
					continue
				}
				tryMark("maps seeds finished", false)
				return
			}

			prog := mon.Snapshot()
			n := 0
			if cnt, err := w.svc.CountPlacesCached(context.Background(), jobID); err == nil {
				n = cnt
			}
			if n != lastCount {
				lastCount = n
				lastChange = time.Now()
			}

			seedsNearDone := prog.SeedCount > 0 &&
				float64(prog.SeedCompleted) >= float64(prog.SeedCount)*seedNearDone
			plateau := n > 0 &&
				time.Since(started) >= plateauMinRun &&
				time.Since(lastChange) >= plateauIdle

			if seedsNearDone && plateau {
				tryMark(fmt.Sprintf("place count plateau at %d with seeds %d/%d", n, prog.SeedCompleted, prog.SeedCount), true)
				return
			}
		}
	}
}

func defaultSetupMate(cfg *runner.Config) func(context.Context, io.Writer, *web.Job) (mateRunner, error) {
	return func(_ context.Context, writer io.Writer, job *web.Job) (mateRunner, error) {
		// Fair admission: each job keeps a FIXED deep worker budget (does not dilute under load).
		jobConc := web.ReservedPerJobConcurrency(cfg.Concurrency, job.Data.FastMode)
		httpConc := web.ReservedHTTPConcurrency(job.Data.FastMode)
		log.Printf("job %s scrapemate concurrency=%d httpWorkers=%d (reserved, fair-admission active=%d/%d availMemMB=%d)",
			job.ID, jobConc, httpConc, web.ActiveDeepJobs(), web.AdaptiveJobConcurrency(), web.AvailableMemoryMB())
		opts := []func(*scrapemateapp.Config) error{
			scrapemateapp.WithConcurrency(jobConc),
			scrapemateapp.WithHTTPConcurrency(httpConc),
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

		// Low-RAM web workers share concurrent pages in one browser/context.
		// The old deploy default (pool=4, pages=4) launched four Chromium
		// instances for only 2–4 active page workers and wasted most RAM.
		browserCfg := *cfg
		if web.AvailableMemoryMB() < 6000 && browserCfg.BrowserPoolSize > 1 {
			browserCfg.BrowserPoolSize = 1
		}
		if browserCfg.MaxPagesPerBrowser < jobConc {
			browserCfg.MaxPagesPerBrowser = jobConc
		}
		opts = runner.AppendBrowserCapacityOptions(opts, &browserCfg)

		// 提速：如果没配置浏览器容量，给一个默认的优化值
		// 一个浏览器开 4 个页面，省内存换并发
		if browserCfg.MaxPagesPerBrowser <= 1 && browserCfg.BrowserPoolSize <= 0 {
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
