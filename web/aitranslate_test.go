//nolint:testpackage // tests unexported AI helpers
package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAITranslateKeyword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("auth = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "kopi"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	t.Setenv("GRSAI_API_KEY", "test-key")
	t.Setenv("GRSAI_API_HOST", srv.URL)
	t.Setenv("GRSAI_MODEL", "gemini-3.1-flash-lite")

	if !AITranslateEnabled() {
		t.Fatal("expected AI enabled")
	}

	out, err := AITranslateKeyword(context.Background(), "咖啡馆", "Indonesia", "id")
	if err != nil {
		t.Fatalf("AITranslateKeyword: %v", err)
	}
	if out != "kopi" {
		t.Fatalf("got %q, want kopi", out)
	}
}

func TestAITranslateDisabledWithoutKey(t *testing.T) {
	t.Setenv("GRSAI_API_KEY", "")
	if AITranslateEnabled() {
		t.Fatal("expected disabled")
	}
}
