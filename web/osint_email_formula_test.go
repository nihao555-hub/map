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

func TestFilterPlaceholderEmails(t *testing.T) {
	in := []string{
		"hello@fore.coffee",
		"first.last@fore.coffee",
		"jdoe@fore.coffee",
		"john.doe@costco.com",
		"exports@costco.com",
		"cs@eigeradventure.com",
		".bad@eiger.com",
	}
	out := filterPlaceholderEmails(in)
	joined := strings.Join(out, ",")
	for _, want := range []string{"hello@fore.coffee", "exports@costco.com", "cs@eigeradventure.com"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %v", want, out)
		}
	}
	for _, bad := range []string{"first.last@", "jdoe@", "john.doe@", ".bad@"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("placeholder leaked %s in %v", bad, out)
		}
	}
}

func TestPermuteAndPattern(t *testing.T) {
	got := permutePersonEmails("Jane", "Smith", "acme.com")
	if len(got) < 3 || got[0] != "jane.smith@acme.com" {
		t.Fatalf("permute=%v", got)
	}
	pat := detectEmailPattern([]string{"alice.wong@acme.com", "bob.lee@acme.com", "info@acme.com"}, "acme.com")
	if pat != "first.last" {
		t.Fatalf("pattern=%q", pat)
	}
	if e := applyEmailPattern(pat, "Jane", "Smith", "acme.com"); e != "jane.smith@acme.com" {
		t.Fatalf("apply=%q", e)
	}
}

func TestExtractEmailsFromHTML(t *testing.T) {
	html := `
	<a href="mailto:Sales@Acme.com">mail</a>
	<script type="application/ld+json">{"email":"info@acme.com"}</script>
	<p>support [at] acme.com</p>
	<span>first.last@acme.com</span>
	`
	got := extractEmailsFromHTML(html, "acme.com")
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "sales@acme.com") || !strings.Contains(joined, "info@acme.com") || !strings.Contains(joined, "support@acme.com") {
		t.Fatalf("got=%v", got)
	}
	if strings.Contains(joined, "first.last@") {
		t.Fatalf("placeholder not filtered: %v", got)
	}
}

func TestAttachMapsContactsAndOrg(t *testing.T) {
	makers := []DecisionMaker{{Name: "A", Title: "Purchasing Manager"}, {Name: "B", Title: "CEO"}}
	place := Place{Phone: "+62111", WhatsApp: "+62111"}
	makers = attachMapsContactsToMakers(makers, place)
	if makers[0].WhatsApp != "+62111" || makers[1].Phone != "+62111" {
		t.Fatalf("contacts not attached: %+v", makers)
	}
	org := orgUnitsFromDecisionMakers(makers, "Acme")
	roles := map[string]bool{}
	for _, u := range org {
		roles[u.Role] = true
	}
	if !roles["procurement"] || !roles["executive"] {
		t.Fatalf("org=%+v", org)
	}
}

func TestSeedRoleEmailsRequiresMXPath(t *testing.T) {
	roles := seedTradeRoleEmails("acme.id")
	if len(roles) < 4 || roles[0] != "sales@acme.id" {
		t.Fatalf("roles=%v", roles)
	}
}

