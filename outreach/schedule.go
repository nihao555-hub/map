package outreach

import (
	"math/rand/v2"
	"strings"
	"time"
)

// maxScheduleLookahead bounds the search for the next allowed send slot.
const maxScheduleLookahead = 21 // days

// SendWindow describes when cold emails may go out, expressed in the
// recipient's local time. Data across the industry consistently shows the
// highest open/reply rates on Tuesday-Thursday mornings (roughly 8-11 AM
// local time), which is why those are the defaults.
type SendWindow struct {
	StartHour int
	EndHour   int
	// Days holds ISO weekday digits ("1"=Monday ... "7"=Sunday).
	Days string
}

// Window extracts the send window from the settings.
func (s *Settings) Window() SendWindow {
	w := SendWindow{
		StartHour: s.SendStartHour,
		EndHour:   s.SendEndHour,
		Days:      s.SendDays,
	}

	if w.StartHour <= 0 && w.EndHour <= 0 {
		w.StartHour = 8
		w.EndHour = 11
	}

	if w.EndHour <= w.StartHour {
		w.EndHour = w.StartHour + 3
	}

	if w.Days == "" {
		w.Days = "234"
	}

	return w
}

func (w *SendWindow) dayAllowed(t time.Time) bool {
	iso := int(t.Weekday())
	if iso == 0 {
		iso = 7 // Sunday
	}

	return strings.ContainsRune(w.Days, rune('0'+iso))
}

// NextSendTime returns the earliest instant at or after "after" that falls
// inside the send window in the given location. The result is in UTC.
func NextSendTime(after time.Time, loc *time.Location, w SendWindow) time.Time {
	local := after.In(loc)

	for range maxScheduleLookahead {
		candidate := local

		if !w.dayAllowed(candidate) {
			local = startOfDay(candidate.AddDate(0, 0, 1))

			continue
		}

		windowStart := time.Date(candidate.Year(), candidate.Month(), candidate.Day(), w.StartHour, 0, 0, 0, loc)
		windowEnd := time.Date(candidate.Year(), candidate.Month(), candidate.Day(), w.EndHour, 0, 0, 0, loc)

		switch {
		case candidate.Before(windowStart):
			return windowStart.UTC()
		case candidate.Before(windowEnd):
			return candidate.UTC()
		default:
			local = startOfDay(candidate.AddDate(0, 0, 1))
		}
	}

	// Malformed window: fall back to sending immediately rather than never.
	return after.UTC()
}

// NextSendTimeJittered spreads sends across the window instead of stacking
// everything at the window opening. It keeps a safety margin so the jittered
// time never leaves the window.
func NextSendTimeJittered(after time.Time, loc *time.Location, w SendWindow) time.Time {
	base := NextSendTime(after, loc, w)

	windowMinutes := (w.EndHour - w.StartHour) * 60
	if windowMinutes <= 30 {
		return base
	}

	//nolint:gosec // non-cryptographic jitter for send-time spreading
	jitter := time.Duration(rand.IntN(windowMinutes-30)) * time.Minute
	jittered := base.Add(jitter)

	// Re-validate: if jitter pushed us outside the window, keep the base.
	if NextSendTime(jittered, loc, w).Equal(jittered) {
		return jittered
	}

	return base
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// LoadLocation resolves an IANA timezone name with a fallback.
func LoadLocation(name, fallback string) *time.Location {
	if name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}

	if fallback != "" {
		if loc, err := time.LoadLocation(fallback); err == nil {
			return loc
		}
	}

	return time.UTC
}

// AllowedToday computes the daily cap with warm-up ramping: a fresh mailbox
// starts at WarmupStart emails/day and grows by WarmupStep per active day
// until DailyCap is reached. daysActive is the number of days since the very
// first send from this mailbox.
func (s *Settings) AllowedToday(daysActive int) int {
	cap_ := s.DailyCap
	if cap_ <= 0 {
		cap_ = 40
	}

	start := s.WarmupStart
	if start <= 0 {
		return cap_
	}

	step := s.WarmupStep
	if step < 0 {
		step = 0
	}

	allowed := start + step*daysActive
	if allowed > cap_ {
		return cap_
	}

	return allowed
}

// SendGap returns a randomized human-like pause between two consecutive
// sends.
func (s *Settings) SendGap() time.Duration {
	minGap := s.MinGapSeconds
	if minGap <= 0 {
		minGap = 90
	}

	maxGap := s.MaxGapSeconds
	if maxGap <= minGap {
		maxGap = minGap + 60
	}

	//nolint:gosec // non-cryptographic jitter between sends
	return time.Duration(minGap+rand.IntN(maxGap-minGap)) * time.Second
}
