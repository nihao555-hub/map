//go:build livecustoms
package web
import (
  "context"
  "testing"
  "time"
)
func TestLiveImportYetiWalMart(t *testing.T) {
  ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
  defer cancel()
  tr, err := lookupImportYeti(ctx, "Wal Mart", "walmart.com")
  if err != nil { t.Fatal(err) }
  if tr == nil || tr.TotalShipments == 0 { t.Fatalf("%+v", tr) }
  t.Logf("iy name=%s shipments=%d suppliers=%d hs=%d source=%s url=%s", tr.Name, tr.TotalShipments, len(tr.TopSuppliers), len(tr.TopHSCodes), tr.Source, tr.ProfileURL)
}
func TestLiveKirchnerTarget(t *testing.T) {
  ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
  defer cancel()
  tr, err := lookupKirchnerCompany(ctx, "TARGET CORP")
  if err != nil { t.Fatal(err) }
  t.Logf("kirchner name=%s shipments=%d hs=%d", tr.Name, tr.TotalShipments, len(tr.TopHSCodes))
}
