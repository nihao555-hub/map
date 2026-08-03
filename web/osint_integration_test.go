package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOSINTToolsInstalled(t *testing.T) {
	h, sf := OSINTToolsAvailable()
	if !h {
		t.Fatalf("theHarvester not available; run tools/install_osint.sh")
	}
	if !sf {
		t.Fatalf("SpiderFoot not available; run tools/install_osint.sh")
	}
}

func TestBuildPlaceIntelWithOSINT(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	h, sf := OSINTToolsAvailable()
	if !h || !sf {
		t.Skip("osint tools missing")
	}
	dir := t.TempDir()
	svc := &Service{dataFolder: dir}
	// minimal CSV so GetPlaces works if needed
	_ = os.MkdirAll(dir, 0o755)
	place := Place{
		Title:   "Example Domain",
		Website: "https://example.com",
		PlaceID: "ChIJ_test_example",
		Address: "Example",
		Category: "Company",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	intel, err := svc.BuildPlaceIntel(ctx, "job-osint-test", place)
	if err != nil {
		t.Fatal(err)
	}
	if intel.Status != IntelReady {
		t.Fatalf("status=%s", intel.Status)
	}
	t.Logf("provider=%s domain=%s emails=%v tech=%v sources=%v conf=%s",
		intel.Provider, intel.Domain, intel.ExtraEmails, intel.Technologies, intel.Sources, intel.Confidence)
	path := filepath.Join(dir, "intel", "job-osint-test")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
