package web

import (
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
)

// Memory/CPU-aware concurrency for deep Playwright jobs.
//
// Design goal for multi-job speed:
//   Prefer FEWER full-speed jobs over MANY starved jobs.
//   Each admitted deep job keeps a fixed inner worker budget (usually 2);
//   new jobs wait in queue until a slot frees — so active scrapes don't slow down.
//
// Hundreds of concurrent deep jobs need horizontal workers (many processes/nodes),
// not one VPS oversubscribing browsers. See docs/operations-guide-zh.md.

const (
	// Deep Playwright jobs need headroom (~400–700MB per active browser worker).
	deepReserveMBPerWorker = 450
	fastReserveMBPerWorker = 80
	minFreeMemoryMB        = 400
	// Each deep job is assumed to want ~2 logical CPUs for stable page speed.
	deepCPUPerJob = 2
)

var (
	memStatMu  sync.Mutex
	memStatAt  time.Time
	memAvailMB uint64
	memTotalMB uint64

	// activeDeepJobs tracks currently running scrape jobs for fair admission.
	activeDeepJobs atomic.Int64
)

// AvailableMemoryMB returns approximate free+available RAM in MiB (cached ~2s).
func AvailableMemoryMB() uint64 {
	memStatMu.Lock()
	defer memStatMu.Unlock()
	if time.Since(memStatAt) < 2*time.Second && memAvailMB > 0 {
		return memAvailMB
	}
	vm, err := mem.VirtualMemory()
	if err != nil {
		return 1500
	}
	memAvailMB = vm.Available / (1024 * 1024)
	memTotalMB = vm.Total / (1024 * 1024)
	memStatAt = time.Now()
	return memAvailMB
}

// JobConcurrency is the hard cap on parallel scrape jobs (default 4).
// Override with GMS_WEB_JOB_CONCURRENCY. Raised max to 64 for worker fleets;
// a single small VPS should still keep this at 2–4.
func JobConcurrency() int {
	v := strings.TrimSpace(os.Getenv("GMS_WEB_JOB_CONCURRENCY"))
	if v == "" {
		return 4
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 4
	}
	if n > 64 {
		return 64
	}
	return n
}

// AdaptiveJobConcurrency is the fair-admission slot count for THIS process:
// min(env cap, memory budget, CPU budget). New jobs beyond this wait in queue
// instead of starting and slowing everyone down.
func AdaptiveJobConcurrency() int {
	base := JobConcurrency()
	avail := AvailableMemoryMB()
	if avail <= minFreeMemoryMB {
		return 1
	}
	byMem := int((avail - minFreeMemoryMB) / (deepReserveMBPerWorker * uint64(deepCPUPerJob)))
	if byMem < 1 {
		byMem = 1
	}
	byCPU := runtime.GOMAXPROCS(0) / deepCPUPerJob
	if byCPU < 1 {
		byCPU = 1
	}
	n := base
	if byMem < n {
		n = byMem
	}
	if byCPU < n {
		n = byCPU
	}
	if n < 1 {
		n = 1
	}
	return n
}

// ActiveDeepJobs returns how many scrape jobs are running in this process.
func ActiveDeepJobs() int {
	return int(activeDeepJobs.Load())
}

// BeginDeepJob marks a scrape as running (call once per job start).
func BeginDeepJob() { activeDeepJobs.Add(1) }

// EndDeepJob marks a scrape as finished.
func EndDeepJob() {
	for {
		cur := activeDeepJobs.Load()
		if cur <= 0 {
			activeDeepJobs.Store(0)
			return
		}
		if activeDeepJobs.CompareAndSwap(cur, cur-1) {
			return
		}
	}
}

// CanAdmitDeepJob reports whether starting another deep job would preserve
// full-speed budgets for already-running jobs.
func CanAdmitDeepJob() bool {
	slots := AdaptiveJobConcurrency()
	return ActiveDeepJobs() < slots
}

// ReservedPerJobConcurrency is the FIXED inner worker count for an admitted job.
// It does NOT shrink when more jobs are queued — admission control protects speed.
func ReservedPerJobConcurrency(configured int, fastMode bool) int {
	if configured < 1 {
		configured = 1
	}
	if fastMode {
		n := 4
		if configured < n {
			n = configured
		}
		if n > 6 {
			n = 6
		}
		if n < 1 {
			n = 1
		}
		return n
	}
	// Deep: always give full reserved budget (2), never dilute under load.
	n := 2
	if configured < n {
		n = configured
	}
	if n < 1 {
		n = 1
	}
	return n
}

// AdaptivePerJobConcurrency kept for compatibility; deep path uses reserved budget.
func AdaptivePerJobConcurrency(configured int, fastMode bool) int {
	return ReservedPerJobConcurrency(configured, fastMode)
}

// PerJobScrapemateConcurrency is the historical name; delegates to reserved budget.
func PerJobScrapemateConcurrency(configured int, fastMode bool) int {
	return ReservedPerJobConcurrency(configured, fastMode)
}

// UseBloomDeduper reports whether large-grid jobs should switch to bloom membership.
func UseBloomDeduper() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GMS_BLOOM_DEDUP")))
	switch v {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	default:
		return AvailableMemoryMB() < 1500
	}
}

// ConcurrencySnapshot is exposed to the UI / ops API.
type ConcurrencySnapshot struct {
	ActiveJobs    int    `json:"active_jobs"`
	AdmitSlots    int    `json:"admit_slots"`
	EnvCap        int    `json:"env_cap"`
	AvailMemMB    uint64 `json:"avail_mem_mb"`
	PerJobDeep    int    `json:"per_job_deep_workers"`
	Bloom         bool   `json:"bloom_dedup"`
	GOMAXPROCS    int    `json:"gomaxprocs"`
	FairAdmission bool   `json:"fair_admission"`
	ScaleHint     string `json:"scale_hint"`
}

// GetConcurrencySnapshot returns live concurrency numbers for the UI.
func GetConcurrencySnapshot() ConcurrencySnapshot {
	slots := AdaptiveJobConcurrency()
	return ConcurrencySnapshot{
		ActiveJobs:    ActiveDeepJobs(),
		AdmitSlots:    slots,
		EnvCap:        JobConcurrency(),
		AvailMemMB:    AvailableMemoryMB(),
		PerJobDeep:    ReservedPerJobConcurrency(16, false),
		Bloom:         UseBloomDeduper(),
		GOMAXPROCS:    runtime.GOMAXPROCS(0),
		FairAdmission: true,
		ScaleHint:     "单机公平准入：宁少勿慢。数百并发请水平扩展多个 -web worker（共享同一 jobs.db / 队列）。",
	}
}

// LogMemoryPressure emits a one-line diagnostic for ops.
func LogMemoryPressure(tag string) {
	s := GetConcurrencySnapshot()
	log.Printf("%s mem: avail=%dMB active=%d/%d(envCap=%d) perJobDeep=%d gomaxprocs=%d bloom=%v",
		tag, s.AvailMemMB, s.ActiveJobs, s.AdmitSlots, s.EnvCap, s.PerJobDeep, s.GOMAXPROCS, s.Bloom)
}

// BloomExpectedKeys returns sizing hint for bloom filter from env or default.
func BloomExpectedKeys() int {
	v := strings.TrimSpace(os.Getenv("GMS_BLOOM_EXPECTED_KEYS"))
	if v == "" {
		return 500_000
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1000 {
		return 500_000
	}
	return n
}
