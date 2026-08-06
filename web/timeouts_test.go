package web

import (
	"testing"
	"time"
)

func TestStaleWorkingAgeDefault(t *testing.T) {
	t.Setenv("GMS_STALE_WORKING", "")
	if got := StaleWorkingAge(); got != DefaultStaleWorking {
		t.Fatalf("got %s want %s", got, DefaultStaleWorking)
	}
	t.Setenv("GMS_STALE_WORKING", "40m")
	if got := StaleWorkingAge(); got != 40*time.Minute {
		t.Fatalf("got %s", got)
	}
}

func TestJobWallClock(t *testing.T) {
	t.Setenv("GMS_JOB_WALL", "")
	if got := JobWallClock(120 * time.Minute); got != 150*time.Minute {
		t.Fatalf("120m+grace capped: got %s", got)
	}
	if got := JobWallClock(30 * time.Minute); got != 60*time.Minute {
		t.Fatalf("30m+grace: got %s want 60m", got)
	}
	if got := JobWallClock(0); got != 90*time.Minute {
		t.Fatalf("zero max: got %s", got)
	}
}
