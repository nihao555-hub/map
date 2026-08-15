package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchCustomsBuyersFromLeadFinder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/lead-finder" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("keywords") == "" {
			t.Errorf("missing keywords")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"year": 2025,
			"importers": []map[string]any{
				{
					"name":               "ACME TOOLS INC",
					"total_shipments":    12,
					"matching_shipments": 8,
					"focus_pct":          67,
					"profile_url":        "/importer-profile?name=ACME",
					"api_profile_url":    "https://www.kirchnerdata.com/api/company-profile/ACME/2025/2025",
				},
			},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), CustomsBaseURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{
		Keyword: "power tools", Kind: KindCustoms, Role: RoleBuyer, Year: 2025,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Name != "ACME TOOLS INC" {
		t.Fatalf("hits=%+v", res.Hits)
	}
	if res.Hits[0].Role != RoleBuyer || res.Hits[0].Country != "US" {
		t.Fatalf("hit %+v", res.Hits[0])
	}
	if res.Hits[0].Extra["matching"] != "8" {
		t.Fatalf("extra %+v", res.Hits[0].Extra)
	}
	if !strings.Contains(res.Note, "美国海关") {
		t.Fatalf("note=%s", res.Note)
	}
}

func TestSearchCustomsSellersFromProfiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"year": 2025,
			"importers": []map[string]any{
				{"name": "US BUYER LLC", "total_shipments": 3, "matching_shipments": 3, "focus_pct": 100, "profile_url": "/x"},
			},
			"profiles": []map[string]any{
				{
					"name": "US BUYER LLC",
					"top_suppliers": []map[string]any{
						{"name": "BANGKOK POWER CO", "country": "Thailand", "count": 9},
						{"name": "SHENZHEN FACTORY", "country": "China", "count": 4},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), CustomsBaseURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{
		Keyword: "电动工具", Kind: KindCustoms, Role: RoleSeller, Country: "TH", Year: 2025,
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

func TestSearchCustomsBuyerNonUSIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"year":      2025,
			"importers": []map[string]any{{"name": "US BUYER", "total_shipments": 1, "matching_shipments": 1, "focus_pct": 100, "profile_url": "/x"}},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), CustomsBaseURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{
		Keyword: "furniture", Kind: KindCustoms, Role: RoleBuyer, Country: "TH", Year: 2025,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("hits=%+v", res.Hits)
	}
	if !strings.Contains(res.Note, "美国海关") {
		t.Fatalf("note=%s", res.Note)
	}
}

func TestLooksLikeHS(t *testing.T) {
	if !looksLikeHS("8467") || !looksLikeHS("9403.20") || looksLikeHS("LED灯") {
		t.Fatal("hs detection")
	}
}

func TestLookupCustomsProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/company-profile" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":             "ACME TOOLS INC",
			"country":          "United States",
			"total_shipments":  12,
			"unique_suppliers": 3,
			"from_year":        2025,
			"to_year":          2025,
			"profile_url":      "/importer-profile?name=ACME",
			"top_suppliers":    []map[string]any{{"name": "FACTORY A", "country": "China", "count": 5}},
			"latest_shipments": []map[string]any{{"date": "2025-03-01", "shipper": "FACTORY A", "consignee": "ACME TOOLS INC", "product": "drills"}},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), CustomsBaseURL: srv.URL}
	prof, err := c.LookupCustomsProfile(context.Background(), "ACME TOOLS INC", 2025)
	if err != nil {
		t.Fatal(err)
	}
	if prof.Name != "ACME TOOLS INC" || prof.TotalShipments != 12 || len(prof.Suppliers) != 1 {
		t.Fatalf("%+v", prof)
	}
}

func TestSearchCustomsSkipsWhenUnconfigured(t *testing.T) {
	c := &Client{DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "coffee", Kind: KindCustoms})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("hits=%+v", res.Hits)
	}
}
