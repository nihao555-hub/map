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

func TestDirectorySearchChineseSwitchgearInIndonesia(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "merchants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:id-buyer", Source: "gleif", Name: "PT Sumber Panel Listrik Trading", Shop: "GENERAL", Country: "ID", City: "Jakarta", Homepage: "https://listrik-trading.example", Profiles: []Profile{
			{ExtID: "gleif:id-buyer", Platform: PlatformFacebook, URL: "https://www.facebook.com/sumberlistrik", Handle: "sumberlistrik", Source: "website"},
		}},
		{ExtID: "osm:id-shop", Source: "osm", Name: "Toko Listrik Jaya", Shop: "electrical", Country: "ID", City: "Surabaya", Homepage: "https://toko-listrik.example", Profiles: []Profile{
			{ExtID: "osm:id-shop", Platform: PlatformInstagram, URL: "https://www.instagram.com/tokolistrikjaya", Handle: "tokolistrikjaya", Source: "osm-tag"},
		}},
		{ExtID: "osm:id-electronics", Source: "osm", Name: "Toko Listrik Cihapit", Shop: "electronics", Country: "ID", City: "Bandung", Homepage: "https://www.openstreetmap.org/node/13795250644"},
		{ExtID: "osm:id-sinar", Source: "osm", Name: "Toko Sinar Listrik", Shop: "yes", Country: "ID", City: "Makassar", Homepage: "https://www.openstreetmap.org/node/4175017663"},
		{ExtID: "osm:id-market", Source: "osm", Name: "Toko Sumber Jaya Listrik", Shop: "supermarket", Country: "ID", City: "Makassar", Homepage: "https://www.openstreetmap.org/node/4172387604"},
		{ExtID: "gleif:id-brand", Source: "gleif", Name: "PT Schneider Electric Switchgear Indonesia", Shop: "GENERAL", Country: "ID", City: "Jakarta", Homepage: "https://www.se.com"},
		{ExtID: "osm:de-light", Source: "osm", Name: "Licht Kraus", Shop: "lighting", Country: "DE", City: "Berlin", Homepage: "https://licht-kraus.example"},
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := dir.Search(context.Background(), "配电柜", "ID", 50)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, row := range rows {
		got[row.Name] = true
	}
	if !got["PT Sumber Panel Listrik Trading"] || !got["Toko Listrik Jaya"] || !got["Toko Listrik Cihapit"] || !got["Toko Sinar Listrik"] {
		t.Fatalf("indonesian electrical buyers missing: %+v", rows)
	}
	if got["Licht Kraus"] {
		t.Fatalf("german lighting shop leaked: %+v", rows)
	}
	if got["Toko Sumber Jaya Listrik"] {
		t.Fatalf("supermarket leaked into switchgear search: %+v", rows)
	}

	hits := mergeHits(merchantsToHits(rows), "配电柜", 0, RoleBuyer, "ID")
	names := map[string]bool{}
	for _, h := range hits {
		names[h.Name] = true
	}
	if !names["PT Sumber Panel Listrik Trading"] || !names["Toko Listrik Jaya"] || !names["Toko Listrik Cihapit"] {
		t.Fatalf("buyer merge dropped local merchants: %+v", hits)
	}
	if names["PT Schneider Electric Switchgear Indonesia"] {
		t.Fatalf("global brand seller leaked into buyer results: %+v", hits)
	}
}

func TestDirectorySearchSwitchgearDropsHardwareStore(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "merchants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "osm:us-ace", Source: "osm", Name: "Ace Hardware", Shop: "hardware", Country: "US", City: "Chicago", Homepage: "https://www.acehardware.com"},
		{ExtID: "osm:us-panel", Source: "osm", Name: "Midwest Switchgear Supply", Shop: "electrical", Country: "US", City: "Chicago", Homepage: "https://midwest-switchgear.example"},
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := dir.Search(context.Background(), "配电柜", "US", 50)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, row := range rows {
		got[row.Name] = true
	}
	if got["Ace Hardware"] {
		t.Fatalf("hardware store leaked into switchgear search: %+v", rows)
	}
	if !got["Midwest Switchgear Supply"] {
		t.Fatalf("electrical switchgear shop missing: %+v", rows)
	}
}

