package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSherlockSocialsRewritesLinkedInCompany(t *testing.T) {
	raw := []byte(`{
	  "LinkedIn":{"url":"https://linkedin.com/in/{}"},
	  "TikTok":{"url":"https://www.tiktok.com/@{}"},
	  "Chess":{"url":"https://www.chess.com/member/{}"},
	  "Adult":{"url":"https://onlyfans.com/{}","isNSFW":true}
	}`)
	got := parseSherlockSocials(raw)
	by := map[string]string{}
	for _, t0 := range got {
		by[t0.Platform] = t0.URL
	}
	if !strings.Contains(by[PlatformLinkedIn], "/company/") {
		t.Fatalf("linkedin=%q", by[PlatformLinkedIn])
	}
	if by[PlatformTikTok] == "" {
		t.Fatal("tiktok missing")
	}
	if _, ok := by["chess"]; ok {
		t.Fatal("non-social leaked")
	}
	urls := sherlockURLs("signify", got)
	joined := strings.Join(urls, " ")
	if !strings.Contains(joined, "linkedin.com/company/signify") || strings.Contains(joined, "/in/signify") {
		t.Fatalf("%v", urls)
	}
}

func TestDistinctiveNameHandleSkipsShortAndGeneric(t *testing.T) {
	if got := distinctiveNameHandle("Apple Inc."); got != "" {
		t.Fatalf("apple should be too short after suffix strip, got %q", got)
	}
	if got := distinctiveNameHandle("Osdin Lighting GmbH"); got != "osdinlighting" {
		t.Fatalf("got %q", got)
	}
	if got := distinctiveNameHandle("Signify Holding B.V."); got != "signify" {
		t.Fatalf("got %q", got)
	}
	if !distinctiveNeedsLinkedIn("signify") || distinctiveNeedsLinkedIn("osdinlighting") {
		t.Fatal("length gate")
	}
}

func TestTrustedSherlockHandlesUseDomainNotLegalName(t *testing.T) {
	row := Merchant{
		Name:     "Apple Inc.",
		Homepage: "https://www.signify.com",
		Profiles: []Profile{{Platform: PlatformFacebook, URL: "https://www.facebook.com/Signify", Handle: "Signify"}},
	}
	got := strings.Join(trustedSherlockHandles(row), " ")
	if !strings.Contains(strings.ToLower(got), "signify") {
		t.Fatalf("%q", got)
	}
	if strings.EqualFold(got, "apple") || strings.Contains(strings.ToLower(got), "apple") {
		t.Fatalf("legal name leaked: %q", got)
	}
}

func TestFilterSherlockHitsRequiresConsensus(t *testing.T) {
	one := []Profile{{Platform: PlatformTelegram, URL: "https://t.me/apple", Handle: "apple"}}
	if filterSherlockHits(one, true) != nil || filterSherlockHits(one, false) != nil {
		t.Fatal("single telegram should drop")
	}
	two := []Profile{
		{Platform: PlatformFacebook, URL: "https://www.facebook.com/osdinlighting", Handle: "osdinlighting"},
		{Platform: PlatformInstagram, URL: "https://www.instagram.com/osdinlighting", Handle: "osdinlighting"},
	}
	if len(filterSherlockHits(two, false)) != 2 {
		t.Fatalf("%+v", filterSherlockHits(two, false))
	}
	if filterSherlockHits(two, true) != nil {
		t.Fatal("short handle without LinkedIn company should drop")
	}
	withLI := append(two, Profile{Platform: PlatformLinkedIn, URL: "https://www.linkedin.com/company/signify", Handle: "signify"})
	if len(filterSherlockHits(withLI, true)) != 3 {
		t.Fatalf("%+v", filterSherlockHits(withLI, true))
	}
}

func TestListDistinctiveGLEIFKeepsUniqueLongNames(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:1", Source: "gleif", Name: "Osdin Lighting GmbH", Country: "DE"},
		{ExtID: "gleif:2", Source: "gleif", Name: "Osdin Lighting Ltd", Country: "GB"},
		{ExtID: "gleif:3", Source: "gleif", Name: "Apple Inc.", Country: "US"},
		{ExtID: "gleif:4", Source: "gleif", Name: "Uniquebrandwidgets LLC", Country: "US"},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := dir.ListDistinctiveGLEIF(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, row := range rows {
		got[row.ExtID] = true
	}
	if got["gleif:1"] || got["gleif:2"] {
		t.Fatalf("ambiguous osdinlighting leaked: %+v", rows)
	}
	if got["gleif:3"] {
		t.Fatal("apple leaked")
	}
	if !got["gleif:4"] {
		t.Fatalf("unique long name missing: %+v", rows)
	}
}
