//nolint:testpackage // tests unexported handlers (viewJob, requestWithID, securityHeaders) directly
package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, dir string) *Server {
	t.Helper()

	srv, err := New(NewService(nil, dir), ":0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return srv
}

func TestServeAgentWebPContentType(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/agent/decor/logo-m.webp", http.NoBody)
	rec := httptest.NewRecorder()

	srv.serveAgentApp(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/webp" {
		t.Fatalf("expected image/webp, got %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("expected immutable cache, got %q", got)
	}
}

func TestViewJobRendersPlaces(t *testing.T) {
	dir := t.TempDir()
	id := "11111111-1111-1111-1111-111111111111"

	csv := "title,latitude,longitude\nPlace,1.5,2.5\n"
	if err := os.WriteFile(filepath.Join(dir, id+".csv"), []byte(csv), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	srv := newTestServer(t, dir)

	// 默认 lite：壳子快开，places 由前端 API 拉取
	req := requestWithID(httptest.NewRequest(http.MethodGet, "/view?id="+id, http.NoBody))
	rec := httptest.NewRecorder()
	srv.viewJob(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{`id="map-modal"`, `initJobMap()`, `var places = [];`, `LITE_MODE = true`, `job-intel-popup`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}

	// lite=0 仍可嵌入全量 places（兼容）
	reqFull := requestWithID(httptest.NewRequest(http.MethodGet, "/view?id="+id+"&lite=0", http.NoBody))
	recFull := httptest.NewRecorder()
	srv.viewJob(recFull, reqFull)
	if recFull.Code != http.StatusOK {
		t.Fatalf("lite=0 expected 200, got %d", recFull.Code)
	}
	full := recFull.Body.String()
	for _, want := range []string{`"title":"Place"`, `"latitude":1.5`, `LITE_MODE = false`} {
		if !strings.Contains(full, want) {
			t.Fatalf("lite=0 body missing %q:\n%s", want, full)
		}
	}
}

func TestViewJobEmptyState(t *testing.T) {
	srv := newTestServer(t, t.TempDir())

	id := "22222222-2222-2222-2222-222222222222"
	req := requestWithID(httptest.NewRequest(http.MethodGet, "/view?id="+id, http.NoBody))
	rec := httptest.NewRecorder()
	srv.viewJob(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "var places = [];") {
		t.Fatalf("expected empty places array, got:\n%s", body)
	}
}

func TestViewJobInvalidID(t *testing.T) {
	srv := newTestServer(t, t.TempDir())

	req := requestWithID(httptest.NewRequest(http.MethodGet, "/view?id=not-a-uuid", http.NoBody))
	rec := httptest.NewRecorder()
	srv.viewJob(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
}

func TestDownloadCSVServesAttachment(t *testing.T) {
	dir := t.TempDir()
	id := "33333333-3333-3333-3333-333333333333"
	csvBody := "title,phone\nShop,123\n"
	if err := os.WriteFile(filepath.Join(dir, id+".csv"), []byte(csvBody), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	srv := newTestServer(t, dir)
	req := requestWithID(httptest.NewRequest(http.MethodGet, "/download?id="+id, http.NoBody))
	rec := httptest.NewRecorder()
	srv.download(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".csv") {
		t.Fatalf("Content-Disposition=%q", cd)
	}
	body := rec.Body.Bytes()
	if len(body) < 3 || body[0] != 0xEF || body[1] != 0xBB || body[2] != 0xBF {
		t.Fatalf("expected UTF-8 BOM prefix, got %v", body[:min(8, len(body))])
	}
	if !strings.Contains(string(body), "title,phone") {
		t.Fatalf("body missing csv: %q", rec.Body.String())
	}
}

func TestSecurityHeadersAllowMapResources(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"tile.openstreetmap.org",
		"cdnjs.cloudflare.com",
		"basemaps.cartocdn.com",
		"unpkg.com",
		"nominatim.openstreetmap.org",
		"*.is.autonavi.com",
		"*.googleusercontent.com",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP missing %q: %s", want, csp)
		}
	}
}
