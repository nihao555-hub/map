package web

import (
	"strings"
	"testing"
	"time"
)

func TestKirchnerYearWindow(t *testing.T) {
	from, to := kirchnerYearWindow(time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC))
	if to != 2026 || from != 2022 {
		t.Fatalf("got %d-%d want 2022-2026", from, to)
	}
	from, to = kirchnerYearWindow(time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC))
	if from != 2015 || to != 2016 {
		t.Fatalf("floor: got %d-%d want 2015-2016", from, to)
	}
}

func TestCompanyIntroSummaryStripsCustoms(t *testing.T) {
	mixed := "美国海关进口记录：Allbirds · 累计提单 488 · 区间 2022–2026 · 主要伙伴 N/A · HS 640411 · 来源 kirchner Allbirds is an American footwear brand."
	got := companyIntroSummary(mixed)
	if !strings.Contains(got, "American footwear") || strings.Contains(got, "累计提单") {
		t.Fatalf("got %q", got)
	}
	if companyIntroSummary("美国海关进口记录：X · 来源 kirchner") != "" {
		t.Fatal("customs-only should clear")
	}
	if companyIntroSummary("A normal company intro about shoes.") != "A normal company intro about shoes." {
		t.Fatal("plain intro should pass through")
	}
}

func TestApplyTradeIntelDoesNotPolluteCompanySummary(t *testing.T) {
	intel := &PlaceIntel{Title: "Allbirds", Summary: "Allbirds makes wool shoes."}
	tr := &TradeIntel{Source: "kirchner", Name: "Allbirds", TotalShipments: 10, Summary: "美国海关进口记录：Allbirds · 累计提单 10 · 来源 kirchner"}
	applyTradeIntel(intel, tr)
	if intel.Trade == nil || intel.Trade.TotalShipments != 10 {
		t.Fatal("trade missing")
	}
	if intel.Summary != "Allbirds makes wool shoes." {
		t.Fatalf("summary polluted: %q", intel.Summary)
	}
}

func TestSlugifyImportYeti(t *testing.T) {
	if got := slugifyImportYeti("PT Deugro Indonesia"); got != "deugro-indonesia" && got != "deugro" {
		// brand strip may leave deugro-indonesia
		if !strings.Contains(got, "deugro") {
			t.Fatalf("got %q", got)
		}
	}
	if got := slugifyImportYeti("Wal-Mart Inc."); got == "" {
		t.Fatal("empty slug")
	}
}

func TestMapImportYetiData(t *testing.T) {
	data := map[string]any{
		"title":           "Wal Mart",
		"website":         "walmart.com",
		"phone_number":    "14792738420",
		"address":         "Bentonville, Ar",
		"country":         "United States",
		"total_shipments": float64(100),
		"date_range":      map[string]any{"start_date": "2015-01-01", "end_date": "2026-01-01"},
		"suppliers_table": []any{
			map[string]any{"supplier_name": "Acme Factory", "country": "China", "shipments_12m": float64(12), "key": "/supplier/acme-factory"},
			map[string]any{"supplier_name": "N/A", "shipments_12m": float64(50)},
			map[string]any{"supplier_name": "Missing in source document", "shipments_12m": float64(99)},
		},
		"hs_codes": []any{
			map[string]any{"hs_code": "080521", "description": "Mandarins", "shipments_12m": float64(5)},
		},
		"recent_bols": []any{
			map[string]any{
				"date_formatted": "01/01/2026", "Shipper_Name": "Acme Factory",
				"Consignee_Name": "Wal Mart", "Product_Description": "Fruit", "HS_Code": "080521", "Country": "China",
				"Bill_of_Lading": "BOL123",
			},
		},
	}
	tr := mapImportYetiData(data, "company", "wal-mart")
	if tr.Name != "Wal Mart" || tr.TotalShipments != 100 {
		t.Fatalf("bad trade: %+v", tr)
	}
	if len(tr.TopSuppliers) != 1 || tr.TopSuppliers[0].Name != "Acme Factory" {
		t.Fatalf("suppliers: %+v", tr.TopSuppliers)
	}
	if len(tr.TopHSCodes) != 1 || tr.TopHSCodes[0].Code != "080521" {
		t.Fatalf("hs: %+v", tr.TopHSCodes)
	}
	intel := &PlaceIntel{Title: "Wal Mart", Socials: map[string]string{}}
	applyTradeIntel(intel, tr)
	if intel.Trade == nil || intel.Trade.Source != "importyeti" {
		t.Fatal("trade not applied")
	}
	if len(intel.OrgStructure) == 0 {
		t.Fatal("expected supplier org units")
	}
	if !strings.Contains(strings.Join(intel.Sources, ","), "importyeti") {
		t.Fatalf("sources=%v", intel.Sources)
	}
}

