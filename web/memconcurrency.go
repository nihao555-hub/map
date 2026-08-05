package web

import (
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
)

// Memory-aware concurrency for small VPS hosts.
// Inspired by Google crawler practice: keep membership tests cheap (bloom/hash)
// and shrink worker fan-out under memory pressure instead of OOM-killing browsers.

const (
	// Deep Playwright jobs need headroom (~400–700MB per active browser worker).
	deepReserveMBPerWorker = 450
	fastReserveMBPerWorker = 80
	minFreeMemoryMB        = 400
)

var (
	memStatMu  sync.Mutex
	memStatAt  time.Time
	memAvailMB uint64
	memTotalMB uint64
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
		// Fallback: assume modest host
		return 1500
	}
	memAvailMB = vm.Available / (1024 * 1024)
	memTotalMB = vm.Total / (1024 * 1024)
	memStatAt = time.Now()
	return memAvailMB
}

// AdaptiveJobConcurrency returns how many scrape jobs may run in parallel
// given current free memory. Caps at JobConcurrency() env setting.
func AdaptiveJobConcurrency() int {
	base := JobConcurrency()
	avail := AvailableMemoryMB()
	if avail <= minFreeMemoryMB {
		return 1
	}
	// Deep-mode only product: reserve for browsers
	byMem := int((avail - minFreeMemoryMB) / deepReserveMBPerWorker)
	if byMem < 1 {
		byMem = 1
	}
	if byMem > base {
		byMem = base
	}
	return byMem
}

// AdaptivePerJobConcurrency derives scrapemate workers for one job under RAM pressure.
// Prefer deep quality: never explode Playwright page count on small hosts.
func AdaptivePerJobConcurrency(configured int, fastMode bool) int {
	base := PerJobScrapemateConcurrency(configured, fastMode)
	avail := AvailableMemoryMB()
	reserve := uint64(deepReserveMBPerWorker)
	if fastMode {
		reserve = fastReserveMBPerWorker
	}
	slots := AdaptiveJobConcurrency()
	if slots < 1 {
		slots = 1
	}
	budget := int(avail / reserve)
	if budget < 1 {
		budget = 1
	}
	perJob := budget / slots
	if perJob < 1 {
		perJob = 1
	}
	if perJob > base {
		perJob = base
	}
	if !fastMode && perJob > 2 {
		perJob = 2
	}
	if configured > 0 && perJob > configured {
		perJob = configured
	}
	return perJob
}

// UseBloomDeduper reports whether large-grid jobs should switch to bloom membership.
// Enable with GMS_BLOOM_DEDUP=1 (default on when free RAM < 1.5GB).
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

// LogMemoryPressure emits a one-line diagnostic for ops.
func LogMemoryPressure(tag string) {
	avail := AvailableMemoryMB()
	log.Printf("%s mem: avail=%dMB total≈%dMB jobs=%d/%d gomaxprocs=%d bloom=%v",
		tag, avail, memTotalMB, AdaptiveJobConcurrency(), JobConcurrency(),
		runtime.GOMAXPROCS(0), UseBloomDeduper())
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
