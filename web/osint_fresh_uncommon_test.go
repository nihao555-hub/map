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

// freshUncommonFixtures20：与历史所有 bench 完全不重合的冷门/垂直 B2B（非咖啡连锁、非印尼便利店、非美零售巨头）。
var freshUncommonFixtures20 = []struct {
	Title, Website, Category, Address, Phone string
}{
	{"Indotropika Agung Lestari", "https://indotropika.com/", "Ornamental fish exporter", "Bekasi, Indonesia", ""},
	{"Kapok Indonesia", "https://kapokindonesia.com/", "Kapok fiber exporter", "Semarang, Indonesia", ""},
	{"Juseng Huat Trading", "http://jusenghuat.asia/", "Dried seafood wholesaler", "Batu Caves, Malaysia", "+60361208290"},
	{"Van Aroma", "https://www.vanaroma.com/", "Fragrance ingredients", "Indonesia", ""},
	{"Phapros", "https://www.phapros.co.id/", "Pharmaceuticals", "Indonesia", ""},
	{"Ultra Prima Abadi", "https://www.ultraplas.com/", "Packaging plastics", "Indonesia", ""},
	{"Triputra Agro Persada", "https://www.tap-agri.com/", "Plantation", "Indonesia", ""},
	{"Eagle High Plantations", "https://www.eaglehighplantations.com/", "Palm oil", "Indonesia", ""},
	{"London Sumatra", "https://www.londonsumatra.com/", "Plantation", "Indonesia", ""},
	{"Karya Hijau", "https://karyahijau.com/", "Agriculture", "Indonesia", ""},
	{"Sampoerna Agro", "https://www.sampoernaagro.com/", "Palm oil", "Indonesia", ""},
	{"Tunas Baru Lampung", "https://www.tunasbarulampung.com/", "Agribusiness", "Lampung, Indonesia", ""},
	{"Bumitama Agri", "https://www.bumitama-agri.com/", "Palm oil", "Indonesia", ""},
	{"Sekar Laut", "https://www.sekarlaut.com/", "Seafood snacks", "Indonesia", ""},
	{"Sekar Bumi", "https://www.sekarbumi.com/", "Seafood processing", "Indonesia", ""},
	{"Kedawung Setia Industrial", "https://www.kedawungsetia.com/", "Housewares manufacturer", "Indonesia", ""},
	{"Langgeng Makmur Industri", "https://www.lmi.co.id/", "Household products", "Indonesia", ""},
	{"Kedaung Indah Can", "https://www.kedaung.com/", "Enamelware", "Indonesia", ""},
	{"Central Proteina Prima", "https://www.cpp.co.id/", "Shrimp aquaculture", "Indonesia", ""},
	{"Harum Energy", "https://www.harumenergy.com/", "Coal energy", "Indonesia", ""},
}

func TestFreshFixturesNoOverlapWithHistory(t *testing.T) {
	banned := []string{
		"excelso", "kopikenangan", "tanamera", "commongrounds", "jco", "breadtalk", "eiger", "consina",
		"sarinah", "gramedia", "sariayu", "sociolla", "orami", "acehardware", "informa", "ruparupa",
		"erafone", "guardian", "alfamart", "indomaret", "alfagift", "jiwagroup", "fore.coffee",
		"sidomuncul", "mayora", "costco", "homedepot", "target.com", "tractorsupply", "williams-sonoma",
		"walmart", "deugro", "gordi.id", "importer.co.id", "athan.co.id", "abtrade",
	}
	for _, f := range freshUncommonFixtures20 {
		blob := strings.ToLower(f.Title + " " + f.Website)
		for _, b := range banned {
			if strings.Contains(blob, b) {
				t.Fatalf("overlap with historical fixture %q in %s", b, f.Title)
			}
		}
	}
	if len(freshUncommonFixtures20) != 20 {
		t.Fatalf("need exactly 20, got %d", len(freshUncommonFixtures20))
	}
}

