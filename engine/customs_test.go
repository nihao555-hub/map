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
					"name":                  "ACME TOOLS INC",
					"total_shipments":       12,
					"matching_shipments":    8,
					"focus_pct":             67,
					"match_weight_total_kg": 1200,
					"profile_url":           "/importer-profile?name=ACME",
					"api_profile_url":       "/api/company-profile/ACME/2025/2025",
				},
			},
			"profiles": []map[string]any{
				{
					"name":    "ACME TOOLS INC",
					"address": "1 Main St",
					"top_products": []map[string]any{
						{"hs_code": "846721", "count": 8},
					},
					"top_product_terms": []map[string]any{
						{"term": "POWER DRILLS", "count": 8},
					},
					"latest_shipments": []map[string]any{
						{"actual_arrival_date": "20250301", "product_desc": "DRILLS<br/>DRILLS", "shipper_name": "FACTORY A", "hs_code": "846721"},
					},
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
	if res.Hits[0].Extra["product"] != "POWER DRILLS" || res.Hits[0].Extra["hs"] != "846721" {
		t.Fatalf("product extra %+v", res.Hits[0].Extra)
	}
	if res.Hits[0].Extra["last_date"] != "2025-03-01" || res.Hits[0].Extra["weight_kg"] != "1200" {
		t.Fatalf("date/weight extra %+v", res.Hits[0].Extra)
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
			"name":              "ACME TOOLS INC",
			"country":           "United States",
			"total_shipments":   12,
			"unique_suppliers":  3,
			"from_year":         2025,
			"to_year":           2025,
			"profile_url":       "/importer-profile?name=ACME",
			"top_suppliers":     []map[string]any{{"name": "FACTORY A", "country": "China", "count": 5}},
			"top_carriers":      []map[string]any{{"name": "ONE LINE", "count": 3}},
			"top_products":      []map[string]any{{"hs_code": "846721", "count": 8}},
			"top_product_terms": []map[string]any{{"term": "POWER DRILLS", "count": 8}},
			"latest_shipments": []map[string]any{{
				"actual_arrival_date": "20250301",
				"shipper_name":        "FACTORY A",
				"consignee_name":      "ACME TOOLS INC",
				"product_desc":        "drills<br/>drills",
				"vessel_name":         "ONE SINGAPORE",
			}},
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
	if prof.Country != "美国" {
		t.Fatalf("buyer country=%q", prof.Country)
	}
	if len(prof.Shipments) != 1 || prof.Shipments[0].Date != "2025-03-01" || prof.Shipments[0].Shipper != "FACTORY A" {
		t.Fatalf("shipments %+v", prof.Shipments)
	}
	if !strings.Contains(strings.ToLower(prof.Shipments[0].Product), "drill") {
		t.Fatalf("product %q", prof.Shipments[0].Product)
	}
	if len(prof.Products) < 2 || len(prof.Carriers) != 1 {
		t.Fatalf("products/carriers %+v", prof)
	}
}

func TestSearchCustomsFallsBackToCompanyProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "lead-finder") {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/company-profile" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":              "SIDEWALK DISTRIBUTION",
			"total_shipments":   5,
			"unique_suppliers":  1,
			"from_year":         2025,
			"to_year":           2025,
			"address":           "LOS ALAMITOS CA",
			"top_products":      []map[string]any{{"hs_code": "950670", "count": 6}},
			"top_product_terms": []map[string]any{{"term": "SKATEBOARD DECKS", "count": 3}},
			"latest_shipments": []map[string]any{{
				"actual_arrival_date": "20251017",
				"shipper_name":        "HUIZHOU CHOPCHOP WOODSHOP CO LTD",
				"product_desc":        "SKATEBOARD DECKS AND COMPLETES",
			}},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), CustomsBaseURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{
		Keyword: "SIDEWALK DISTRIBUTION", Kind: KindCustoms, Role: RoleBuyer, Year: 2025,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Name != "SIDEWALK DISTRIBUTION" {
		t.Fatalf("hits=%+v", res.Hits)
	}
	if res.Hits[0].Extra["hs"] != "950670" || res.Hits[0].Extra["product"] != "SKATEBOARD DECKS" {
		t.Fatalf("extra %+v", res.Hits[0].Extra)
	}
	if res.Hits[0].Extra["last_date"] != "2025-10-17" {
		t.Fatalf("last_date %+v", res.Hits[0].Extra)
	}
}

func TestSearchCustomsDoesNotTreatProductAsCompany(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "lead-finder") {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":            "shoes",
			"total_shipments": 2154,
			"from_year":       2024,
			"to_year":         2024,
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), CustomsBaseURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "shoes", Kind: KindCustoms, Role: RoleBuyer, Year: 2024})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("product word should not become a company row, got %+v", res.Hits)
	}
}

