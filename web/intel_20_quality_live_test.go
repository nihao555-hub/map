//go:build liveintel

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLiveIntel20Quality 20 家完整背调：噪声 / 具名 / 头像 / 完整度。
func TestLiveIntel20Quality(t *testing.T) {
	st := ProbeOSINTTools()
	t.Logf("osint: th=%v sf=%v photon=%v amass=%v katana=%v blackbird=%v maigret=%v crosslinked=%v ahu=%v ahuProxy=%v",
		st.TheHarvester, st.SpiderFoot, st.Photon, st.Amass, st.Katana, st.Blackbird, st.Maigret, st.CrossLinked, st.AHU, st.AHUProxyConfigured)
	if !st.TheHarvester || !st.SpiderFoot || !st.Photon || !st.Amass || !st.Katana || !st.Blackbird {
		t.Fatalf("core OSINT tools missing: %+v", st)
	}

	fixtures := freshUncommonFixtures20
	if len(fixtures) != 20 {
		t.Fatalf("need 20 fixtures, got %d", len(fixtures))
	}

	dir := filepath.Join(os.TempDir(), "gmaps-intel-20-quality")
	_ = os.RemoveAll(dir)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.MkdirAll("/opt/cursor/artifacts", 0o755)
	svc := NewService(nil, dir)

	type row struct {
		Title             string   `json:"title"`
		Website           string   `json:"website"`
		Seconds           float64  `json:"seconds"`
		Confidence        string   `json:"confidence"`
		Provider          string   `json:"provider"`
		Note              string   `json:"note"`
		Emails            int      `json:"emails"`
		Phones            int      `json:"phones"`
		Makers            int      `json:"makers"`
		NamedPeople       int      `json:"named_people"`
		NoisePeople       int      `json:"noise_people"`
		WithAvatar        int      `json:"with_avatar"`
		NamedWithAvatar   int      `json:"named_with_avatar"`
		WithLinkedIn      int      `json:"with_linkedin"`
		ContactablePeople int      `json:"contactable_people"`
		OrgUnits          int      `json:"org_units"`
		Registry          bool     `json:"registry"`
		HasAHU            bool     `json:"has_ahu"`
		Channels          []string `json:"channels"`
		MakerSamples      []string `json:"maker_samples,omitempty"`
		NoiseNames        []string `json:"noise_names,omitempty"`
		CompletenessPct   int      `json:"completeness_pct"`
		Err               string   `json:"err,omitempty"`
	}

	var (
		mu   sync.Mutex
		rows []row
		wg   sync.WaitGroup
	)
	sem := make(chan struct{}, 3)
	for i, f := range fixtures {
		f := f
		i := i
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			place := Place{
				Title:    f.Title,
				Website:  f.Website,
				Category: f.Category,
				Address:  f.Address,
				Phone:    f.Phone,
				WhatsApp: f.Phone,
				PlaceID:  fmt.Sprintf("live20_%02d", i+1),
			}
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			intel, err := svc.BuildPlaceIntel(ctx, "live-20-quality", place)
			cancel()
			r := row{Title: f.Title, Website: f.Website, Seconds: time.Since(start).Seconds()}
			if err != nil || intel == nil {
				if err != nil {
					r.Err = err.Error()
				} else {
					r.Err = "nil intel"
				}
			} else {
				raw, _ := json.MarshalIndent(intel, "", "  ")
				_ = os.WriteFile(filepath.Join("/opt/cursor/artifacts", fmt.Sprintf("live20-%02d.json", i+1)), raw, 0o644)

				r.Confidence = intel.Confidence
				r.Provider = intel.Provider
				r.Note = intel.Note
				r.Emails = len(intel.ExtraEmails)
				r.Phones = len(intel.Phones)
				r.Makers = len(intel.DecisionMakers)
				r.OrgUnits = len(intel.OrgStructure)
				r.Registry = intel.CompanyRegistry != nil
				blob := strings.ToLower(strings.Join(intel.Sources, " ") + " " + intel.Provider)
				r.HasAHU = strings.Contains(blob, "ahu")
				for _, d := range intel.DecisionMakers {
					if d.Name != "" {
						if IsValidPersonName(d.Name) && QualifiesAsDecisionMaker(d, place.Title) {
							r.NamedPeople++
							if isRealAvatarURL(d.Avatar) {
								r.NamedWithAvatar++
							}
						} else {
							r.NoisePeople++
							r.NoiseNames = append(r.NoiseNames, d.Name+"|"+d.Source)
						}
					}
					if isRealAvatarURL(d.Avatar) {
						r.WithAvatar++
					}
					if d.LinkedIn != "" {
						r.WithLinkedIn++
					}
					if d.Email != "" || d.Phone != "" || d.WhatsApp != "" || d.LinkedIn != "" {
						r.ContactablePeople++
					}
					sample := fmt.Sprintf("%s|%s|av=%v|%s", d.Name, d.Title, isRealAvatarURL(d.Avatar), d.Source)
					if len(r.MakerSamples) < 3 {
						r.MakerSamples = append(r.MakerSamples, sample)
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
				if len(r.Channels) > 0 {
					score++
				}
				r.CompletenessPct = score * 100 / 6
			}
			mu.Lock()
			rows = append(rows, r)
			mu.Unlock()
			t.Logf("[%02d] %s named=%d noise=%d avatar=%d/%d emails=%d phones=%d complete=%d%% (%.0fs) err=%s",
				i+1, f.Title, r.NamedPeople, r.NoisePeople, r.NamedWithAvatar, r.NamedPeople, r.Emails, r.Phones, r.CompletenessPct, r.Seconds, r.Err)
		}()
	}
	wg.Wait()

	sumPath := "/opt/cursor/artifacts/live20-quality-summary.json"
	raw, _ := json.MarshalIndent(rows, "", "  ")
	_ = os.WriteFile(sumPath, raw, 0o644)

	okN, namedN, noiseN, avatarNamed, namedTotal, emailN, phoneN, completeSum := 0, 0, 0, 0, 0, 0, 0, 0
	for _, r := range rows {
		if r.Err == "" {
			okN++
		}
		if r.NamedPeople > 0 {
			namedN++
		}
		noiseN += r.NoisePeople
		avatarNamed += r.NamedWithAvatar
		namedTotal += r.NamedPeople
		if r.Emails > 0 {
			emailN++
		}
		if r.Phones > 0 {
			phoneN++
		}
		completeSum += r.CompletenessPct
		if r.NoisePeople > 0 {
			t.Errorf("%s noise=%d %v", r.Title, r.NoisePeople, r.NoiseNames)
		}
	}
	avgComplete := 0
	if len(rows) > 0 {
		avgComplete = completeSum / len(rows)
	}
	avatarRate := 0
	if namedTotal > 0 {
		avatarRate = avatarNamed * 100 / namedTotal
	}
	summary := fmt.Sprintf(
		"n=%d ok=%d named_firms=%d named_people=%d noise=%d named_avatar=%d/%d (%d%%) email>0=%d phone>0=%d avg_complete=%d%%",
		len(rows), okN, namedN, namedTotal, noiseN, avatarNamed, namedTotal, avatarRate, emailN, phoneN, avgComplete,
	)
	t.Log(summary)
	_ = os.WriteFile("/opt/cursor/artifacts/live20-quality-summary.txt", []byte(summary+"\n"), 0o644)

	if okN < 18 {
		t.Fatalf("too many failures: ok=%d/20", okN)
	}
	if noiseN > 0 {
		t.Fatalf("noise people present: %d", noiseN)
	}
}
