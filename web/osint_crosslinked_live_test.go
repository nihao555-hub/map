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

// TestLiveCrossLinkedAndMaigretIntel 联网测：CrossLinked 员工名 + 门控噪声 +（若已装）Maigret。
func TestLiveCrossLinkedAndMaigretIntel(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "gmaps-crosslinked-intel")
	_ = os.RemoveAll(dir)
	_ = os.MkdirAll(dir, 0o755)
	svc := NewService(nil, dir)

	cases := []Place{
		{
			PlaceID:  "live_pt_sempurna",
			Title:    "PT Sempurna Nilai Sukses",
			Category: "Import service",
			Address:  "Jakarta, Indonesia",
			Website:  "https://importer.co.id/",
			Phone:    "+622138250098",
			Emails:   "athangemilangperkasa@gmail.com",
		},
		{
			PlaceID:  "live_gordi",
			Title:    "Gordi Indonesia",
			Category: "Coffee",
			Address:  "Jakarta, Indonesia",
			Website:  "https://gordi.id/",
		},
	}

	st := ProbeOSINTTools()
	t.Logf("osint: maigret=%v crosslinked=%v ahu=%v ahu_proxy=%v", st.Maigret, st.CrossLinked, st.AHU, st.AHUProxyConfigured)

	var allNamed, allNoise int
	for _, place := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		intel, err := svc.BuildPlaceIntel(ctx, "live-crosslinked", place)
		cancel()
		if err != nil {
			t.Errorf("%s: %v", place.Title, err)
			continue
		}
		b, _ := json.MarshalIndent(intel, "", "  ")
		out := filepath.Join("/opt/cursor/artifacts", "intel-"+place.PlaceID+".json")
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		_ = os.WriteFile(out, b, 0o644)

		named, noise, sources := 0, 0, strings.Join(intel.Sources, ",")
		for _, d := range intel.DecisionMakers {
			if d.Name == "" {
				continue
			}
			if IsValidPersonName(d.Name) && QualifiesAsDecisionMaker(d, place.Title) {
				named++
			} else {
				noise++
				t.Logf("NOISE name=%q title=%q source=%q", d.Name, d.Title, d.Source)
			}
			t.Logf("[%s] dm name=%q title=%q li=%q profiles=%d source=%q",
				place.Title, d.Name, d.Title, d.LinkedIn, len(d.Profiles), d.Source)
		}
		allNamed += named
		allNoise += noise
		t.Logf("[%s] conf=%s makers=%d named=%d noise=%d sources=%s provider=%s note=%s",
			place.Title, intel.Confidence, len(intel.DecisionMakers), named, noise, sources, intel.Provider, intel.Note)

		if noise > 0 {
			t.Errorf("%s: noise names still present: %d", place.Title, noise)
		}
	}

	t.Logf("TOTAL named=%d noise=%d", allNamed, allNoise)
	if allNoise > 0 {
		t.Fatalf("expected near-zero noise, got %d", allNoise)
	}
}

// TestLiveCrossLinkedLookupOnly 只测本地 CrossLinked CLI（tools/CrossLinked）。
func TestLiveCrossLinkedLookupOnly(t *testing.T) {
	if !CrossLinkedAvailable() {
		t.Fatal("CrossLinked not installed under tools/CrossLinked + crosslinked-venv")
	}
	t.Logf("crosslinked bin=%s", crosslinkedBin())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	people, err := lookupCrossLinkedEmployees(ctx, "PT Sempurna Nilai Sukses", "importer.co.id")
	if err != nil {
		// 机房 IP 常被 Bing/Google 空结果；CLI 已调用即算接通，代理环境下才有名单
		t.Logf("crosslinked CLI returned err (often empty without residential proxy): %v", err)
	}
	t.Logf("crosslinked people=%d", len(people))
	for i, p := range people {
		t.Logf("person[%d]=%q title=%q li=%q valid=%v", i, p.Name, p.Title, p.LinkedIn, IsValidPersonName(p.Name))
		if p.Name != "" && !IsValidPersonName(p.Name) {
			t.Errorf("ungated name leaked: %q", p.Name)
		}
	}
}
