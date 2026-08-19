package engine

import (
	"strings"
	"testing"
)

func TestParseUKCompanyFile(t *testing.T) {
	t.Parallel()
	raw := []byte("202606\t1\tACME LTD\t'UNIT 1'\tHIGH ST\tLONDON\t\t\tSW1A 1AA\t94036010\t85044090\n")
	co, pr := parseUKCompanyFile(raw, "importer")
	if len(co) != 1 || co[0].Name != "ACME LTD" || co[0].Country != "GB" {
		t.Fatalf("company=%+v", co)
	}
	if len(pr) != 2 || pr[0].HSCode != "94036010" {
		t.Fatalf("products=%+v", pr)
	}
}

func TestParseUKBDSLine(t *testing.T) {
	t.Parallel()
	line := "20260612026080101210000150001FR007DOV001FR600000001023860000000035000000000000070imp0"
	if len(line) != 85 {
		t.Fatalf("fixture len=%d", len(line))
	}
	rows := parseUKBDSLines([]byte(line+"\n"), "import")
	if len(rows) != 1 {
		t.Fatalf("rows=%+v", rows)
	}
	got := rows[0]
	if got.HSCode != "01012100" || got.PartnerCountry != "FR" || got.StatValue != 102386 {
		t.Fatalf("%+v", got)
	}
}

func TestCustomsCompanyExtIDStable(t *testing.T) {
	t.Parallel()
	a := customsCompanyExtID("GB", "importer", "202606", "ACME LTD", "SW1A 1AA")
	b := customsCompanyExtID("GB", "importer", "202606", "ACME LTD", "SW1A 1AA")
	if a != b || !strings.HasPrefix(a, "gb:importer:202606:") {
		t.Fatalf("%q %q", a, b)
	}
}

func TestCustomsStoreUpsert(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenCustomsStore(dir + "/customs.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	ext := customsCompanyExtID("GB", "importer", "202606", "ACME LTD", "SW1A 1AA")
	n, err := store.UpsertCompanies(ctx, []CustomsCompany{{
		ExtID: ext, Country: "GB", Name: "ACME LTD", Role: "importer", Source: "test", Period: "202606",
	}})
	if err != nil || n != 1 {
		t.Fatalf("companies n=%d err=%v", n, err)
	}
	np, err := store.UpsertCompanyProducts(ctx, []CustomsCompanyProduct{{
		ExtID: ext, HSCode: "94036010", Source: "test", Period: "202606", Country: "GB",
	}})
	if err != nil || np != 1 {
		t.Fatalf("products n=%d err=%v", np, err)
	}
	counts, err := store.Counts(ctx)
	if err != nil || counts["customs_companies"] != 1 || counts["customs_company_products"] != 1 {
		t.Fatalf("counts=%v err=%v", counts, err)
	}
}
