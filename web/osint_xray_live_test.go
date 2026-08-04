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

func TestLiveXRayAndAvatarUnstick(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	people, co, err := lookupLinkedInPeople(ctx, "PT Sempurna Nilai Sukses", "importer.co.id")
	t.Logf("xray err=%v company=%s people=%d", err, co, len(people))
	for i, p := range people {
		t.Logf("person[%d]=%q title=%q li=%s", i, p.Name, p.Title, p.LinkedIn)
	}
	if len(people) == 0 {
		t.Skip("Brave/Clash search egress temporarily blocked (429); retry later")
	}
	enrichDecisionMakerAvatars(people)
	av := 0
	for _, p := range people {
		if isRealAvatarURL(p.Avatar) {
			av++
		}
	}
	t.Logf("avatars=%d/%d", av, len(people))
	if av == 0 {
		t.Fatal("expected at least one real avatar via unavatar after /in/ URLs")
	}
}

func TestLiveImporterIntelWithAvatars(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nil, dir)
	place := Place{
		PlaceID:  "unstick_importer",
		Title:    "PT Sempurna Nilai Sukses",
		Category: "Import service",
		Address:  "Jakarta, Indonesia",
		Website:  "https://importer.co.id/",
		Phone:    "+622138250098",
		Emails:   "athangemilangperkasa@gmail.com",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	intel, err := svc.BuildPlaceIntel(ctx, "unstick", place)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll("/opt/cursor/artifacts", 0o755)
	b, _ := json.MarshalIndent(intel, "", "  ")
	_ = os.WriteFile(filepath.Join("/opt/cursor/artifacts", "unstick-importer.json"), b, 0o644)

	named, noise, av, withIn := 0, 0, 0, 0
	for _, d := range intel.DecisionMakers {
		if d.Name == "" {
			continue
		}
		if IsValidPersonName(d.Name) && QualifiesAsDecisionMaker(d, place.Title) {
			named++
			if isRealAvatarURL(d.Avatar) {
				av++
			}
			if strings.Contains(strings.ToLower(d.LinkedIn), "linkedin.com/in/") {
				withIn++
			}
			t.Logf("dm %q av=%v li=%s", d.Name, isRealAvatarURL(d.Avatar), d.LinkedIn)
		} else {
			noise++
			t.Logf("NOISE %q", d.Name)
		}
	}
	t.Logf("named=%d noise=%d avatars=%d with_in=%d conf=%s note=%s provider=%s",
		named, noise, av, withIn, intel.Confidence, intel.Note, intel.Provider)
	if named < 3 {
		t.Fatalf("expected several named people, got %d", named)
	}
	if av < 2 {
		t.Fatalf("expected real avatars, got %d/%d", av, named)
	}
	if noise > 0 {
		t.Fatalf("noise=%d", noise)
	}
}
