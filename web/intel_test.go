package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestMaxRadiusKm(t *testing.T) {
	if MaxRadiusKm() != 50 {
		t.Fatalf("MaxRadiusKm=%d want 50", MaxRadiusKm())
	}
	if MaxRadiusMeters() != 50000 {
		t.Fatalf("MaxRadiusMeters=%d want 50000", MaxRadiusMeters())
	}
}

func TestParseTargetRadiusMeters(t *testing.T) {
	mk := func(values url.Values) *http.Request {
		req := &http.Request{Form: values, PostForm: values}
		return req
	}

	meters, err := parseTargetRadiusMeters(mk(url.Values{"radius_km": {"12"}}))
	if err != nil || meters != 12000 {
		t.Fatalf("radius_km=12 -> %d, %v", meters, err)
	}

	_, err = parseTargetRadiusMeters(mk(url.Values{"radius_km": {"51"}}))
	if err == nil || !strings.Contains(err.Error(), "≤ 50") {
		t.Fatalf("expected max error, got %v", err)
	}

	meters, err = parseTargetRadiusMeters(mk(url.Values{"radius": {"8000"}}))
	if err != nil || meters != 8000 {
		t.Fatalf("radius meters -> %d, %v", meters, err)
	}

	meters, err = parseTargetRadiusMeters(mk(url.Values{}))
	if err != nil || meters != MaxRadiusMeters() {
		t.Fatalf("default -> %d, %v want %d", meters, err, MaxRadiusMeters())
	}
}

func TestFilterDecisionMakersDropsHallucinations(t *testing.T) {
	evidence := "contact us at sales@acme.id ceo alice tan leads the team"
	known := []string{"sales@acme.id"}
	in := []DecisionMaker{
		{Name: "Alice Tan", Title: "CEO", Email: "sales@acme.id"},
		{Name: "Fake Person", Title: "CTO", Email: "fake@nowhere.test"},
		{Name: "", Title: "Sales", Email: "sales@acme.id"},
	}
	out := filterDecisionMakers(in, evidence, known)
	if len(out) != 2 {
		t.Fatalf("want 2 verified makers, got %d: %+v", len(out), out)
	}
}

func TestSanitizeDecisionMakersDropsJunk(t *testing.T) {
	place := Place{Title: "PT Acme", Phone: "+62211234567", WhatsApp: "+62211234567"}
	in := []DecisionMaker{
		{Name: "（页面提及管理/创始相关头衔，需人工核实）", Title: "Management / Founder mention", Confidence: "low"},
		{Name: "athangemilangperkasa", Title: "Contact", Email: "athangemilangperkasa@gmail.com"},
		{Name: "PT Acme", Title: "Primary business contact", Email: "info@acme.id", Phone: "+62211234567"},
		{Name: "Alice Tan", Title: "Purchasing Manager", Email: "alice@acme.id", LinkedIn: "https://linkedin.com/in/alice-tan"},
	}
	out := sanitizeDecisionMakers(in, place)
	out = pruneOfficeInboxesWhenPeopleExist(out)
	if len(out) < 2 {
		t.Fatalf("want >=2 usable contacts, got %d: %+v", len(out), out)
	}
	for _, d := range out {
		if strings.Contains(d.Name, "核实") || strings.EqualFold(d.Name, "athangemilangperkasa") || strings.EqualFold(d.Name, "PT Acme") {
			t.Fatalf("junk name survived: %+v", d)
		}
		if d.Email != "" && isGenericOfficeEmailLocal(emailLocal(d.Email)) && d.Name == "" {
			t.Fatalf("office inbox should be pruned when named people exist: %+v", d)
		}
	}
	enrichDecisionMakerAvatars(out)
	sortDecisionMakersForOutreach(out)
	if out[0].Name != "Alice Tan" {
		t.Fatalf("expected Alice first for outreach, got %+v", out[0])
	}
	// 头像只能来自真实公开档案；抓不到就留空，由前端渲染首字母，不再造 identicon。
	if out[0].Avatar != "" && !isRealAvatarURL(out[0].Avatar) {
		t.Fatalf("only real profile images may be set, got %q", out[0].Avatar)
	}
}

