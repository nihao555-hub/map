package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchCustomsBuyersFromImportYeti(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("q") == "" {
			t.Errorf("missing q")
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"name":               "NIKE INC",
				"type":               "company",
				"address":            "Beaverton, Or, Us",
				"countryCode":        "US",
				"totalShipments":     18420,
				"mostRecentShipment": "11/03/2026",
				"topSuppliers":       []string{"NIKE VIETNAM", "YUE YUEN"},
				"trademarks":         []string{"Nike"},
				"detailUrl":          "/company/nike-inc",
			},
			{
				"name":               "YUE YUEN INDUSTRIAL",
				"type":               "supplier",
				"address":            "Dongguan, Cn",
				"countryCode":        "CN",
				"totalShipments":     900,
				"mostRecentShipment": "02/02/2026",
				"detailUrl":          "/supplier/yue-yuen-industrial",
			},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ImportYetiURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{
		Keyword: "shoes", Kind: KindCustoms, Role: RoleBuyer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Name != "NIKE INC" {
		t.Fatalf("hits=%+v", res.Hits)
	}
	if res.Hits[0].Role != RoleBuyer || res.Hits[0].Country != "US" {
		t.Fatalf("hit %+v", res.Hits[0])
	}
	if res.Hits[0].Extra["via"] != "importyeti" || res.Hits[0].Extra["shipments"] != "18420" {
		t.Fatalf("extra %+v", res.Hits[0].Extra)
	}
	if res.Hits[0].Extra["last_date"] != "2026-03-11" {
		t.Fatalf("date extra %+v", res.Hits[0].Extra)
	}
	if !strings.Contains(res.Hits[0].HomepageURL, "/company/nike-inc") {
		t.Fatalf("home=%s", res.Hits[0].HomepageURL)
	}
	if !strings.Contains(strings.Join(res.Sources, ","), "importyeti") {
		t.Fatalf("sources=%v", res.Sources)
	}
}

func TestSearchCustomsSellersFromImportYeti(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"name": "US BUYER LLC", "type": "company", "countryCode": "US", "totalShipments": 12, "url": "/company/us-buyer-llc"},
				{"name": "BANGKOK POWER CO", "type": "supplier", "countryCode": "TH", "country": "Thailand", "total_shipments": 9, "most_recent_shipment": "01/04/2026", "url": "/supplier/bangkok-power-co"},
				{"name": "SHENZHEN FACTORY", "type": "supplier", "countryCode": "CN", "totalShipments": 4, "url": "/supplier/shenzhen-factory"},
			},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ImportYetiURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{
		Keyword: "电动工具", Kind: KindCustoms, Role: RoleSeller, Country: "TH",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Name != "BANGKOK POWER CO" {
		t.Fatalf("want Thai supplier only, got %+v", res.Hits)
	}
	if res.Hits[0].Role != RoleSeller || res.Hits[0].Country != "TH" {
		t.Fatalf("hit %+v", res.Hits[0])
	}
}

func TestLookupCustomsProfileFromImportYeti(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"name":               "NIKE INC",
				"type":               "company",
				"address":            "Beaverton, Or, Us",
				"totalShipments":     10,
				"mostRecentShipment": "11/03/2026",
				"topSuppliers":       []string{"NIKE VIETNAM"},
				"detailUrl":          "/company/nike-inc",
			},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ImportYetiURL: srv.URL, DisablePublic: true}
	prof, err := c.LookupCustomsProfileAt(context.Background(), "NIKE INC", srv.URL+"/company/nike-inc", 2026)
	if err != nil {
		t.Fatal(err)
	}
	if prof.Name != "NIKE INC" || prof.TotalShipments != 10 {
		t.Fatalf("%+v", prof)
	}
	if prof.Country != "美国" || !strings.Contains(prof.HomepageURL, "nike-inc") {
		t.Fatalf("%+v", prof)
	}
	if len(prof.Suppliers) != 1 || prof.Suppliers[0].Name != "NIKE VIETNAM" {
		t.Fatalf("suppliers %+v", prof.Suppliers)
	}
}

func TestParseImportYetiIgnoresCloudflareHTML(t *testing.T) {
	rows := parseImportYetiSearch([]byte(`<!DOCTYPE html><title>Just a moment...</title>`), defaultImportYetiURL)
	if len(rows) != 0 {
		t.Fatalf("%+v", rows)
	}
}

func TestImportYetiPrefersLiveOverKirchner(t *testing.T) {
	iy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "LIVE IMPORTER", "type": "company", "totalShipments": 3, "url": "/company/live-importer"},
		})
	}))
	defer iy.Close()
	kirchner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "", http.StatusInternalServerError)
	}))
	defer kirchner.Close()

	c := &Client{HTTP: iy.Client(), ImportYetiURL: iy.URL, CustomsBaseURL: kirchner.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "shoes", Kind: KindCustoms, Role: RoleBuyer})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Name != "LIVE IMPORTER" {
		t.Fatalf("%+v", res.Hits)
	}
}
