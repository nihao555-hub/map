//nolint:testpackage // exercises the unexported outreachPage handler and template
package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosom/google-maps-scraper/outreach"
)

// stubJobRepo is an empty job repository so the outreach page can render
// without a real SQLite jobs database.
type stubJobRepo struct{}

func (stubJobRepo) Get(context.Context, string) (Job, error) {
	return Job{}, errors.New("not found")
}
func (stubJobRepo) Create(context.Context, *Job) error   { return nil }
func (stubJobRepo) Delete(context.Context, string) error { return nil }
func (stubJobRepo) Update(context.Context, *Job) error   { return nil }
func (stubJobRepo) Select(context.Context, SelectParams) ([]Job, error) {
	return nil, nil
}

// TestOutreachPageRendersFully guards against template execution errors (such
// as pointer-receiver methods on non-addressable fields) that would truncate
// the page and drop the trailing <script> tag.
func TestOutreachPageRendersFully(t *testing.T) {
	dir := t.TempDir()

	store, err := outreach.NewStore(filepath.Join(dir, "outreach.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	osvc := outreach.NewService(store, outreach.NewEngine(store, nil, nil), dir)

	srv, err := New(NewService(stubJobRepo{}, dir), ":0", osvc)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/outreach", http.NoBody)
	rec := httptest.NewRecorder()
	srv.outreachPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, want := range []string{
		"panel-overview",
		"最近客户往来",
		"批量群发",
		`src="/static/js/outreach.js"`,
		"</html>",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered page missing %q (template likely truncated)", want)
		}
	}
}
