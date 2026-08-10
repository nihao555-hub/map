//go:build livecustoms

package web

import (
	"context"
	"testing"
	"time"
)

func TestLiveKirchnerAllbirdsWideWindow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tr, err := lookupKirchnerCompany(ctx, "Allbirds")
	if err != nil {
		t.Fatal(err)
	}
	if tr.TotalShipments < 50 {
		t.Fatalf("expected multi-year shipments, got %+v", tr)
	}
	t.Logf("Allbirds shipments=%d suppliers=%d hs=%d range=%s-%s", tr.TotalShipments, len(tr.TopSuppliers), len(tr.TopHSCodes), tr.DateStart, tr.DateEnd)
}

func TestLiveKirchnerStarbucksBrand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tr, err := lookupKirchnerCompany(ctx, "Starbucks Coffee Company")
	if err != nil {
		t.Fatal(err)
	}
	if tr.TotalShipments < 50 {
		t.Fatalf("expected brand-level shipments, got %+v", tr)
	}
	t.Logf("Starbucks shipments=%d name=%s suppliers=%d", tr.TotalShipments, tr.Name, len(tr.TopSuppliers))
}
