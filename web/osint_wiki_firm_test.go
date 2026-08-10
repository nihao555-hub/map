package web

import "testing"

func TestParseWikiMoneyAndEmployees(t *testing.T) {
	usd, year, ok := parseWikiMoney("US$189.8 million (2024)")
	if !ok || usd != 189_800_000 || year != "2024" {
		t.Fatalf("money got %d %s %v", usd, year, ok)
	}
	n, y, ok := parseWikiEmployees("542 (2024) 927 (2023)")
	if !ok || n != 542 || y != "2024" {
		t.Fatalf("emp got %d %s %v", n, y, ok)
	}
}

func TestLookupWikipediaFirmographicsAllbirdsLive(t *testing.T) {
	if testing.Short() {
		t.Skip("live")
	}
	fg, makers, err := lookupWikipediaFirmographics(t.Context(), "Allbirds")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if fg.RevenueUSD < 100_000_000 || fg.Employees < 100 {
		t.Fatalf("firm=%+v", fg)
	}
	t.Logf("rev=%d year=%s emp=%d makers=%d", fg.RevenueUSD, fg.RevenueYear, fg.Employees, len(makers))
}
