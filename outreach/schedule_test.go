package outreach_test

import (
	"testing"
	"time"

	"github.com/gosom/google-maps-scraper/outreach"
)

func TestNextSendTimeUsesRecipientLocalWindow(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}

	// Monday at 17:00 New York time should move to Tuesday 08:00.
	after := time.Date(2026, time.August, 10, 21, 0, 0, 0, time.UTC)
	window := outreach.SendWindow{StartHour: 8, EndHour: 11, Days: "234"}

	got := outreach.NextSendTime(after, location, window)
	want := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)

	if !got.Equal(want) {
		t.Fatalf("NextSendTime() = %s, want %s", got, want)
	}
}

func TestAllowedTodayRampsToHardCap(t *testing.T) {
	t.Parallel()

	settings := outreach.DefaultSettings()
	settings.DailyCap = 40
	settings.WarmupStart = 15
	settings.WarmupStep = 5

	tests := []struct {
		days int
		want int
	}{
		{days: 0, want: 15},
		{days: 2, want: 25},
		{days: 20, want: 40},
	}

	for _, test := range tests {
		if got := settings.AllowedToday(test.days); got != test.want {
			t.Fatalf("AllowedToday(%d) = %d, want %d", test.days, got, test.want)
		}
	}
}
