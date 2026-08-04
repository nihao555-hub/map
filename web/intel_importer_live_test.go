//go:build liveintel

package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveImporterIntelQuality(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "gmaps-importer-intel")
	_ = os.RemoveAll(dir)
	_ = os.MkdirAll(dir, 0o755)
	svc := NewService(nil, dir)
	place := Place{
		PlaceID:  "test_importer_co_id",
		Title:    "PT Sempurna Nilai Sukses",
		Category: "Import service",
		Address:  "Jakarta, Indonesia",
		Website:  "https://importer.co.id/",
		Emails:   "athangemilangperkasa@gmail.com",
		Phone:    "+622138250098",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	intel, err := svc.BuildPlaceIntel(ctx, "job-importer", place)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(intel, "", "  ")
	out := "/opt/cursor/artifacts/importer-intel.json"
	_ = os.MkdirAll(filepath.Dir(out), 0o755)
	_ = os.WriteFile(out, b, 0o644)
	t.Logf("confidence=%s note=%s makers=%d emails=%d li=%v", intel.Confidence, intel.Note, len(intel.DecisionMakers), len(intel.ExtraEmails), intel.Socials["linkedin"])
	for i, d := range intel.DecisionMakers {
		t.Logf("dm[%d] name=%q title=%q email=%q phone=%q wa=%q li=%q avatar=%v", i, d.Name, d.Title, d.Email, d.Phone, d.WhatsApp, d.LinkedIn, d.Avatar != "")
	}
	if strings.Contains(intel.Note, "SpiderFoot") || strings.Contains(intel.Note, "证据驱动") {
		t.Fatalf("noise note: %s", intel.Note)
	}
	for _, d := range intel.DecisionMakers {
		if strings.Contains(d.Name, "核实") || d.Title == "Management / Founder mention" {
			t.Fatalf("junk maker: %+v", d)
		}
		if d.Email != "" && d.Name != "" {
			local := d.Email
			if i := strings.Index(d.Email, "@"); i > 0 {
				local = d.Email[:i]
			}
			if strings.EqualFold(d.Name, local) {
				t.Fatalf("email local as name: %+v", d)
			}
		}
	}
	if len(intel.DecisionMakers) == 0 && len(intel.ExtraEmails) == 0 && len(intel.Phones) == 0 {
		t.Fatal("no contact channels at all")
	}
}
