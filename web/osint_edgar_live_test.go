package web

import (
	"context"
	"testing"
	"time"
)

func TestLookupEDGARFirmographicsAllbirdsLive(t *testing.T) {
	if testing.Short() {
		t.Skip("live")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fg, err := lookupEDGARFirmographics(ctx, "Allbirds")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if fg == nil || fg.RevenueUSD <= 0 {
		t.Fatalf("firm=%+v", fg)
	}
	t.Logf("legal=%s ticker=%s cik=%s rev=%d year=%s", fg.LegalName, fg.Ticker, fg.CIK, fg.RevenueUSD, fg.RevenueYear)
}