func TestHarvestContactChannelsFromHTML(t *testing.T) {
	html := `
		<a href="https://wa.me/6281234567890">WA</a>
		<a href="tel:+62215551234">call</a>
		<a href="https://www.instagram.com/acme.id">ig</a>
		<a href="https://www.facebook.com/acmeid">fb</a>
		<a href="https://t.me/acme">tg</a>
		<a href="https://www.linkedin.com/company/acme">li</a>
	`
	phones, wa, socials := harvestContactChannelsFromHTML(html)
	if wa != "+6281234567890" {
		t.Fatalf("wa=%q", wa)
	}
	if len(phones) == 0 {
		t.Fatalf("no phones")
	}
	if socials["instagram"] == "" || socials["facebook"] == "" || socials["telegram"] == "" || socials["linkedin"] == "" {
		t.Fatalf("socials=%v", socials)
	}
	if deriveWhatsAppFromPhone("+62 812-3456-7890", "") != "+6281234567890" {
		t.Fatalf("derive failed")
	}
}

// TestLiveFreshUncommon20Channels 对全新 20 家冷门公司测：邮箱/电话/WhatsApp/社媒 + 决策人 + 架构。
func TestLiveFreshUncommon20Channels(t *testing.T) {
	// Opt-in like the other live benches: 20 concurrent OSINT runs exceed the
	// default 10m package timeout and fail the whole suite.
	if testing.Short() || os.Getenv("LIVE_OSINT_CHANNELS") == "" {
		t.Skip("set LIVE_OSINT_CHANNELS=1")
	}
	type row struct {
		Title             string   `json:"title"`
		Website           string   `json:"website"`
		OK                bool     `json:"ok"`
		Emails            int      `json:"emails"`
		Phones            int      `json:"phones"`
		WhatsApp          bool     `json:"whatsapp"`
		SocialKeys        []string `json:"social_keys,omitempty"`
		Makers            int      `json:"decision_makers"`
		NamedPeople       int      `json:"named_people"`
		ContactablePeople int      `json:"contactable_people"`
		OrgUnits          int      `json:"org_units"`
		OrgRoles          []string `json:"org_roles,omitempty"`
		MakerSamples      []string `json:"maker_samples,omitempty"`
		Channels          []string `json:"channels,omitempty"`
		Confidence        string   `json:"confidence"`
		Provider          string   `json:"provider"`
		Note              string   `json:"note,omitempty"`
		Seconds           float64  `json:"seconds"`
		Err               string   `json:"err,omitempty"`
	}

	dir := t.TempDir()
	svc := &Service{dataFolder: dir}
	var (
		mu   sync.Mutex
		rows []row
		wg   sync.WaitGroup
	)
	sem := make(chan struct{}, 3)
	for i, f := range freshUncommonFixtures20 {
		f := f
		i := i
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			place := Place{
				Title: f.Title, Website: f.Website, Category: f.Category,
				Address: f.Address, Phone: f.Phone, WhatsApp: f.Phone,
				PlaceID: fmt.Sprintf("fresh_%02d", i+1),
			}
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			intel, err := svc.BuildPlaceIntel(ctx, "fresh-uncommon-20", place)
			cancel()
			r := row{Title: f.Title, Website: f.Website, Seconds: time.Since(start).Seconds()}
			if err != nil || intel == nil {
				if err != nil {
					r.Err = err.Error()
				} else {
					r.Err = "nil intel"
				}
			} else {
				r.OK = intel.Status == IntelReady || len(intel.ExtraEmails)+len(intel.Phones)+len(intel.DecisionMakers) > 0
				r.Emails = len(intel.ExtraEmails)
				r.Phones = len(intel.Phones)
				r.WhatsApp = intel.Socials != nil && intel.Socials["whatsapp"] != ""
				for k, v := range intel.Socials {
					if strings.TrimSpace(v) != "" {
						r.SocialKeys = append(r.SocialKeys, k)
					}
				}
				r.Makers = len(intel.DecisionMakers)
				r.NamedPeople = countNamedPeople(intel.DecisionMakers)
				for _, d := range intel.DecisionMakers {
					if d.Email != "" || d.Phone != "" || d.WhatsApp != "" || d.LinkedIn != "" {
						r.ContactablePeople++
					}
					sample := strings.TrimSpace(d.Name + "|" + d.Title + "|" + d.Source)
					if sample != "||" && len(r.MakerSamples) < 3 {
						r.MakerSamples = append(r.MakerSamples, sample)
					}
				}
				r.OrgUnits = len(intel.OrgStructure)
				for _, u := range intel.OrgStructure {
					if u.Role != "" {
						r.OrgRoles = append(r.OrgRoles, u.Role)
					}
				}
				if r.Emails > 0 {
					r.Channels = append(r.Channels, "email")
				}
				if r.Phones > 0 {
					r.Channels = append(r.Channels, "phone")
				}
				if r.WhatsApp {
					r.Channels = append(r.Channels, "whatsapp")
				}
				for _, k := range []string{"linkedin", "facebook", "instagram", "telegram"} {
					if intel.Socials != nil && intel.Socials[k] != "" {
						r.Channels = append(r.Channels, k)
					}
				}
				r.Confidence = intel.Confidence
				r.Provider = intel.Provider
				r.Note = intel.Note
			}
			mu.Lock()
			rows = append(rows, r)
			mu.Unlock()
			t.Logf("[%02d] %s ok=%v ch=%v emails=%d phones=%d wa=%v makers=%d named=%d contactable=%d org=%d roles=%v (%.0fs) err=%s",
				i+1, f.Title, r.OK, r.Channels, r.Emails, r.Phones, r.WhatsApp, r.Makers, r.NamedPeople, r.ContactablePeople, r.OrgUnits, r.OrgRoles, r.Seconds, r.Err)
		}()
	}
	wg.Wait()

	okN, anyCh, emailN, phoneN, waN, socialN, makerN, namedN, contactN, orgN := 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	for _, r := range rows {
		if r.OK {
			okN++
		}
		if len(r.Channels) > 0 {
			anyCh++
		}
		if r.Emails > 0 {
			emailN++
		}
		if r.Phones > 0 {
			phoneN++
		}
		if r.WhatsApp {
			waN++
		}
		if len(r.SocialKeys) > 0 {
			socialN++
		}
		if r.Makers > 0 {
			makerN++
		}
		if r.NamedPeople > 0 {
			namedN++
		}
		if r.ContactablePeople > 0 {
			contactN++
		}
		if r.OrgUnits > 0 {
			orgN++
		}
	}
	summary := map[string]any{
		"n": len(rows), "ok": okN, "any_channel": anyCh,
		"email": emailN, "phone": phoneN, "whatsapp": waN, "social": socialN,
		"makers": makerN, "named_people": namedN, "contactable_people": contactN, "org": orgN,
	}
	out := map[string]any{"summary": summary, "results": rows}
	b, _ := json.MarshalIndent(out, "", "  ")
	outPath := filepath.Join(os.TempDir(), "fresh_uncommon_20_channels.json")
	_ = os.WriteFile(outPath, b, 0o644)
	_ = os.MkdirAll("/opt/cursor/artifacts", 0o755)
	_ = os.WriteFile("/opt/cursor/artifacts/fresh_uncommon_20_channels.json", b, 0o644)
	t.Logf("SUMMARY %+v wrote %s", summary, outPath)

	if okN < 15 {
		t.Fatalf("too many failures: %+v", summary)
	}
	if anyCh < 12 {
		t.Fatalf("too few companies with any contact channel: %+v", summary)
	}
	if makerN < 10 {
		t.Fatalf("too few decision-maker rows: %+v", summary)
	}
	if orgN < 8 {
		t.Fatalf("too few org structures: %+v", summary)
	}
}