func TestLooksLikeCompanyName(t *testing.T) {
	if !looksLikeCompanyName("SIDEWALK DISTRIBUTION") || !looksLikeCompanyName("ACME TOOLS INC") {
		t.Fatal("company names")
	}
	if looksLikeCompanyName("shoes") || looksLikeCompanyName("coffee") || looksLikeCompanyName("furniture") {
		t.Fatal("product words")
	}
}

func TestPickHS4PrefersMatchingChapter(t *testing.T) {
	hs := pickHS4("shoes", []usitcRow{
		{Htsno: "4417.00", Description: "boot or shoe lasts of wood"},
		{Htsno: "9902.00", Description: "Footwear with outer soles and uppers of rubber"},
		{Htsno: "6403.91", Description: "Tennis shoes, basketball shoes"},
		{Htsno: "6403.19", Description: "Golf shoes"},
		{Htsno: "6402.99", Description: "Tennis shoes"},
	})
	if hs != "6403" && hs != "6402" {
		t.Fatalf("hs=%s", hs)
	}
}

func TestSearchCustomsComtradeIsNoteNotCompany(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/lead-finder", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "", http.StatusInternalServerError)
	})
	mux.HandleFunc("/C/A/HS", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cmdCode") == "" {
			t.Errorf("missing cmdCode")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"count": 2,
			"data": []map[string]any{
				{"partnerCode": 156, "partnerISO": "CHN", "partnerDesc": "China", "cmdCode": "6403", "cmdDesc": "Footwear", "period": "2024", "primaryValue": 9000000000.0},
				{"partnerCode": 704, "partnerISO": "VNM", "partnerDesc": "Viet Nam", "cmdCode": "6403", "cmdDesc": "Footwear", "period": "2024", "primaryValue": 1000000000.0},
			},
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "keyword=") {
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"htsno": "6403.91", "description": "Tennis shoes"},
			})
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{
		HTTP: srv.Client(), CustomsBaseURL: srv.URL, ComtradeURL: srv.URL,
		USITCURL: srv.URL, DisablePublic: true,
	}
	res, err := c.Search(context.Background(), Query{
		Keyword: "shoes", Kind: KindCustoms, Role: RoleSeller, Year: 2024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("Comtrade must not become company rows, got %+v", res.Hits)
	}
	if !strings.Contains(res.Note, "Comtrade") {
		t.Fatalf("note=%s", res.Note)
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

func TestSearchCustomsCompanyNameFromProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/lead-finder" {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		if r.URL.Path == "/api/company-profile" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":             "FOOT LOCKER INC",
				"total_shipments":  48,
				"unique_suppliers": 12,
				"address":          "330 W 34TH ST",
				"top_suppliers": []map[string]any{
					{"name": "FACTORY A", "country": "Vietnam", "count": 9},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), CustomsBaseURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{
		Keyword: "FOOT LOCKER INC", Kind: KindCustoms, Role: RoleBuyer, Year: 2025,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Name != "FOOT LOCKER INC" {
		t.Fatalf("hits=%+v", res.Hits)
	}
	if res.Hits[0].Extra["shipments"] != "48" {
		t.Fatalf("extra %+v", res.Hits[0].Extra)
	}

	sellers, err := c.Search(context.Background(), Query{
		Keyword: "FOOT LOCKER INC", Kind: KindCustoms, Role: RoleSeller, Year: 2025,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sellers.Hits) == 0 || sellers.Hits[0].Name != "FACTORY A" {
		t.Fatalf("sellers=%+v", sellers.Hits)
	}
}

func TestMergeCustomsHitsDropsZeroWhenRealShipmentsExist(t *testing.T) {
	out := mergeCustomsHits([]Hit{
		{Name: "Footscientific", Extra: map[string]string{"shipments": "0"}, Score: 90},
		{Name: "FOOT LOCKER INC", Extra: map[string]string{"shipments": "48"}, Score: 80},
		{Name: "WAL-MART STORES INC", Extra: map[string]string{"shipments": "100"}, Score: 70},
	}, 10)
	if len(out) != 2 || out[0].Name != "WAL-MART STORES INC" || out[1].Name != "FOOT LOCKER INC" {
		t.Fatalf("%+v", out)
	}
}

func TestMergeCustomsHitsDedupesCommaInc(t *testing.T) {
	out := mergeCustomsHits([]Hit{
		{Name: "FOOT LOCKER, INC", Extra: map[string]string{"shipments": "48"}},
		{Name: "FOOT LOCKER INC", Extra: map[string]string{"shipments": "48"}},
	}, 10)
	if len(out) != 1 {
		t.Fatalf("%+v", out)
	}
}

func TestCompanyTokensMatchFootLocker(t *testing.T) {
	if !companyTokensMatch("FOOT LOCKER INC", "Foot Locker Inc") {
		t.Fatal("should match foot locker")
	}
	if companyTokensMatch("FOOT LOCKER INC", "Footscientific") {
		t.Fatal("should not match footsientific")
	}
}