func TestResolveMerchantDBPrefersExistingDump(t *testing.T) {
	t.Setenv("ENGINE_MERCHANT_DB", "")
	if got := ResolveMerchantDB("/tmp/explicit.db"); got != "/tmp/explicit.db" {
		t.Fatalf("explicit=%q", got)
	}
	t.Setenv("ENGINE_MERCHANT_DB", "/tmp/from-env.db")
	if got := ResolveMerchantDB(""); got != "/tmp/from-env.db" {
		t.Fatalf("env=%q", got)
	}
	t.Setenv("ENGINE_MERCHANT_DB", "")
	if got := ResolveMerchantDB(""); got != DefaultMerchantDB {
		t.Fatalf("fresh ingest should write %s, got %s", DefaultMerchantDB, got)
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

func TestSortHitsByCountry(t *testing.T) {
	out := sortHitsByCountry([]Hit{
		{Name: "B", Country: "US", Score: 10},
		{Name: "A", Country: "DE", Score: 1},
		{Name: "C", Country: "DE", Score: 9},
	})
	if len(out) != 3 || out[0].Country != "DE" || out[0].Name != "C" || out[2].Country != "US" {
		t.Fatalf("%+v", out)
	}
}

func TestListToProbeSkipsCJKAndExistingSocial(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "merchants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "osm:node:10", Source: "osm", Name: "Backwerk", Shop: "bakery", Country: "DE", Homepage: "https://www.openstreetmap.org/node/10"},
		{ExtID: "osm:node:11", Source: "osm", Name: "老王灯具", Shop: "lighting", Country: "CN", Homepage: "https://www.openstreetmap.org/node/11"},
		{ExtID: "osm:node:12", Source: "osm", Name: "Licht Kraus", Shop: "lighting", Country: "DE", Homepage: "https://licht-kraus.example", Profiles: []Profile{
			{ExtID: "osm:node:12", Platform: PlatformWebsite, URL: "https://licht-kraus.example", Verified: true, Source: "website"},
		}},
		{ExtID: "osm:node:13", Source: "osm", Name: "Aldi", Shop: "supermarket", Country: "DE", Homepage: "https://www.openstreetmap.org/node/13", Profiles: []Profile{
			{ExtID: "osm:node:13", Platform: PlatformFacebook, URL: "https://www.facebook.com/aldi", Handle: "aldi", Verified: true, Source: "osm-tag"},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := dir.ListToProbe(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, row := range rows {
		got[row.Name] = true
	}
	if !got["Backwerk"] {
		t.Fatalf("map-only latin handle missing: %+v", rows)
	}
	if !got["Licht Kraus"] {
		t.Fatalf("website domain handle missing: %+v", rows)
	}
	if got["老王灯具"] {
		t.Fatalf("CJK name leaked: %+v", rows)
	}
	if got["Aldi"] {
		t.Fatalf("already has social: %+v", rows)
	}
}

func TestListUnverifiedProfiles(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "merchants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := dir.UpsertProfiles(context.Background(), []Profile{
		{ExtID: "osm:1", Platform: PlatformInstagram, URL: "https://www.instagram.com/demo/", Handle: "demo", Verified: false, Source: "osm-tag"},
		{ExtID: "osm:1", Platform: PlatformFacebook, URL: "https://www.facebook.com/demo", Handle: "demo", Verified: true, Source: "osm-tag"},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := dir.ListUnverifiedProfiles(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Platform != PlatformInstagram || rows[0].Verified {
		t.Fatalf("%+v", rows)
	}
}

func TestMergeWikidataLEIProp(t *testing.T) {
	by := map[string]*Merchant{}
	n := mergeWikidataLEIProp(by, []byte(`{"results":{"bindings":[
		{"lei":{"value":"001GPB6A9XPE8XJICC14"},"val":{"value":"https://www.signify.com"}}
	]}}`), "", PlatformWebsite)
	if n != 1 || by["001GPB6A9XPE8XJICC14"] == nil || !strings.Contains(by["001GPB6A9XPE8XJICC14"].Homepage, "signify.com") {
		t.Fatalf("n=%d by=%+v", n, by)
	}
	if len(by["001GPB6A9XPE8XJICC14"].Profiles) != 1 || by["001GPB6A9XPE8XJICC14"].Profiles[0].Platform != PlatformWebsite {
		t.Fatalf("website profile %+v", by["001GPB6A9XPE8XJICC14"].Profiles)
	}
}

func TestWikidataLEIPropSPARQLPaginated(t *testing.T) {
	q := wikidataLEIPropSPARQL("P856", 80000)
	for _, want := range []string{"PREFIX wdt:", "P1278", "P856", "LIMIT 80000", "OFFSET 80000"} {
		if !strings.Contains(q, want) {
			t.Fatalf("missing %s in %s", want, q)
		}
	}
}

func TestParseWikidataLEIMerchants(t *testing.T) {
	raw := []byte(`{"results":{"bindings":[
		{"lei":{"value":"001GPB6A9XPE8XJICC14"},
		 "itemLabel":{"value":"Signify"},
		 "website":{"value":"https://www.signify.com"},
		 "facebook":{"value":"Signify"}}
	]}}`)
	rows := parseWikidataLEIMerchants(raw, "NL")
	if len(rows) != 1 || rows[0].ExtID != "gleif:001GPB6A9XPE8XJICC14" || !strings.Contains(rows[0].Homepage, "signify.com") {
		t.Fatalf("%+v", rows)
	}
	if len(rows[0].Profiles) == 0 {
		t.Fatalf("missing facebook %+v", rows[0])
	}
}

func TestAttachExistingUpdatesGLEIF(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:001GPB6A9XPE8XJICC14", Source: "gleif", Name: "Signify Holding B.V.", Country: "NL", Homepage: "https://search.gleif.org/#/record/001GPB6A9XPE8XJICC14"},
	}); err != nil {
		t.Fatal(err)
	}
	n, p, err := dir.attachExisting(context.Background(), parseWikidataLEIMerchants([]byte(`{"results":{"bindings":[
		{"lei":{"value":"001GPB6A9XPE8XJICC14"},
		 "itemLabel":{"value":"Signify"},
		 "website":{"value":"https://www.signify.com"},
		 "linkedin":{"value":"signify"}}
	]}}`), "NL"))
	if err != nil || n != 1 || p == 0 {
		t.Fatalf("n=%d p=%d err=%v", n, p, err)
	}
	hits := merchantsToHits([]Merchant{{
		ExtID: "gleif:001GPB6A9XPE8XJICC14", Source: "gleif", Name: "Signify Holding B.V.",
		Homepage: "https://www.signify.com",
		Profiles: []Profile{{ExtID: "gleif:001GPB6A9XPE8XJICC14", Platform: PlatformLinkedIn, URL: "https://www.linkedin.com/company/signify", Verified: true}},
	}})
	if len(hits) == 0 {
		t.Fatal("gleif with social should appear in 找人")
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

func TestSEAIngestBoxesCoverASEAN(t *testing.T) {
	got := map[string]int{}
	for _, box := range SEAIngestBoxes() {
		got[box.country]++
	}
	for _, code := range []string{"TH", "VN", "MY", "ID", "SG", "PH", "KH", "LA", "MM", "BN"} {
		if got[code] == 0 {
			t.Fatalf("missing SEA country %s in %+v", code, got)
		}
	}
	if got["VN"] < 3 || got["ID"] < 5 || got["PH"] < 2 {
		t.Fatalf("SEA coverage too thin: %+v", got)
	}
}

func TestParseWikidataSEAMerchants(t *testing.T) {
	raw := []byte(`{"results":{"bindings":[
		{"item":{"value":"http://www.wikidata.org/entity/Q123"},
		 "itemLabel":{"value":"CP All"},
		 "website":{"value":"https://www.cpall.co.th"},
		 "facebook":{"value":"CPALL"},
		 "instagram":{"value":"cpall"}}
	]}}`)
	rows := parseWikidataSEAMerchants(raw, "TH")
	if len(rows) != 1 || rows[0].ExtID != "wd:Q123" || rows[0].Country != "TH" || rows[0].Source != "wikidata" {
		t.Fatalf("%+v", rows)
	}
	if len(rows[0].Profiles) < 2 {
		t.Fatalf("socials=%+v", rows[0].Profiles)
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
