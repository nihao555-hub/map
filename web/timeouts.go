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
	DefaultStaleWorking = 25 * time.Minute

	// DefaultJobWallCap is the absolute maximum wall-clock time one scrape may
	// hold an admission slot, even if MaxTime / grid estimates are larger.
	DefaultJobWallCap = 150 * time.Minute

	// DefaultJobWallGrace is added on top of MaxTime for setup + email budget.
	DefaultJobWallGrace = 30 * time.Minute
)

// StaleWorkingAge returns the no-heartbeat timeout for zombie working jobs.
// Override with GMS_STALE_WORKING (e.g. "25m", "1h").
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
