package placecache

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreLookupRoundTrip(t *testing.T) {
	dir := t.TempDir()
	Close()
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)
	SetTTL(time.Hour)

	Store(&CachedPlace{
		Key:      "pid:ChIJtestplace0000000000000",
		PlaceID:  "ChIJtestplace0000000000000",
		Title:    "Kopi Test",
		Phone:    "+628111",
		WebSite:  "https://example.test",
		Emails:   []string{"a@example.test"},
		Link:     "https://www.google.com/maps/place/Kopi/@-6.2,106.8,15z/data=!4m6!3m5!1s0x2:0x3!8m2!3d-6.2!4d106.8!16s%2Fg%2F11test",
		Latitude: -6.2, Longitude: 106.8,
	})

	got := Lookup("pid:ChIJtestplace0000000000000")
	if got == nil || got.Title != "Kopi Test" || got.Phone != "+628111" || len(got.Emails) != 1 {
		t.Fatalf("lookup miss/wrong: %+v", got)
	}
	h, m, s := Stats()
	if h < 1 || s < 1 {
		t.Fatalf("stats hits=%d misses=%d stores=%d", h, m, s)
	}
	_ = filepath.Join(dir, "place_cache.db")
}

func TestTTLExpiry(t *testing.T) {
	dir := t.TempDir()
	Close()
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)
	SetTTL(10 * time.Millisecond)
	Store(&CachedPlace{Key: "pid:ChIJexpired000000000000000", Title: "Old"})
	time.Sleep(30 * time.Millisecond)
	if Lookup("pid:ChIJexpired000000000000000") != nil {
		t.Fatal("expected TTL miss")
	}
}
