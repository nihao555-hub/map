package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectorySearchByShopAndName(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "merchants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	n, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "osm:node:1", Source: "osm", Name: "Licht Kraus", Shop: "lighting", Country: "DE", City: "Berlin", Homepage: "https://licht-kraus.example", Profiles: []Profile{
			{ExtID: "osm:node:1", Platform: PlatformFacebook, URL: "https://www.facebook.com/lichtkraus", Handle: "lichtkraus", Verified: true, Source: "website"},
		}},
		{ExtID: "osm:node:2", Source: "osm", Name: "Aldi", Shop: "supermarket", Country: "DE", City: "Berlin", Homepage: "https://www.openstreetmap.org/node/2"},
		{ExtID: "gleif:001", Source: "gleif", Name: "Signify Holding B.V.", Shop: "GENERAL", Country: "NL", City: "Eindhoven", Homepage: "https://search.gleif.org/#/record/001"},
	})
	if err != nil || n != 3 {
		t.Fatalf("insert n=%d err=%v", n, err)
	}

	rows, err := dir.Search(context.Background(), "LED灯", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	var sawLight, sawAldi bool
	for _, row := range rows {
		if row.Name == "Licht Kraus" {
			sawLight = true
		}
		if row.Name == "Aldi" {
			sawAldi = true
		}
	}
	if !sawLight {
		t.Fatalf("lighting shop missing: %+v", rows)
	}
	if sawAldi {
		t.Fatalf("unrelated supermarket leaked: %+v", rows)
	}

	hits := merchantsToHits(rows)
	if len(hits) == 0 || hits[0].Name != "Licht Kraus" {
		t.Fatalf("hits=%+v", hits)
	}
	if len(hits[0].Profiles) != 1 || hits[0].Profiles[0].Platform != PlatformFacebook {
		t.Fatalf("profiles=%+v", hits[0].Profiles)
	}
}

func TestPackCompanyHitsMergesSocials(t *testing.T) {
	out := packCompanyHits([]Hit{
		{ID: "web", Name: "Licht Kraus", Platform: PlatformWebsite, HomepageURL: "https://licht.example", Extra: map[string]string{"ext_id": "osm:node:1"}},
		{ID: "fb", Name: "Licht Kraus", Platform: PlatformFacebook, HomepageURL: "https://www.facebook.com/lichtkraus", Handle: "lichtkraus", Verified: true, Extra: map[string]string{"ext_id": "osm:node:1", "via": "https://licht.example"}},
		{ID: "ig", Name: "Licht Kraus", Platform: PlatformInstagram, HomepageURL: "https://www.instagram.com/lichtkraus/", Handle: "lichtkraus", Verified: true, Extra: map[string]string{"ext_id": "osm:node:1"}},
	})
	if len(out) != 1 || len(out[0].Profiles) != 2 {
		t.Fatalf("card=%+v", out)
	}
}

func TestGLEIFWithoutSocialDroppedFromHits(t *testing.T) {
	hits := merchantsToHits([]Merchant{{
		ExtID: "gleif:1", Source: "gleif", Name: "Some Fund", Homepage: "https://search.gleif.org/#/record/1",
	}})
	if len(hits) != 0 {
		t.Fatalf("gleif-only leaked %+v", hits)
	}
}

func TestGLEIFColumnIndex(t *testing.T) {
	idx := gleifColumnIndex([]string{
		"LEI", "Entity.LegalName", "Entity.LegalName.xmllang",
		"Entity.LegalAddress.City", "Entity.LegalAddress.Country", "Entity.EntityCategory",
	})
	if idx.lei != 0 || idx.name != 1 || idx.city != 3 || idx.country != 4 || idx.category != 5 {
		t.Fatalf("idx=%+v", idx)
	}
}

func TestImportGLEIFZip(t *testing.T) {
	zipPath := writeTestGLEIFZip(t)
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	n, err := importGLEIFZip(context.Background(), dir, zipPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("rows=%d", n)
	}
	rows, err := dir.Search(context.Background(), "Signify", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].Source != "gleif" || rows[0].Country != "NL" {
		t.Fatalf("rows=%+v", rows)
	}
}

func TestIngestOSMAllShops(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotQuery = r.Form.Get("data")
		_, _ = w.Write([]byte(`{"elements":[
			{"type":"node","id":11,"tags":{"name":"Backwerk","shop":"bakery","addr:country":"DE"}},
			{"type":"node","id":12,"tags":{"name":"Licht Kraus","shop":"lighting","website":"https://licht.example","contact:facebook":"LichtKraus","contact:instagram":"lichtkraus"}}
		]}`))
	}))
	defer srv.Close()

	dbPath := filepath.Join(t.TempDir(), "m.db")
	c := &Client{HTTP: srv.Client(), OverpassURL: srv.URL}
	stats, err := c.IngestMerchants(context.Background(), IngestOptions{
		DBPath:          dbPath,
		SkipGLEIF:       true,
		Overpass:        true,
		OSMBoxes:        []ingestBox{{city: "berlin", country: "DE", south: 52.45, west: 13.25, north: 52.58, east: 13.55}},
		OSMLimitPerCity: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Source != "osm" || stats[0].Rows != 2 {
		t.Fatalf("stats=%+v", stats)
	}
	if !strings.Contains(gotQuery, `["shop"]["name"]`) || strings.Contains(gotQuery, `["shop"="lighting"]`) {
		t.Fatalf("expected all-shop query, got %s", gotQuery)
	}

	dir, err := OpenDirectory(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	n, err := dir.Count(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	profs, err := dir.ProfilesFor(context.Background(), []string{"osm:node:12"})
	if err != nil {
		t.Fatal(err)
	}
	if len(profs["osm:node:12"]) < 2 {
		t.Fatalf("osm social tags not stored: %+v", profs)
	}
}

func TestOSMTagProfiles(t *testing.T) {
	got := osmTagProfiles("osm:node:1", "Licht Kraus", map[string]string{
		"contact:facebook":  "LichtKraus",
		"contact:instagram": "https://www.instagram.com/lichtkraus/",
	})
	if len(got) != 2 {
		t.Fatalf("got=%+v", got)
	}
}

func TestSearchPeopleUsesDirectory(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dir.Close() })
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "osm:node:9", Source: "osm", Name: "Opple Showroom", Shop: "lighting", Country: "DE", Homepage: "https://opple.example"},
	}); err != nil {
		t.Fatal(err)
	}

	c := &Client{DisablePublic: true, OverpassURL: "", dir: dir}
	res, err := c.searchPeople(context.Background(), Query{Keyword: "LED灯", Kind: KindPeople, Role: RoleBuyer})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 || res.Hits[0].Name != "Opple Showroom" {
		t.Fatalf("hits=%+v sources=%v", res.Hits, res.Sources)
	}
}

func writeTestGLEIFZip(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("lei2.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("LEI,Entity.LegalName,Entity.LegalAddress.City,Entity.LegalAddress.Country,Entity.EntityCategory\n"))
	_, _ = w.Write([]byte("001GPB6A9XPE8XJICC14,Signify Holding B.V.,Eindhoven,NL,GENERAL\n"))
	_, _ = w.Write([]byte("5493001KJTIIGC8Y1R12,Acme Lighting GmbH,Berlin,DE,GENERAL\n"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "gleif.csv.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
