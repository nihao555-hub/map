package web_test

import (
	"testing"

	"github.com/gosom/google-maps-scraper/web"
)

func TestStablePlaceKeyFallback(t *testing.T) {
	p := web.Place{Title: "ABtrade", Latitude: -6.256038, Longitude: 106.827557}
	k := web.StablePlaceKey(p)
	if k == "" || k[:4] != "geo_" {
		t.Fatalf("want geo_*, got %q", k)
	}
	p2 := p
	p2.PlaceID = "ChIJ123"
	if web.StablePlaceKey(p2) != "ChIJ123" {
		t.Fatalf("prefer place_id")
	}
}
