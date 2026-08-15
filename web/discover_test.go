package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosom/google-maps-scraper/engine"
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
	for _, want := range []string{
		"智能引擎搜索", "discover-form", "发开发信", "地图获客", "社媒主页", "不是地图搜店",
		"私信模式", "营销模式", "一键营销", "preview-pane", "共 0 条", "/static/js/discover.js",
		`id="app-rail"`, "/static/css/shell.css", "rail-item is-active",
		"智能引擎能为你做什么", "精确", "试试", "wmt-features", "wmt-ai-orb", "开发信跟进",
		"请输入企业或商品名称", "批发", "国家/地区", "wmt-country",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}

	if strings.Contains(body, "wmt-side") || strings.Contains(body, "wmt-tabs") {
		t.Fatal("discover should use the map app-rail, not a second sidebar")
	}

	if strings.Contains(body, "工作台") || strings.Contains(body, "全球搜索") || strings.Contains(body, "智能推荐") || strings.Contains(body, "展会买家") || strings.Contains(body, "市场洞察") {
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
	for _, want := range []string{"/api/v1/discover/platforms", "/api/v1/discover/search", "/api/v1/discover/preview", "/api/v1/discover/countries", "plat-logo", "showPreview", "已找到", "PAGE_SIZE", "limit: 0", "搜索繁忙，请稍后再试。", "cell-clip", "shortHandle", "validateKeyword", "precise: isPrecise", "isHomepageHit"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, forbid := range []string{"data.sources", "data.warnings", "took_ms", "duckduckgo"} {
		if strings.Contains(body, forbid) {
			t.Fatalf("technical field %q leaked in JS", forbid)
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

func TestDiscoverCountriesListsMarkets(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discover/countries", nil)
	rec := httptest.NewRecorder()
	srv.apiDiscoverCountries(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"不限", "马来西亚", "美国", `"code":"MY"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
}

func TestScrubDiscoverResultStripsEngineNames(t *testing.T) {
	res := engine.Result{
		Hits: []engine.Hit{{
			Name:        "厂",
			HomepageURL: "https://www.facebook.com/factory",
			Source:      "bing",
		}},
		Sources:  []string{"bing", "duckduckgo"},
		Warnings: []string{"duckduckgo: status 429 rate limited"},
		TookMS:   12,
		Note:     "内部诊断",
	}
	scrubDiscoverResult(&res)
	if res.Hits[0].Source != "" || len(res.Sources) != 0 || res.Warnings != nil || res.TookMS != 0 {
		t.Fatalf("not scrubbed %+v", res)
	}
	if res.Note != "系统不会代发。" {
		t.Fatalf("note=%s", res.Note)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, forbid := range []string{"bing", "duckduckgo", "429"} {
		if strings.Contains(body, forbid) {
			t.Fatalf("leaked %q in %s", forbid, body)
		}
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

func TestDiscoverSearchRejectsWeakKeyword(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	for _, body := range []string{
		`{"keyword":"的","kind":"people"}`,
		`{"keyword":"搜索","kind":"people"}`,
		`{"keyword":"啊","kind":"people"}`,
		`{"keyword":"配电","kind":"people","precise":true}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/discover/search", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		srv.apiDiscoverSearch(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d body=%s for %s", rec.Code, rec.Body.String(), body)
		}
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
	if warns, ok := payload["warnings"].([]any); ok && len(warns) > 0 {
		t.Fatalf("warnings leaked to UI: %v", warns)
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
	if !strings.Contains(body, "davidteather/TikTok-Api") || !strings.Contains(body, "Johnserf-Seed/f2") || !strings.Contains(body, "public-websearch") || !strings.Contains(body, "s0md3v/Photon") {
		t.Fatalf("body=%s", body)
	}
}

func TestDiscoverPreviewRejectsLocalhost(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discover/preview?url=http://127.0.0.1/", nil)
	rec := httptest.NewRecorder()
	srv.apiDiscoverPreview(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
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
	if !strings.Contains(body, "outreach-emails") || !strings.Contains(body, "不会代发") {
		t.Fatalf("outreach should accept emails query: %s", body)
	}
	if !strings.Contains(body, `id="app-rail"`) || !strings.Contains(body, "/static/css/shell.css") {
		t.Fatal("outreach should share the map app-rail")
	}
	if strings.Contains(body, "wmt-side") || strings.Contains(body, "wmt-tabs") {
		t.Fatal("outreach should use the map app-rail, not a second sidebar")
	}
}
