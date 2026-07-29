package geocode_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gosom/google-maps-scraper/geocode"
)

func TestBoundingBox(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "Bangkok" {
			t.Errorf("unexpected query %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"boundingbox":["13.4940861","13.9548994","100.3278135","100.9378222"]}]`))
	}))
	defer srv.Close()

	client := &geocode.Client{Endpoint: srv.URL, HTTP: srv.Client()}

	bbox, err := client.BoundingBox(context.Background(), "Bangkok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bbox.MinLat != 13.4940861 || bbox.MaxLat != 13.9548994 {
		t.Errorf("unexpected latitudes: %+v", bbox)
	}

	if bbox.MinLon != 100.3278135 || bbox.MaxLon != 100.9378222 {
		t.Errorf("unexpected longitudes: %+v", bbox)
	}
}

func TestBoundingBoxNoResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := &geocode.Client{Endpoint: srv.URL, HTTP: srv.Client()}

	if _, err := client.BoundingBox(context.Background(), "nowhere"); err == nil {
		t.Fatal("expected an error")
	}
}
