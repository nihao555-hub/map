//go:build liveintel

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveFullIntelQuality 完整背调实测：噪声 / 质量 / 完整度。
func TestLiveFullIntelQuality(t *testing.T) {
	st := ProbeOSINTTools()
	b, _ := json.MarshalIndent(st, "", "  ")
	t.Logf("OSINT probe:\n%s", b)
	_ = os.MkdirAll("/opt/cursor/artifacts", 0o755)
	_ = os.WriteFile("/opt/cursor/artifacts/osint-probe.json", b, 0o644)

	required := []struct {
		name string
		ok   bool
	}{
		{"theHarvester", st.TheHarvester},
		{"SpiderFoot", st.SpiderFoot},
		{"Photon", st.Photon},
		{"Amass", st.Amass},
		{"Katana", st.Katana},
		{"Blackbird", st.Blackbird},
		{"Maigret", st.Maigret},
		{"CrossLinked", st.CrossLinked},
		{"AHU", st.AHU},
		{"AHU_PROXY", st.AHUProxyConfigured},
		{"Holehe", st.Holehe},
	}
	var missing []string
	for _, r := range required {
		if !r.ok {
			missing = append(missing, r.name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("required tools missing: %v", missing)
	}

	cases := []Place{
		{
			PlaceID:  "q_importer",
			Title:    "PT Sempurna Nilai Sukses",
			Category: "Import service",
			Address:  "Jakarta, Indonesia",
			Website:  "https://importer.co.id/",
			Phone:    "+622138250098",
			Emails:   "athangemilangperkasa@gmail.com",
		},
		{
			PlaceID:  "q_gordi",
			Title:    "Gordi Indonesia",
			Category: "Coffee",
			Address:  "Jakarta, Indonesia",
			Website:  "https://gordi.id/",
		},
		{
			PlaceID:  "q_fore",
			Title:    "Fore Coffee",
			Category: "Coffee",
			Address:  "Jakarta, Indonesia",
			Website:  "https://www.fore.coffee",
		},
		{
			PlaceID:  "q_eiger",
			Title:    "Eiger Adventure",
			Category: "Outdoor",
			Address:  "Indonesia",
			Website:  "https://www.eigeradventure.com",
		},
		{
			PlaceID:  "q_sidomuncul",
			Title:    "Sido Muncul",
			Category: "Herbal",
			Address:  "Indonesia",
			Website:  "https://www.sidomuncul.co.id",
		},
	}

	dir := filepath.Join(os.TempDir(), "gmaps-full-intel-quality")
	_ = os.RemoveAll(dir)
	_ = os.MkdirAll(dir, 0o755)
	svc := NewService(nil, dir)

	type scoreRow struct {
		Title             string   `json:"title"`
		Seconds           float64  `json:"seconds"`
		Confidence        string   `json:"confidence"`
		Provider          string   `json:"provider"`
		Note              string   `json:"note"`
		Emails            int      `json:"emails"`
		Phones            int      `json:"phones"`
		SocialKeys        []string `json:"social_keys"`
		Makers            int      `json:"makers"`
		NamedPeople       int      `json:"named_people"`
		NoisePeople       int      `json:"noise_people"`
		ContactablePeople int      `json:"contactable_people"`
		WithLinkedIn      int      `json:"with_linkedin"`
		WithProfiles      int      `json:"with_maigret_profiles"`
		OrgUnits          int      `json:"org_units"`
		Registry          bool     `json:"registry"`
		RegistrySource    string   `json:"registry_source,omitempty"`
		Sources           []string `json:"sources"`
		HasAHU            bool     `json:"has_ahu"`
		HasCrossLinked    bool     `json:"has_crosslinked"`
		HasLinkedInXRay   bool     `json:"has_linkedin_xray"`
		NoiseNames        []string `json:"noise_names,omitempty"`
		MakerSamples      []string `json:"maker_samples"`
		Channels          []string `json:"channels"`
		CompletenessPct   int      `json:"completeness_pct"`
		Err               string   `json:"err,omitempty"`
	}

	var rows []scoreRow
	totalNoise, totalNamed := 0, 0

	for _, place := range cases {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		intel, err := svc.BuildPlaceIntel(ctx, "full-quality", place)
		cancel()
		r := scoreRow{Title: place.Title, Seconds: time.Since(start).Seconds()}
		if err != nil || intel == nil {
			if err != nil {
				r.Err = err.Error()
			} else {
				r.Err = "nil intel"
			}
			rows = append(rows, r)
			t.Errorf("%s: %v", place.Title, err)
			continue
		}

		out := filepath.Join("/opt/cursor/artifacts", "full-intel-"+place.PlaceID+".json")
		raw, _ := json.MarshalIndent(intel, "", "  ")
		_ = os.WriteFile(out, raw, 0o644)

		r.Confidence = intel.Confidence
		r.Provider = intel.Provider
		r.Note = intel.Note
		r.Emails = len(intel.ExtraEmails)
		r.Phones = len(intel.Phones)
		r.Sources = append([]string{}, intel.Sources...)
		r.OrgUnits = len(intel.OrgStructure)
		r.Registry = intel.CompanyRegistry != nil
		if intel.CompanyRegistry != nil {
			r.RegistrySource = intel.CompanyRegistry.Source
		}
		for k, v := range intel.Socials {
			if strings.TrimSpace(v) != "" {
				r.SocialKeys = append(r.SocialKeys, k)
			}
		}
		blob := strings.ToLower(strings.Join(intel.Sources, " ") + " " + intel.Provider)
		r.HasAHU = strings.Contains(blob, "ahu")
		r.HasCrossLinked = strings.Contains(blob, "crosslinked")
		r.HasLinkedInXRay = strings.Contains(blob, "linkedin")

		r.Makers = len(intel.DecisionMakers)
		for _, d := range intel.DecisionMakers {
			sample := fmt.Sprintf("%s|%s|%s", d.Name, d.Title, d.Source)
			if len(r.MakerSamples) < 5 {
				r.MakerSamples = append(r.MakerSamples, sample)
			}
			if d.Name == "" {
				continue
			}
			if IsValidPersonName(d.Name) && QualifiesAsDecisionMaker(d, place.Title) {
				r.NamedPeople++
			} else {
				r.NoisePeople++
				r.NoiseNames = append(r.NoiseNames, d.Name+"|"+d.Source)
			}
			if d.Email != "" || d.Phone != "" || d.WhatsApp != "" || d.LinkedIn != "" {
				r.ContactablePeople++
			}
			if d.LinkedIn != "" {
				r.WithLinkedIn++
			}
			if len(d.Profiles) > 0 {
				r.WithProfiles++
			}
		}
		if r.Emails > 0 {
			r.Channels = append(r.Channels, "email")
		}
		if r.Phones > 0 {
			r.Channels = append(r.Channels, "phone")
		}
		if intel.Socials != nil && intel.Socials["whatsapp"] != "" {
			r.Channels = append(r.Channels, "whatsapp")
		}
		for _, k := range []string{"linkedin", "facebook", "instagram"} {
			if intel.Socials != nil && intel.Socials[k] != "" {
				r.Channels = append(r.Channels, k)
			}
		}

		// 完整度：联系渠道 + 具名决策人 + 可联 + 主体/组织 + 社媒 共 6 维
		score := 0
		if r.Emails > 0 {
			score++
		}
		if r.Phones > 0 {
			score++
		}
		if r.NamedPeople > 0 {
			score++
		}
		if r.ContactablePeople > 0 {
			score++
		}
		if r.Registry || r.OrgUnits > 0 {
			score++
		}
		if len(r.SocialKeys) > 0 {
			score++
		}
		r.CompletenessPct = score * 100 / 6

		totalNoise += r.NoisePeople
		totalNamed += r.NamedPeople
		rows = append(rows, r)

		t.Logf("[%s] conf=%s named=%d noise=%d contactable=%d emails=%d phones=%d org=%d reg=%v ahu=%v li=%v complete=%d%% (%.0fs) note=%s",
			place.Title, r.Confidence, r.NamedPeople, r.NoisePeople, r.ContactablePeople,
			r.Emails, r.Phones, r.OrgUnits, r.Registry, r.HasAHU, r.HasLinkedInXRay,
			r.CompletenessPct, r.Seconds, r.Note)
		for _, n := range r.NoiseNames {
			t.Logf("  NOISE %s", n)
		}
		for _, s := range r.MakerSamples {
			t.Logf("  DM %s", s)
		}

		if r.NoisePeople > 0 {
			t.Errorf("%s: noise people=%d %v", place.Title, r.NoisePeople, r.NoiseNames)
		}
		if strings.Contains(r.Note, "SpiderFoot") || strings.Contains(r.Note, "证据驱动") {
			t.Errorf("%s: debug noise in note: %s", place.Title, r.Note)
		}
	}

	sumPath := "/opt/cursor/artifacts/full-intel-quality-summary.json"
	sumRaw, _ := json.MarshalIndent(rows, "", "  ")
	_ = os.WriteFile(sumPath, sumRaw, 0o644)

	avgComplete := 0
	nOK := 0
	for _, r := range rows {
		if r.Err == "" {
			avgComplete += r.CompletenessPct
			nOK++
		}
	}
	if nOK > 0 {
		avgComplete /= nOK
	}
	t.Logf("TOTAL named=%d noise=%d avg_completeness=%d%% cases=%d/%d", totalNamed, totalNoise, avgComplete, nOK, len(cases))
	if totalNoise > 0 {
		t.Fatalf("expected near-zero noise, got %d", totalNoise)
	}
	if nOK < len(cases) {
		t.Fatalf("some cases failed: %d/%d", nOK, len(cases))
	}
}
