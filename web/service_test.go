//nolint:testpackage // shares the internal web test package with web_test.go
package web

import (
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCountCSVResults(t *testing.T) {
	tests := []struct {
		name   string
		csv    string
		count  int
		latest []string
	}{
		{
			name:   "header only",
			csv:    "title,address\n",
			count:  0,
			latest: nil,
		},
		{
			name:   "counts rows and keeps latest five names",
			csv:    "title,address\nOne,1\nTwo,2\nThree,3\nFour,4\nFive,5\nSix,6\n",
			count:  6,
			latest: []string{"Two", "Three", "Four", "Five", "Six"},
		},
		{
			name:   "handles quoted commas and incomplete trailing row",
			csv:    "title,address\n\"Coffee, One\",A\n\"Coffee, Two\",B\n\"Incomplete",
			count:  2,
			latest: []string{"Coffee, One", "Coffee, Two"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, latest, err := countCSVResults(strings.NewReader(tt.csv))
			if err != nil {
				t.Fatalf("countCSVResults: %v", err)
			}

			if count != tt.count {
				t.Fatalf("count = %d, want %d", count, tt.count)
			}

			if strings.Join(latest, "|") != strings.Join(tt.latest, "|") {
				t.Fatalf("latest = %v, want %v", latest, tt.latest)
			}
		})
	}
}

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		seconds int64
		want    string
	}{
		{seconds: 0, want: "00:00"},
		{seconds: 7, want: "00:07"},
		{seconds: 65, want: "01:05"},
		{seconds: 3661, want: "61:01"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := formatElapsed(tt.seconds); got != tt.want {
				t.Fatalf("formatElapsed(%d) = %q, want %q", tt.seconds, got, tt.want)
			}
		})
	}
}

func TestParsePlacesHandlesMultiValueAndMissingColumns(t *testing.T) {
	input := `title,category,emails,images,review_rating
Cafe,"[""coffee"",""cafe""]","[""a@example.com"",""b@example.com""]","[{""image"":""https://example.com/a.jpg""}]",4.5
Minimal,,,,`

	places, err := parsePlaces(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parsePlaces: %v", err)
	}

	if places[0].Category != "coffee, cafe" || places[0].Email != "a@example.com, b@example.com" {
		t.Fatalf("multi-value fields = %#v", places[0])
	}

	if places[0].Images != "https://example.com/a.jpg" || places[1].Website != "" {
		t.Fatalf("image or missing-column parsing = %#v", places)
	}
}

