package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchPeopleFromTikTokAPISidecar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	c := &Client{HTTP: srv.Client(), TikTokURL: srv.URL, F2URL: "http://127.0.0.1:1"}
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

	c := &Client{HTTP: srv.Client(), TikTokURL: "http://127.0.0.1:1", F2URL: srv.URL}
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
