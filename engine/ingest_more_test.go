package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestWikidataParentSocialSPARQL(t *testing.T) {
	q := wikidataParentSocialSPARQL("54")
	for _, want := range []string{"PREFIX wdt:", "P1278", "P749", "P355", `STRSTARTS(STR(?lei), "54")`} {
		if !strings.Contains(q, want) {
			t.Fatalf("missing %s in %s", want, q)
		}
	}
}

func TestMergeWikidataParentSocials(t *testing.T) {
	raw := []byte(`{"results":{"bindings":[{
		"lei":{"value":"001GPB6A9XPE8XJICC14"},
		"web":{"value":"https://www.signify.com"},
		"fb":{"value":"signify"}
	}]}}`)
	by := map[string]*Merchant{}
	n := mergeWikidataParentSocials(by, raw)
	if n < 2 || by["001GPB6A9XPE8XJICC14"] == nil {
		t.Fatalf("n=%d by=%+v", n, by)
	}
	m := by["001GPB6A9XPE8XJICC14"]
	if !strings.Contains(m.Homepage, "signify.com") {
		t.Fatalf("%+v", m)
	}
	var sawFB bool
	for _, p := range m.Profiles {
		if p.Platform == PlatformFacebook && p.Source == "wikidata-parent" {
			sawFB = true
		}
	}
	if !sawFB {
		t.Fatalf("%+v", m.Profiles)
	}
}

func TestParseSECTickersAndWebsiteJSON(t *testing.T) {
	tickers, err := parseSECTickers([]byte(`{
		"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."},
		"1":{"cik_str":0,"ticker":"X","title":""}
	}`))
	if err != nil || len(tickers) != 1 || tickers[0].CIK != 320193 {
		t.Fatalf("tickers=%+v err=%v", tickers, err)
	}
	home := secWebsiteFromJSON([]byte(`{"name":"Apple Inc.","website":"www.apple.com"}`))
	if home != "https://www.apple.com" {
		t.Fatalf("home=%q", home)
	}
}

func TestOSMContactQueryRichAddsTwitter(t *testing.T) {
	core := osmContactQuery(ingestBox{city: "sea", south: 1, west: 100, north: 2, east: 101})
	if !strings.Contains(core, "contact:facebook") || strings.Contains(core, "contact:twitter") {
		t.Fatalf("core=%s", core)
	}
	rich := osmContactQuery(ingestBox{city: "de", south: 47, west: 6, north: 55, east: 15, rich: true})
	if !strings.Contains(rich, "contact:twitter") || !strings.Contains(rich, "contact:tiktok") {
		t.Fatalf("rich=%s", rich)
	}
}

func TestOSMContactBoxesSplitTimedOutRegions(t *testing.T) {
	got := map[string]bool{}
	for _, box := range osmContactBoxes() {
		got[box.city] = true
		if box.city == "eu-west" || box.city == "eu-east" || box.city == "us-east" || box.city == "in-pk-bd" {
			t.Fatalf("old timeout box still present: %s", box.city)
		}
	}
	for _, want := range []string{"de-north", "fr-south", "in-west", "us-ne", "ca-east", "mx", "br-se"} {
		if !got[want] {
			t.Fatalf("missing split box %s", want)
		}
	}
	for _, box := range osmContactGapBoxes() {
		if box.city == "sea-th-my-sg" || box.city == "cn-kr-jp-tw" {
			t.Fatalf("gap list should skip already-ingested core box %s", box.city)
		}
		if !box.rich {
			t.Fatalf("gap box %s should request extra contact tags", box.city)
		}
	}
}

func TestIngestWikidataParentAttachesExistingGLEIF(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:001GPB6A9XPE8XJICC14", Source: "gleif", Name: "Signify Belgium", Country: "BE",
			Homepage: "https://search.gleif.org/#/record/001GPB6A9XPE8XJICC14"},
	}); err != nil {
		t.Fatal(err)
	}
	by := map[string]*Merchant{}
	mergeWikidataParentSocials(by, []byte(`{"results":{"bindings":[{
		"lei":{"value":"001GPB6A9XPE8XJICC14"},
		"fb":{"value":"signify"}
	}]}}`))
	rows := []Merchant{*by["001GPB6A9XPE8XJICC14"]}
	matched, profiles, err := dir.attachExisting(context.Background(), rows)
	if err != nil || matched != 1 || profiles < 1 {
		t.Fatalf("matched=%d profiles=%d err=%v", matched, profiles, err)
	}
}
