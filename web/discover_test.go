package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverPageRenders(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/discover", nil)
	rec := httptest.NewRecorder()
	srv.discoverPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}

	body := rec.Body.String()
  for _, want := range []string{"智能引擎搜索", "discover-form", "app-rail", "Facebook", "LinkedIn"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}

	if !strings.Contains(body, "地图搜索") {
		t.Fatal("rail should include 地图搜索")
	}
}

func TestDiscoverSearchRequiresKeyword(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/discover/search", bytes.NewBufferString(`{"kind":"people"}`))
	rec := httptest.NewRecorder()
	srv.apiDiscoverSearch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDiscoverExhibitionDoesNotCrawl(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/discover/search", bytes.NewBufferString(`{"keyword":"CES","kind":"exhibition"}`))
	rec := httptest.NewRecorder()
	srv.apiDiscoverSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	hits, _ := payload["hits"].([]any)
	if len(hits) != 0 {
		t.Fatalf("hits=%v", hits)
	}
}

func TestDiscoverSourcesListsOSS(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discover/sources", nil)
	rec := httptest.NewRecorder()
	srv.apiDiscoverSources(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "davidteather/TikTok-Api") || !strings.Contains(body, "Johnserf-Seed/f2") || !strings.Contains(body, "public-websearch") {
		t.Fatalf("body=%s", body)
	}
}
