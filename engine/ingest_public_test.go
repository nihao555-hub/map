package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFoldLegalName(t *testing.T) {
	cases := map[string]string{
		"Signify Holding B.V.": "signify",
		"Acme Lighting GmbH":   "acme lighting",
		"Foo Sdn Bhd":          "foo",
		"Bar Ltd.":             "bar",
		"The Widget Company":   "widget",
		"松下电器产业株式会社":           "松下电器产业",
	}
	for in, want := range cases {
		if got := foldLegalName(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
	if key := nameCountryKey("Signify Holding B.V.", "NL"); key != "NL|signify" {
		t.Fatalf("key=%q", key)
	}
	if nameCountryKey("AB", "DE") != "" || nameCountryKey("Trading Ltd", "DE") != "" {
		t.Fatal("short/generic keys should be empty")
	}
}

func TestParseRORDumpKeepsLEIAndWebsite(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("v2-ror-data.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`[
	  {"id":"https://ror.org/01abc","names":[{"value":"Signify","types":["ror_display"]}],
	   "links":[{"type":"website","value":"https://www.signify.com"}],
	   "locations":[{"geonames_details":{"country_code":"NL"}}],
	   "external_ids":[{"type":"lei","preferred":"001GPB6A9XPE8XJICC14","all":["001GPB6A9XPE8XJICC14"]}]},
	  {"id":"https://ror.org/02xyz","names":[{"value":"No LEI","types":["ror_display"]}],
	   "links":[{"type":"website","value":"https://example.org"}]}
	]`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ror.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	orgs, err := parseRORDump(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 1 || orgs[0].LEI != "001GPB6A9XPE8XJICC14" || !strings.Contains(orgs[0].Website, "signify.com") {
		t.Fatalf("%+v", orgs)
	}
}

func TestAttachUniqueNameSocialsCopiesVerifiedHomepages(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:001GPB6A9XPE8XJICC14", Source: "gleif", Name: "Signify Holding B.V.", Country: "NL",
			Homepage: "https://search.gleif.org/#/record/001GPB6A9XPE8XJICC14"},
		{ExtID: "gleif:dup1", Source: "gleif", Name: "Common Store Ltd", Country: "DE", Homepage: "https://search.gleif.org/#/record/dup1"},
		{ExtID: "gleif:dup2", Source: "gleif", Name: "Common Store GmbH", Country: "DE", Homepage: "https://search.gleif.org/#/record/dup2"},
		{ExtID: "osm:node:1", Source: "osm", Name: "Signify", Country: "NL", Homepage: "https://www.signify.com",
			Profiles: []Profile{{ExtID: "osm:node:1", Platform: PlatformFacebook, URL: "https://www.facebook.com/signify", Handle: "signify", Verified: true, Source: "website"}}},
		{ExtID: "osm:node:2", Source: "osm", Name: "Common Store", Country: "DE", Homepage: "https://common.example"},
	}); err != nil {
		t.Fatal(err)
	}

	st := dir.attachUniqueNameSocials(context.Background())
	if st.Err != "" || st.Rows != 1 {
		t.Fatalf("stats=%+v", st)
	}
	profs, err := dir.ProfilesFor(context.Background(), []string{"gleif:001GPB6A9XPE8XJICC14", "gleif:dup1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(profs["gleif:001GPB6A9XPE8XJICC14"]) == 0 {
		t.Fatalf("signify not copied: %+v", profs)
	}
	if len(profs["gleif:dup1"]) != 0 {
		t.Fatalf("ambiguous name leaked: %+v", profs)
	}
}

func TestWikidataLEIWebsiteSPARQLChunks(t *testing.T) {
	q := wikidataLEIWebsiteSPARQL("54")
	if !strings.Contains(q, `STRSTARTS(STR(?lei), "54")`) || !strings.Contains(q, "P856") {
		t.Fatalf("%s", q)
	}
	if got := leiStartPrefixes(1); len(got) != 36 || got[0] != "0" || got[10] != "A" {
		t.Fatalf("%v", got)
	}
}

func TestIngestRORAttachesExistingGLEIF(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:001GPB6A9XPE8XJICC14", Source: "gleif", Name: "Signify Holding B.V.", Country: "NL",
			Homepage: "https://search.gleif.org/#/record/001GPB6A9XPE8XJICC14"},
	}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("ror.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`[{"id":"https://ror.org/01","names":[{"value":"Signify","types":["ror_display"]}],
		"links":[{"type":"website","value":"https://www.signify.com"}],
		"external_ids":[{"type":"lei","preferred":"001GPB6A9XPE8XJICC14"}]}]`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "ror.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	st := (*Client)(nil).ingestROR(context.Background(), dir, zipPath)
	if st.Err != "" || st.Rows != 1 {
		t.Fatalf("%+v", st)
	}
}
