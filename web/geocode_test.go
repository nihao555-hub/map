//nolint:testpackage // shares the internal web test package with web_test.go
package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHasGeoAnchor(t *testing.T) {
	tests := []struct {
		lat, lon string
		want     bool
	}{
		{"", "", false},
		{"0", "0", false},       // 表单默认值（几内亚湾）视为未锚定
		{"0.0", "0.0", false},
		{"abc", "100", false},   // 非法值
		{"13.7563", "", false},
		{"13.7563", "100.5018", true},
		{" 13.7563 ", "100.5018", true}, // 首尾空格
		{"0", "100.5018", true},         // 只有一个分量为 0 仍是有效坐标
		{"-33.8688", "151.2093", true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s,%s", tt.lat, tt.lon), func(t *testing.T) {
			if got := hasGeoAnchor(tt.lat, tt.lon); got != tt.want {
				t.Fatalf("hasGeoAnchor(%q, %q) = %v, want %v", tt.lat, tt.lon, got, tt.want)
			}
		})
	}
}

func TestLangForCountryCode(t *testing.T) {
	tests := []struct {
		cc   string
		want string
	}{
		{"th", "th"},
		{"TH", "th"}, // 大小写不敏感
		{"cn", "zh"},
		{"jp", "ja"},
		{"us", "en"},
		{"xx", ""}, // 未覆盖的国家保留用户语言
		{"", ""},
	}

	for _, tt := range tests {
		if got := langForCountryCode(tt.cc); got != tt.want {
			t.Fatalf("langForCountryCode(%q) = %q, want %q", tt.cc, got, tt.want)
		}
	}
}

func TestGeocode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent header")
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{
			"lat": "13.7563309",
			"lon": "100.5017651",
			"boundingbox": ["13.5", "14.0", "100.3", "100.7"],
			"address": {"country_code": "th"}
		}]`)
	}))
	defer srv.Close()

	orig := nominatimSearchURL
	nominatimSearchURL = srv.URL

	defer func() { nominatimSearchURL = orig }()

	point, err := Geocode(context.Background(), "Bangkok")
	if err != nil {
		t.Fatalf("Geocode: %v", err)
	}

	if point.Lat != 13.7563309 || point.Lon != 100.5017651 {
		t.Fatalf("unexpected coords: %f,%f", point.Lat, point.Lon)
	}

	if point.CountryCode != "th" {
		t.Fatalf("unexpected country code: %q", point.CountryCode)
	}

	if point.MinLat != 13.5 || point.MaxLat != 14.0 || point.MinLon != 100.3 || point.MaxLon != 100.7 {
		t.Fatalf("unexpected bbox: %+v", point)
	}
}

func TestGeocodeNoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	orig := nominatimSearchURL
	nominatimSearchURL = srv.URL

	defer func() { nominatimSearchURL = orig }()

	_, err := Geocode(context.Background(), "不存在的地点xyz")
	if err == nil {
		t.Fatal("expected error for empty results, got nil")
	}
}

func TestGeocodeEmptyQuery(t *testing.T) {
	if _, err := Geocode(context.Background(), "  "); err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
}

func TestGeocodeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	orig := nominatimSearchURL
	nominatimSearchURL = srv.URL

	defer func() { nominatimSearchURL = orig }()

	_, err := Geocode(context.Background(), "Bangkok")
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}