func TestExternalRecordMatchesBusinessRejectsPrefixFalsePositives(t *testing.T) {
	if ExternalRecordMatchesBusiness("Paper", "Paper Son Coffee") {
		t.Fatal("Paper must not match Paper Son Coffee")
	}
	if ExternalRecordMatchesBusiness("Raja", "Raja Kurma Indonesia Office") {
		t.Fatal("Raja must not match Raja Kurma…")
	}
	if !ExternalRecordMatchesBusiness("Allbirds", "Allbirds") {
		t.Fatal("Allbirds should match")
	}
	if !ExternalRecordMatchesBusiness("STARBUCKS", "Starbucks Coffee Company") {
		t.Fatal("Starbucks brand should match Starbucks Coffee Company")
	}
	if !ExternalRecordMatchesBusiness("ALLBIRDS INC", "Allbirds") {
		t.Fatal("ALLBIRDS INC should match Allbirds")
	}
}

func TestMapKirchnerLatestShipmentsAndSkipNA(t *testing.T) {
	parsed := map[string]any{
		"name":             "ALLBIRDS",
		"address":          "ATTN MARIA TEL +1 650 273-0151 FEIN 47",
		"country":          "VN, VIET NAM",
		"from_year":        float64(2022),
		"to_year":          float64(2025),
		"total_shipments":  float64(100),
		"unique_suppliers": float64(10),
		"unique_products":  float64(5),
		"last_year_total":  float64(20),
		"newest_record_month": "2025-12",
		"profile_url":      "/importer-profile?name=ALLBIRDS&from_year=2022&to_year=2025",
		"top_suppliers": []any{
			map[string]any{"name": "N/A", "count": float64(50)},
			map[string]any{"name": "ATHENA VIET NAM", "count": float64(40)},
		},
		"top_products": []any{
			map[string]any{"hs_code": "640411", "count": float64(30)},
		},
		"top_carriers": []any{
			map[string]any{"name": "FLXT", "count": float64(90)},
		},
		"top_origin_countries": []any{
			map[string]any{"country": "VN, VIET NAM", "count": float64(80)},
		},
		"monthly_shipments": []any{
			map[string]any{"month": "2025-11", "count": float64(3)},
			map[string]any{"month": "2025-12", "count": float64(4)},
		},
		"latest_shipments": []any{
			map[string]any{
				"bill_of_lading":      "FLXT1",
				"product_desc":        "SHOES<br/>HS",
				"consignee_name":      "ALLBIRDS INC",
				"shipper_name":        "N/A",
				"vessel_name":         "NESTOS",
				"actual_arrival_date": "20251224",
				"carrier_sasc_code":   "FLXT",
			},
		},
	}
	// Reuse fetchKirchnerProfile mapping via a tiny helper path: call map functions through POST stub.
	// Directly exercise helpers used by fetchKirchnerProfile.
	suppliers := mapKirchnerPartners(parsed["top_suppliers"], 8)
	if len(suppliers) != 1 || suppliers[0].Name != "ATHENA VIET NAM" {
		t.Fatalf("suppliers=%+v", suppliers)
	}
	bols := mapKirchnerLatestShipments(parsed["latest_shipments"], 5)
	if len(bols) != 1 || bols[0].BillOfLading != "FLXT1" || bols[0].Date != "2025-12-24" {
		t.Fatalf("bols=%+v", bols)
	}
	if bols[0].Shipper != "" {
		t.Fatalf("N/A shipper should clear, got %q", bols[0].Shipper)
	}
	phone := extractPhoneFromCustomsText(asString(parsed["address"]))
	if phone == "" || !strings.Contains(phone, "650") {
		t.Fatalf("phone=%q", phone)
	}
	if absoluteKirchnerURL(asString(parsed["profile_url"])) != "https://www.kirchnerdata.com/importer-profile?name=ALLBIRDS&from_year=2022&to_year=2025" {
		t.Fatalf("profile url bad")
	}
}
