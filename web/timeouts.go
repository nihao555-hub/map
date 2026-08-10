package web

import (
	"os"
	"strings"
	"time"
)

const (
	// DefaultStaleWorking is how long a "working" job may go without a heartbeat
	// before it is treated as a zombie (crash / hung browser) and failed so it
	// cannot block the queue or confuse operators.
	// Heartbeats tick every ~60s; 10m of silence means the worker is gone.
	DefaultStaleWorking = 10 * time.Minute

	// DefaultProgressStall is how long a live job may write zero new CSV rows
	// before the runner cancels Playwright and frees the admit slot. Heartbeat
	// alone is not enough: a hung browser can keep TouchJob alive forever.
	DefaultProgressStall = 15 * time.Minute

	// DefaultJobWallCap is the absolute maximum wall-clock time one scrape may
	// hold an admission slot, even if MaxTime / grid estimates are larger.
	DefaultJobWallCap = 150 * time.Minute

	// DefaultJobWallGrace is added on top of MaxTime for setup + email budget.
	DefaultJobWallGrace = 30 * time.Minute
)

// StaleWorkingAge returns the no-heartbeat timeout for zombie working jobs.
// Override with GMS_STALE_WORKING (e.g. "10m", "1h").
func StaleWorkingAge() time.Duration {
	v := strings.TrimSpace(os.Getenv("GMS_STALE_WORKING"))
	if v == "" {
		return DefaultStaleWorking
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < time.Minute {
		return DefaultStaleWorking
	}
	if d > 6*time.Hour {
		return 6 * time.Hour
	}
	return d
}

// ProgressStallAge returns how long a working scrape may stall (no new CSV rows)
// before forced cancel. Override with GMS_PROGRESS_STALL (e.g. "15m"). Set "0"
// to disable.
func ProgressStallAge() time.Duration {
	v := strings.TrimSpace(os.Getenv("GMS_PROGRESS_STALL"))
	if v == "" {
		return DefaultProgressStall
	}
	if v == "0" || strings.EqualFold(v, "off") {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 2*time.Minute {
		return DefaultProgressStall
	}
	if d > 2*time.Hour {
		return 2 * time.Hour
	}
	return d
}

// JobWallClock is the hard wall-clock budget for one scrape (including browser
// setup). After this, the runner cancels the job and frees the admit slot even
// if Playwright ignores context cancel.
// Override cap with GMS_JOB_WALL (e.g. "150m").
func JobWallClock(maxTime time.Duration) time.Duration {
	capWall := DefaultJobWallCap
	if v := strings.TrimSpace(os.Getenv("GMS_JOB_WALL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 30*time.Minute {
			capWall = d
		}
	}
	wall := maxTime + DefaultJobWallGrace
	if maxTime <= 0 {
		wall = 90 * time.Minute
	}
	if wall < 45*time.Minute {
		wall = 45 * time.Minute
	}
	if wall > capWall {
		wall = capWall
	}
	return wall
}
