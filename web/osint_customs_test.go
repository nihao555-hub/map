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

func TestImportYetiSlugCandidates(t *testing.T) {
	cands := importYetiSlugCandidates("PT Deugro Indonesia", "deugro.com", "Deugro")
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	joined := strings.Join(cands, ",")
	if !strings.Contains(joined, "deugro") {
		t.Fatalf("cands=%v", cands)
	}
}
