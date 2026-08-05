//nolint:testpackage // shares the internal web test package with web_test.go
package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeCSV(t *testing.T, dir, id, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, id+".csv"), []byte(content), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}
}

func TestGetPlacesSortsEmailFirst(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "job-email.csv")
	content := "title,address,latitude,longitude,link,category,phone,website,review_rating,review_count,emails\n" +
		"OnlyPhone,Addr,1.0,2.0,http://a,cat,111,,4.0,10,\n" +
		"HasEmail,Addr,1.1,2.1,http://b,cat,222,http://b.com,3.0,5,a@b.com\n" +
		"Neither,Addr,1.2,2.2,http://c,cat,,,5.0,20,\n"
	if err := os.WriteFile(csvPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := NewService(nil, dir)
	places, err := svc.GetPlaces(context.Background(), "job-email")
	if err != nil {
		t.Fatalf("GetPlaces: %v", err)
	}
	if len(places) != 3 {
		t.Fatalf("len=%d", len(places))
	}
	if places[0].Title != "HasEmail" {
		t.Fatalf("want email-first, got %q", places[0].Title)
	}
	if places[1].Title != "OnlyPhone" {
		t.Fatalf("want phone second, got %q", places[1].Title)
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
	if p.Title != "Coffee Place" || p.Latitude != 37.7749 || p.Longitude != -122.4194 {
		t.Fatalf("unexpected place: %+v", p)
	}

	if p.ReviewRating != 4.5 {
		t.Fatalf("unexpected rating: %v", p.ReviewRating)
	}
}

func TestGetPlacesSkipsRowsWithoutCoords(t *testing.T) {
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

	if len(places) != 1 {
		t.Fatalf("expected 1 place, got %d", len(places))
	}

	if places[0].Title != "Good" {
		t.Fatalf("unexpected place: %+v", places[0])
	}
}

func TestGetPlacesSkipsNonFiniteAndOutOfRangeCoords(t *testing.T) {
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

	if len(places) != 2 {
		t.Fatalf("expected 2 places (BadRating + Good), got %d: %+v", len(places), places)
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

func TestJobConcurrencyDefaultIsFour(t *testing.T) {
	t.Setenv("GMS_WEB_JOB_CONCURRENCY", "")
	if got := JobConcurrency(); got != 4 {
		t.Fatalf("default=%d want 4", got)
	}
	t.Setenv("GMS_WEB_JOB_CONCURRENCY", "3")
	if got := JobConcurrency(); got != 3 {
		t.Fatalf("override=%d want 3", got)
	}
	t.Setenv("GMS_WEB_JOB_CONCURRENCY", "99")
	if got := JobConcurrency(); got != 64 {
		t.Fatalf("cap=%d want 64", got)
	}
	t.Setenv("GMS_WEB_JOB_CONCURRENCY", "4")
	if got := PerJobScrapemateConcurrency(16, true); got != 4 {
		t.Fatalf("per-job fast=%d want 4", got)
	}
	if got := PerJobScrapemateConcurrency(16, false); got != 2 {
		t.Fatalf("per-job deep=%d want 2", got)
	}
}
