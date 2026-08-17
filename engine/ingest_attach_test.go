package engine

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestListMerchantsMissingSocialsPhases(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:site", Source: "gleif", Name: "Bare Electric Co", Country: "US", Homepage: "https://bare-electric.example"},
		{ExtID: "osm:node:1", Source: "osm", Name: "AC Wholesale Electric", Country: "US", City: "Houston", Homepage: "https://www.openstreetmap.org/node/1"},
		{ExtID: "wd:Q1", Source: "wikidata", Name: "Some Company", Country: "DE", Homepage: "https://www.wikidata.org/wiki/Q1"},
		{ExtID: "gleif:reg", Source: "gleif", Name: "Registry Only Ltd", Country: "NL", Homepage: "https://search.gleif.org/#/record/x"},
		{ExtID: "gleif:has", Source: "gleif", Name: "Has Social", Country: "NL", Homepage: "https://has.example",
			Profiles: []Profile{{ExtID: "gleif:has", Platform: PlatformFacebook, URL: "https://www.facebook.com/has", Source: "wikidata-social"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := dir.MarkProbed(context.Background(), []string{"wd:Q1"}); err != nil {
		t.Fatal(err)
	}

	home, err := dir.ListMerchantsMissingSocials(context.Background(), 20, "homepage")
	if err != nil {
		t.Fatal(err)
	}
	if len(home) != 1 || home[0].ExtID != "gleif:site" {
		t.Fatalf("homepage=%+v", home)
	}

	osm, err := dir.ListMerchantsMissingSocials(context.Background(), 20, "osm")
	if err != nil {
		t.Fatal(err)
	}
	if len(osm) != 1 || osm[0].ExtID != "osm:node:1" {
		t.Fatalf("osm=%+v", osm)
	}

	other, err := dir.ListMerchantsMissingSocials(context.Background(), 20, "other")
	if err != nil {
		t.Fatal(err)
	}
	otherIDs := map[string]bool{}
	for _, row := range other {
		otherIDs[row.ExtID] = true
	}
	if !otherIDs["gleif:reg"] || !otherIDs["gleif:site"] || otherIDs["osm:node:1"] {
		t.Fatalf("other=%+v", other)
	}

	all, err := dir.ListMerchantsMissingSocials(context.Background(), 20, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, row := range all {
		got[row.ExtID] = true
	}
	if !got["gleif:site"] || !got["osm:node:1"] || !got["gleif:reg"] {
		t.Fatalf("all=%+v", all)
	}
	if got["gleif:has"] || got["wd:Q1"] {
		t.Fatalf("already social/probed leaked: %+v", all)
	}
}

func TestIngestAttachSocialsFillsAndMarksProbed(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "osm:node:99", Source: "osm", Name: "Licht Kraus", Country: "DE", City: "Berlin", Homepage: "https://www.openstreetmap.org/node/99"},
		{ExtID: "gleif:reg", Source: "gleif", Name: "AC Wholesale Electric", Country: "US", City: "Houston", Homepage: "https://search.gleif.org/#/record/x"},
		{ExtID: "gleif:has", Source: "gleif", Name: "Already Has", Country: "US", Homepage: "https://has.example",
			Profiles: []Profile{{ExtID: "gleif:has", Platform: PlatformFacebook, URL: "https://www.facebook.com/already", Source: "wikidata-social"}}},
	}); err != nil {
		t.Fatal(err)
	}

	overpass := `{"elements":[{"type":"node","id":99,"tags":{"name":"Licht Kraus","contact:facebook":"LichtKraus"}}]}`
	ddg := `
<html><body>
  <div class="result">
    <a class="result__a" href="https://www.facebook.com/acwholesaleelectric">AC Wholesale Electric</a>
  </div>
</body></html>`
	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := ddg
			if strings.Contains(req.URL.Host, "overpass") {
				body = overpass
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
		OverpassURL: "http://overpass.test/api",
	}

	st := c.ingestAttachSocials(context.Background(), dir, IngestOptions{AttachWorkers: 2})
	if st.Err != "" {
		t.Fatal(st.Err)
	}
	if st.Rows < 2 {
		t.Fatalf("attached=%d note=%s", st.Rows, st.Note)
	}

	byID, err := dir.ProfilesFor(context.Background(), []string{"osm:node:99", "gleif:reg", "gleif:has"})
	if err != nil {
		t.Fatal(err)
	}
	if !hasPlatform(byID["osm:node:99"], PlatformFacebook) {
		t.Fatalf("osm profiles=%+v", byID["osm:node:99"])
	}
	if !hasPlatform(byID["gleif:reg"], PlatformFacebook) {
		t.Fatalf("gleif profiles=%+v", byID["gleif:reg"])
	}

	left, err := dir.ListMerchantsMissingSocials(context.Background(), 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("should be probed or filled: %+v", left)
	}
}

func hasPlatform(rows []Profile, platform string) bool {
	for _, p := range rows {
		if p.Platform == platform && strings.TrimSpace(p.URL) != "" {
			return true
		}
	}
	return false
}
