package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchPeopleFromTikTokAPISidecar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}

		if r.URL.Path != "/search/users" {
			http.NotFound(w, r)
			return
		}

		_ = json.NewEncoder(w).Encode(sidecarResponse{
			Source: "tiktok-api",
			Users: []sidecarUser{{
				Platform:  "tiktok",
				UniqueID:  "powertools_id",
				Nickname:  "Power Tools ID",
				Signature: "Importer in Jakarta",
			}},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), TikTokURL: srv.URL, F2URL: "http://127.0.0.1:1", DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "power tools", Kind: KindPeople, Platforms: []string{PlatformTikTok}})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Hits) != 1 {
		t.Fatalf("hits=%+v warnings=%v", res.Hits, res.Warnings)
	}

	if res.Hits[0].Handle != "powertools_id" || res.Hits[0].Platform != PlatformTikTok {
		t.Fatalf("hit %+v", res.Hits[0])
	}

	if !strings.Contains(res.Note, "不会代发") {
		t.Fatalf("note=%s", res.Note)
	}
}

func TestSearchPeopleFromF2DouyinProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}

		_ = json.NewEncoder(w).Encode(sidecarResponse{
			Source: "f2-douyin",
			Users: []sidecarUser{{
				Platform:    "douyin",
				SecUID:      "MS4wLjABAAAAtest",
				Nickname:    "某工厂",
				Signature:   "电动工具",
				HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAtest",
			}},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), TikTokURL: "http://127.0.0.1:1", F2URL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{
		Keyword:   "https://www.douyin.com/user/MS4wLjABAAAAtest",
		Kind:      KindPeople,
		Platforms: []string{PlatformDouyin},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Hits) != 1 || res.Hits[0].Platform != PlatformDouyin {
		t.Fatalf("hits=%+v", res.Hits)
	}
}

func TestSearchPeoplePublicWebSearch(t *testing.T) {
	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(ddgDouyinHTML)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
		TikTokURL: "",
		F2URL:     "",
	}

	res, err := c.Search(context.Background(), Query{
		Keyword:   "电动工具",
		Kind:      KindPeople,
		Platforms: []string{PlatformDouyin, PlatformTikTok},
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Hits) < 2 {
		t.Fatalf("hits=%+v warnings=%v", res.Hits, res.Warnings)
	}

	var sawTK, sawDY bool
	for _, h := range res.Hits {
		if h.Platform == PlatformTikTok && h.Handle == "boschpowertools" {
			sawTK = true
		}

		if h.Platform == PlatformDouyin && strings.Contains(h.HomepageURL, "douyin.com/user/") {
			sawDY = true
		}

		if h.MessageURL == "" || !strings.Contains(h.MessageHint, "不会代发") {
			t.Fatalf("incomplete hit %+v", h)
		}
	}

	if !sawTK || !sawDY {
		t.Fatalf("missing profiles tk=%v dy=%v hits=%+v", sawTK, sawDY, res.Hits)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSearchRequiresKeyword(t *testing.T) {
	if _, err := (&Client{}).Search(context.Background(), Query{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestExhibitionDoesNotSelfCrawl(t *testing.T) {
	res, err := (&Client{}).Search(context.Background(), Query{Keyword: "Canton Fair", Kind: KindExhibition})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Hits) != 0 {
		t.Fatalf("expected no self-built hits, got %+v", res.Hits)
	}

	if len(res.Warnings) == 0 || !strings.Contains(strings.Join(res.Warnings, " "), "没有高 star") {
		t.Fatalf("warnings=%v", res.Warnings)
	}
}

func TestCustomsDoesNotSelfCrawl(t *testing.T) {
	res, err := (&Client{}).Search(context.Background(), Query{Keyword: "Allbirds", Kind: KindCustoms})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Hits) != 0 {
		t.Fatalf("expected no self-built hits, got %+v", res.Hits)
	}
}

func TestParseSidecarUsersError(t *testing.T) {
	_, _, err := parseSidecarUsers([]byte(`{"error":"down","users":[]}`), PlatformTikTok, "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMergeHitsUnlimitedWhenLimitZero(t *testing.T) {
	items := make([]Hit, 0, 35)
	for i := 0; i < 35; i++ {
		items = append(items, Hit{ID: "id-" + strings.Repeat("a", i+1), Name: "n", Score: 1})
	}
	out := mergeHits(items, "", 0)
	if len(out) != 35 {
		t.Fatalf("got %d want 35", len(out))
	}
}
