package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type memInviteStore struct {
	mu       sync.Mutex
	codes    map[string]bool // code -> used
	sessions map[string]time.Time
}

func newMemInviteStore(codes ...string) *memInviteStore {
	m := &memInviteStore{
		codes:    make(map[string]bool),
		sessions: make(map[string]time.Time),
	}
	for _, c := range codes {
		m.codes[NormalizeInviteCode(c)] = false
	}
	return m
}

func (m *memInviteStore) EnsureSeed(context.Context, int) ([]string, error) { return nil, nil }

func (m *memInviteStore) Redeem(_ context.Context, code string) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	code = NormalizeInviteCode(code)
	used, ok := m.codes[code]
	if !ok || used {
		return "", time.Time{}, ErrInvalidInvite
	}
	m.codes[code] = true
	token := "tok-" + code
	exp := time.Now().UTC().Add(InviteSessionTTL)
	m.sessions[token] = exp
	return token, exp, nil
}

func (m *memInviteStore) ValidSession(_ context.Context, token string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.sessions[token]
	return ok && time.Now().UTC().Before(exp), nil
}

func (m *memInviteStore) Stats(context.Context) (int, int, int, error)  { return 0, 0, 0, nil }
func (m *memInviteStore) ListAll(context.Context) ([]InviteCode, error) { return nil, nil }
func (m *memInviteStore) ExportFile(context.Context, string) error      { return nil }

func TestInviteGateRedirectAndRedeem(t *testing.T) {
	code := "GMS-ABCD-EFGH"
	store := newMemInviteStore(code)
	svc := NewService(nil, t.TempDir())
	srv, err := New(svc, ":0", WithInvite(store, true))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/invite") {
		t.Fatalf("location=%q", loc)
	}

	req = httptest.NewRequest(http.MethodGet, "/invite", nil)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invite page: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "邀请码") {
		t.Fatalf("invite body missing 邀请码")
	}

	form := strings.NewReader("code=" + code + "&next=/")
	req = httptest.NewRequest(http.MethodPost, "/invite", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("redeem want 302, got %d body=%s", rec.Code, rec.Body.String())
	}
	var session string
	for _, c := range rec.Result().Cookies() {
		if c.Name == InviteCookieName {
			session = c.Value
		}
	}
	if session == "" {
		t.Fatal("missing session cookie")
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: InviteCookieName, Value: session})
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authed / want 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	rec = httptest.NewRecorder()
	srv.srv.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("api want 401, got %d", rec.Code)
	}
}

func TestNormalizeInviteCode(t *testing.T) {
	if got := NormalizeInviteCode("GMSAB12CD34"); got != "GMS-AB12-CD34" {
		t.Fatalf("compact: %q", got)
	}
	if got := NormalizeInviteCode("gms-ab12-cd34"); got != "GMS-AB12-CD34" {
		t.Fatalf("dashed: %q", got)
	}
	if got := NormalizeInviteCode("  gms ab12 cd34  "); got != "GMS-AB12-CD34" {
		t.Fatalf("spaced: %q", got)
	}
}