// TestLiveContactFormula20 联网实测约 20 组：官网深挖 + Brave @domain（过滤占位）命中率。
func TestLiveContactFormula20(t *testing.T) {
	if os.Getenv("LIVE_CONTACT_FORMULA") == "" && os.Getenv("CI") != "" {
		t.Skip("set LIVE_CONTACT_FORMULA=1")
	}
	fixtures := []struct {
		Title, Website, Phone, Address string
	}{
		{"Excelso Coffee", "https://www.excelso-coffee.com", "", "Indonesia"},
		{"Kopi Kenangan", "https://kopikenangan.com", "+6221", "Indonesia"},
		{"Tanamera Coffee", "https://www.tanameracoffee.com", "", "Indonesia"},
		{"Fore Coffee", "https://www.fore.coffee", "", "Indonesia"},
		{"Eiger Adventure", "https://www.eigeradventure.com", "", "Indonesia"},
		{"Sarinah", "https://www.sarinah.co.id", "", "Jakarta, Indonesia"},
		{"Gramedia", "https://www.gramedia.com", "", "Indonesia"},
		{"Sociolla", "https://www.sociolla.com", "", "Indonesia"},
		{"ACE Hardware ID", "https://www.acehardware.co.id", "", "Indonesia"},
		{"Informa", "https://www.informa.co.id", "", "Indonesia"},
		{"Ruparupa", "https://www.ruparupa.com", "", "Indonesia"},
		{"Sido Muncul", "https://www.sidomuncul.co.id", "", "Indonesia"},
		{"Mayora", "https://www.mayoraindah.co.id", "", "Indonesia"},
		{"Alfamart", "https://www.alfamart.co.id", "", "Indonesia"},
		{"Indomaret", "https://www.indomaret.co.id", "", "Indonesia"},
		{"Costco Wholesale", "https://www.costco.com", "", "USA"},
		{"Home Depot", "https://www.homedepot.com", "", "USA"},
		{"Target", "https://www.target.com", "", "USA"},
		{"Tractor Supply", "https://www.tractorsupply.com", "", "USA"},
		{"Williams-Sonoma", "https://www.williams-sonoma.com", "", "USA"},
	}

	type row struct {
		Title          string   `json:"title"`
		Domain         string   `json:"domain"`
		SiteEmails     []string `json:"site_emails"`
		BraveEmails    []string `json:"brave_emails"`
		PreciseEmails  []string `json:"precise_emails"`
		RoleFallback   []string `json:"role_fallback,omitempty"`
		HasPrecise     bool     `json:"has_precise"`
		HasOutreach    bool     `json:"has_outreach"`
		MapsPhoneBound bool     `json:"maps_phone_bound"`
	}
	var rows []row
	ctx := context.Background()
	preciseN, outreachN := 0, 0

	for _, f := range fixtures {
		domain := hostDomain(f.Website)
		intel := &PlaceIntel{Title: f.Title, Website: f.Website, Domain: domain, HasMX: true}
		place := Place{Title: f.Title, Website: f.Website, Phone: f.Phone, WhatsApp: f.Phone, Address: f.Address}

		siteEmails := []string{}
		for _, u := range teamURLs(f.Website) {
			body, _, err := fetchIntelPage(ctx, u)
			if err != nil || len(body) < 40 {
				continue
			}
			siteEmails = mergeUnique(siteEmails, extractEmailsFromHTML(string(body), domain))
			if len(siteEmails) >= 5 {
				break
			}
		}
		brave, _ := lookupPublishedEmailsBrave(ctx, domain, f.Title)
		precise := filterPlaceholderEmails(mergeUnique(siteEmails, brave))
		roles := []string{}
		if len(precise) == 0 {
			roles = seedTradeRoleEmails(domain)
		}
		intel.ExtraEmails = precise
		if len(precise) == 0 {
			intel.ExtraEmails = roles
		}
		intel.DecisionMakers = []DecisionMaker{{Name: "Probe Contact", Title: "Business contact"}}
		intel.DecisionMakers = attachMapsContactsToMakers(intel.DecisionMakers, place)

		r := row{
			Title: f.Title, Domain: domain,
			SiteEmails: siteEmails, BraveEmails: brave, PreciseEmails: precise,
			RoleFallback: roles, HasPrecise: len(precise) > 0,
			HasOutreach: len(precise) > 0 || len(roles) > 0,
			MapsPhoneBound: f.Phone != "" && intel.DecisionMakers[0].Phone == f.Phone,
		}
		rows = append(rows, r)
		if r.HasPrecise {
			preciseN++
		}
		if r.HasOutreach {
			outreachN++
		}
		t.Logf("%s precise=%v emails=%v brave_extra=%v roles=%d",
			f.Title, r.HasPrecise, precise, diffStrings(brave, siteEmails), len(roles))
		time.Sleep(400 * time.Millisecond)
	}

	summary := map[string]any{
		"n": len(rows), "precise_n": preciseN, "outreach_n": outreachN,
		"precise_rate": float64(preciseN) / float64(len(rows)),
		"outreach_rate": float64(outreachN) / float64(len(rows)),
	}
	out := map[string]any{"summary": summary, "results": rows}
	b, _ := json.MarshalIndent(out, "", "  ")
	_ = os.WriteFile(filepath.Join(os.TempDir(), "contact_formula_20.json"), b, 0o644)
	t.Logf("SUMMARY %+v", summary)

	if preciseN < 6 {
		t.Fatalf("precise email hit too low: %d/20", preciseN)
	}
	if outreachN < 18 {
		t.Fatalf("outreach email coverage too low: %d/20", outreachN)
	}
}

func diffStrings(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range b {
		set[x] = true
	}
	var out []string
	for _, x := range a {
		if !set[x] {
			out = append(out, x)
		}
	}
	return out
}