func TestParsePlacesFormatsJSONFields(t *testing.T) {
	tests := []struct {
		name      string
		hours     string
		address   string
		ratings   string
		about     string
		menu      string
		wantHours string
		wantAddr  string
		wantStars string
		wantAbout string
		wantMenu  string
	}{
		{
			name:      "ordered hours and formatted address",
			hours:     `{"星期三":["10:00–22:00"],"星期一":["08:00–20:00"]}`,
			address:   `{"borough":"福田区","street":"CN 广东省 深圳市 福田区 深南大道 6005号","city":"深圳市","postal_code":"518042","state":"","country":"CN"}`,
			ratings:   `{"1":0,"2":3,"3":0,"4":3,"5":20}`,
			about:     `[{"name":"服务","options":[{"name":"堂食","enabled":true},{"name":"外带","enabled":false}]}]`,
			menu:      `{"link":"https://example.com/menu","source":"google"}`,
			wantHours: "星期三 10:00–22:00 · 星期一 08:00–20:00",
			wantAddr:  "CN 广东省 深圳市 福田区 深南大道 6005号",
			wantStars: "5★ 20 · 4★ 3 · 2★ 3",
			wantAbout: "服务: 堂食",
			wantMenu:  "https://example.com/menu",
		},
		{
			name:     "zero ratings and blank menu",
			hours:    `{}`,
			address:  `{"borough":"福田区","city":"深圳市","country":"CN"}`,
			wantAddr: "福田区 深圳市",
			ratings:  `{"1":0,"2":0,"3":0,"4":0,"5":0}`,
			about:    `null`,
			menu:     `{"link":"","source":""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var builder strings.Builder
			writer := csv.NewWriter(&builder)
			_ = writer.Write([]string{"title", "open_hours", "complete_address", "reviews_per_rating", "about", "menu"})
			_ = writer.Write([]string{"place", tt.hours, tt.address, tt.ratings, tt.about, tt.menu})
			writer.Flush()

			input := builder.String()
			places, err := parsePlaces(strings.NewReader(input))

			if err != nil {
				t.Fatalf("parsePlaces: %v", err)
			}

			place := places[0]
			if place.OpenHours != tt.wantHours || place.CompleteAddress != tt.wantAddr ||
				place.ReviewsPerRating != tt.wantStars || place.Descriptions != tt.wantAbout ||
				place.Menu != tt.wantMenu {
				t.Fatalf("formatted place = %#v", place)
			}
		})
	}
}

func writeCSV(t *testing.T, dir, id, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, id+".csv"), []byte(content), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}
}

func TestGetPlacesParsesCSV(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nil, dir)

	csv := "title,address,latitude,longitude,link,category,phone,website,review_rating\n" +
		"Coffee Place,1 Main St,37.7749,-122.4194,http://maps/1,cafe,555,http://web,4.5\n"
	writeCSV(t, dir, "job-1", csv)

	places, err := svc.GetPlaces(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("GetPlaces: %v", err)
	}

	if len(places) != 1 {
		t.Fatalf("expected 1 place, got %d", len(places))
	}

	p := places[0]
	if p.Title != "Coffee Place" || p.Address != "1 Main St" || p.Website != "http://web" {
		t.Fatalf("unexpected place: %+v", p)
	}

	if p.ReviewRating != 4.5 {
		t.Fatalf("unexpected rating: %v", p.ReviewRating)
	}
}

func TestGetPlacesParsesRowsWithoutCoordinates(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nil, dir)

	csv := "title,latitude,longitude\n" +
		"No Coords,,\n" +
		"Zero,0,0\n" +
		"Bad,abc,def\n" +
		"Good,1.5,2.5\n"
	writeCSV(t, dir, "job-2", csv)

	places, err := svc.GetPlaces(context.Background(), "job-2")
	if err != nil {
		t.Fatalf("GetPlaces: %v", err)
	}

	if len(places) != 4 {
		t.Fatalf("expected 4 places, got %d", len(places))
	}

	if places[0].Title != "No Coords" {
		t.Fatalf("unexpected place: %+v", places[0])
	}
}

func TestGetPlacesKeepsInvalidRatingsAsZero(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nil, dir)

	csv := "title,latitude,longitude,review_rating\n" +
		"NaN,NaN,2.5,3\n" +
		"Inf,Inf,2.5,3\n" +
		"OutOfRange,91,200,3\n" +
		"BadRating,1.5,2.5,NaN\n" +
		"Good,1.5,2.5,4.5\n"
	writeCSV(t, dir, "job-nf", csv)

	places, err := svc.GetPlaces(context.Background(), "job-nf")
	if err != nil {
		t.Fatalf("GetPlaces: %v", err)
	}

	if len(places) != 5 {
		t.Fatalf("expected 5 places, got %d: %+v", len(places), places)
	}

	for _, p := range places {
		if p.Title == "BadRating" && p.ReviewRating != 0 {
			t.Fatalf("non-finite rating should be sanitized to 0, got %v", p.ReviewRating)
		}
	}
}

func TestGetPlacesMissingCSV(t *testing.T) {
	svc := NewService(nil, t.TempDir())

	if _, err := svc.GetPlaces(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for missing csv")
	}
}

func TestGetPlacesRejectsTraversal(t *testing.T) {
	svc := NewService(nil, t.TempDir())

	if _, err := svc.GetPlaces(context.Background(), "../etc/passwd"); err == nil {
		t.Fatal("expected error for path traversal")
	}
}
