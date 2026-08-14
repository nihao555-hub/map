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
	for _, want := range []string{"智能引擎搜索", "discover-form", "发开发信", "地图获客", "社媒主页", "不是地图搜店", "/static/js/discover.js"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}

	if strings.Contains(body, "工作台") || strings.Contains(body, "全球搜索") || strings.Contains(body, "一键营销") || strings.Contains(body, "智能推荐") || strings.Contains(body, "展会买家") || strings.Contains(body, "市场洞察") {
		t.Fatal("unshipped Waimao Tong modules should not appear")
	}
}

func TestDiscoverJSLoadsPlatformsFromAPI(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/static/js/discover.js", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{"/api/v1/discover/platforms", "/api/v1/discover/search", "plat-logo"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestDiscoverPlatformsListsSupportedOnly(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discover/platforms", nil)
	rec := httptest.NewRecorder()
	srv.apiDiscoverPlatforms(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Platforms []struct {
			ID      string `json:"id"`
			Label   string `json:"label"`
			Default bool   `json:"default"`
		} `json:"platforms"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}

	ids := map[string]bool{}
	for _, p := range payload.Platforms {
		ids[p.ID] = true
	}

	for _, want := range []string{
		"facebook", "linkedin", "instagram", "youtube", "tiktok", "douyin",
		"x", "pinterest", "threads", "xiaohongshu", "kuaishou", "weibo",
		"bilibili", "telegram", "reddit", "twitch",
	} {
		if !ids[want] {
			t.Fatalf("missing supported platform %s in %+v", want, payload.Platforms)
		}
	}

	if ids["exhibition"] || ids["customs"] {
		t.Fatalf("unsupported modules leaked into people platforms: %+v", payload.Platforms)
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

func TestOutreachPageRenders(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/outreach", nil)
	rec := httptest.NewRecorder()
	srv.outreachPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "发开发信") || !strings.Contains(body, "已经有邮箱") {
		t.Fatalf("body=%s", body)
	}
}