func TestDeugroStyleEmailFloodNotDecisionMakers(t *testing.T) {
	place := Place{Title: "PT Deugro Indonesia", Address: "Jakarta, Indonesia"}
	emails := []string{
		"info-australia-milton@deugro.com",
		"info-indonesia@deugro.com",
		"info-china-shanghai@deugro.com",
		"info@deugro.com",
		"sarina.yance@deugro.com",
		"lindo@deugro.com",
		"deugro-airfreight-germany@deugro.com",
		"infosec.privacy@deugro-group.com",
	}
	for i := 0; i < 40; i++ {
		emails = append(emails, fmt.Sprintf("info-country%d@deugro.com", i))
	}
	prioritized := prioritizeExtraEmails(place, emails)
	if len(prioritized) > 24 {
		t.Fatalf("email flood not capped: %d", len(prioritized))
	}
	makers := heuristicDecisionMakers(nil, prioritized, place)
	makers = sanitizeDecisionMakers(makers, place)
	makers = pruneOfficeInboxesWhenPeopleExist(makers)
	if len(makers) > 5 {
		t.Fatalf("too many decision makers from email flood: %d %+v", len(makers), makers)
	}
	for _, d := range makers {
		if strings.HasPrefix(strings.ToLower(emailLocal(d.Email)), "info-") {
			t.Fatalf("info-* must not be decision maker when person emails exist: %+v", d)
		}
		if d.Name != "" && (strings.Contains(d.Name, "info") || strings.Contains(d.Name, "australia")) {
			t.Fatalf("junk name: %+v", d)
		}
	}
	hasSarina := false
	for _, d := range makers {
		if strings.Contains(strings.ToLower(d.Email), "sarina.yance") || d.Name == "Sarina Yance" {
			hasSarina = true
		}
	}
	if !hasSarina {
		t.Fatalf("expected sarina.yance person contact, got %+v", makers)
	}
}

func TestCompanyBrandToken(t *testing.T) {
	if got := companyBrandToken("PT Deugro Indonesia"); got != "Deugro" {
		t.Fatalf("got %q want Deugro", got)
	}
}

func TestLinkedInSlugToName(t *testing.T) {
	name := linkedInSlugToName("https://www.linkedin.com/in/alice-tan-a1b2c3")
	if name != "Alice Tan" {
		t.Fatalf("got %q", name)
	}
	if linkedInSlugToName("https://www.linkedin.com/in/x") != "" {
		t.Fatal("single token should be empty")
	}
}

func TestPublicFacingNoteNoOSINTNoise(t *testing.T) {
	note := publicFacingNote(&PlaceIntel{
		ExtraEmails: []string{"info@acme.id"},
		Phones:      []string{"+6221"},
		DecisionMakers: []DecisionMaker{
			{Name: "Alice Tan", Email: "alice@acme.id", Confidence: "high"},
		},
	})
	if strings.Contains(note, "SpiderFoot") || strings.Contains(note, "证据驱动") {
		t.Fatalf("noise in note: %s", note)
	}
	if !strings.Contains(note, "可核验") {
		t.Fatalf("unexpected note: %s", note)
	}
}

func TestParseLinkedInPublicMetaFromHTML(t *testing.T) {
	// 用正则直接测样本，避免依赖外网
	htmlBody := `<html><head>
<meta property="og:title" content="Alice Tan - Purchasing Manager - Acme | LinkedIn"/>
<meta property="og:image" content="https://media.licdn.com/dms/image/v2/ABC/profile-displayphoto-shrink_200_200/0/1"/>
</head></body></html>`
	if m := ogImageRe.FindStringSubmatch(htmlBody); len(m) < 2 || m[1] == "" && m[2] == "" {
		t.Fatalf("og:image not matched: %#v", m)
	}
	if m := ogTitleRe.FindStringSubmatch(htmlBody); len(m) < 2 {
		t.Fatal("og:title not matched")
	}
}
