package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// PlaceIntel 商户背调：公司架构 + 决策人 + 域名情报（官网 + theHarvester/SpiderFoot/OpenCorporates）。
type PlaceIntel struct {
	PlaceID         string            `json:"place_id"`
	Title           string            `json:"title"`
	Website         string            `json:"website"`
	Domain          string            `json:"domain,omitempty"`
	Status          string            `json:"status"` // pending|running|ready|failed|skipped
	Summary         string            `json:"summary,omitempty"`
	OrgStructure    []OrgUnit         `json:"org_structure,omitempty"`
	DecisionMakers  []DecisionMaker   `json:"decision_makers,omitempty"`
	ExtraEmails     []string          `json:"extra_emails,omitempty"`
	Phones          []string          `json:"phones,omitempty"`
	Socials         map[string]string `json:"socials,omitempty"`
	Technologies    []string          `json:"technologies,omitempty"`
	CompanyRegistry *CompanyHit       `json:"company_registry,omitempty"`
	MXHosts         []string          `json:"mx_hosts,omitempty"`
	HasMX           bool              `json:"has_mx,omitempty"`
	Confidence      string            `json:"confidence,omitempty"` // high|medium|low
	Sources         []string          `json:"sources,omitempty"`
	GeneratedAt     time.Time         `json:"generated_at"`
	Provider        string            `json:"provider,omitempty"`
	Note            string            `json:"note,omitempty"`
}

const (
	IntelPending = "pending"
	IntelRunning = "running"
	IntelReady   = "ready"
	IntelFailed  = "failed"
	IntelSkipped = "skipped"
)

// OrgUnit 组织架构节点
type OrgUnit struct {
	Name     string `json:"name"`
	Role     string `json:"role,omitempty"`
	Parent   string `json:"parent,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

// DecisionMaker 决策人 / 关键联系人（须有证据；无姓名时用角色+邮箱）
type DecisionMaker struct {
	Name       string `json:"name"`
	Title      string `json:"title,omitempty"`
	Email      string `json:"email,omitempty"`
	Phone      string `json:"phone,omitempty"`
	LinkedIn   string `json:"linkedin,omitempty"`
	Source     string `json:"source,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

// CompanyHit OpenCorporates 等公开主体匹配
type CompanyHit struct {
	Name          string `json:"name,omitempty"`
	CompanyNumber string `json:"company_number,omitempty"`
	Jurisdiction  string `json:"jurisdiction,omitempty"`
	CompanyType   string `json:"company_type,omitempty"`
	CurrentStatus string `json:"current_status,omitempty"`
	Incorporation string `json:"incorporation_date,omitempty"`
	RegistryURL   string `json:"registry_url,omitempty"`
	Source        string `json:"source,omitempty"`
}

var (
	teamPathHints = []string{
		"/about", "/about-us", "/aboutus", "/team", "/our-team", "/people",
		"/company", "/leadership", "/management", "/staff", "/contact",
		"/contact-us", "/contacts", "/imprint", "/impressum",
		"/kontak", "/tentang-kami", "/profil", "/struktur", "/karir",
		"/en/about", "/en/team", "/en/contact",
	}
	emailFindRe = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	phoneFindRe = regexp.MustCompile(`(?i)(?:\+|00)?[\d][\d\s\-().]{7,18}\d`)
	linkedinRe  = regexp.MustCompile(`(?i)https?://(?:www\.)?linkedin\.com/(?:in|company)/[a-z0-9\-_%/]+`)
	nameTitleRe = regexp.MustCompile(`(?i)(ceo|founder|owner|director|manager|head of|co-founder|president|partner|kepala|direktur|pendiri|pemilik|chief)`)
	techHints   = []struct {
		needle string
		label  string
	}{
		{"wp-content", "WordPress"},
		{"woocommerce", "WooCommerce"},
		{"shopify", "Shopify"},
		{"wix.com", "Wix"},
		{"squarespace", "Squarespace"},
		{"webflow", "Webflow"},
		{"gtag(", "Google Analytics"},
		{"googletagmanager", "Google Tag Manager"},
		{"facebook.com/tr", "Meta Pixel"},
		{"cdn.shopify.com", "Shopify"},
		{"react", "React"},
		{"next/static", "Next.js"},
		{"vue.", "Vue"},
		{"cloudflare", "Cloudflare"},
	}
)

const maxRadiusKm = 50 // 项目目标半径上限（公里）

// MaxRadiusKm 返回项目允许的最大目标半径（公里）。
func MaxRadiusKm() int { return maxRadiusKm }

// MaxRadiusMeters 返回项目允许的最大目标半径（米）。
func MaxRadiusMeters() int { return maxRadiusKm * 1000 }

func (s *Service) intelPath(jobID, placeID string) (string, error) {
	if strings.Contains(jobID, "..") || strings.Contains(placeID, "..") ||
		strings.ContainsAny(jobID, `/\`) || strings.ContainsAny(placeID, `/\`) {
		return "", fmt.Errorf("invalid id")
	}
	dir := filepath.Join(s.dataFolder, "intel", jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, placeID)
	if safe == "" {
		safe = "unknown"
	}
	return filepath.Join(dir, safe+".json"), nil
}

func (s *Service) loadIntel(jobID, placeID string) (*PlaceIntel, bool) {
	path, err := s.intelPath(jobID, placeID)
	if err != nil {
		return nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var out PlaceIntel
	if json.Unmarshal(b, &out) != nil {
		return nil, false
	}
	return &out, true
}

func (s *Service) saveIntel(jobID string, intel *PlaceIntel) error {
	path, err := s.intelPath(jobID, intel.PlaceID)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(intel, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// BuildPlaceIntel 对单条商家做背调：官网 + theHarvester/SpiderFoot + OpenCorporates + AI。
func (s *Service) BuildPlaceIntel(ctx context.Context, jobID string, place Place) (*PlaceIntel, error) {
	if cached, ok := s.loadIntel(jobID, place.PlaceID); ok {
		if cached.Status == IntelReady || cached.Status == IntelSkipped || cached.Status == IntelFailed {
			return cached, nil
		}
	}

	intel := &PlaceIntel{
		PlaceID:     place.PlaceID,
		Title:       place.Title,
		Website:     place.Website,
		Status:      IntelRunning,
		GeneratedAt: time.Now().UTC(),
		Provider:    "website",
		Note:        "证据驱动：官网抓取 + theHarvester/SpiderFoot（若已安装）+ OpenCorporates；不编造决策人。",
		Socials:     map[string]string{},
	}
	_ = s.saveIntel(jobID, intel)
	if place.Website != "" {
		if u, err := url.Parse(place.Website); err == nil {
			intel.Domain = strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
		}
	}

	fillSocials(intel, place)

	pageTexts := []string{}
	sources := []string{}
	rawBodies := []string{}
	if place.Website != "" {
		urls := teamURLs(place.Website)
		for _, u := range urls {
			body, headers, err := fetchIntelPage(ctx, u)
			if err != nil || len(body) < 80 {
				continue
			}
			sources = append(sources, u)
			rawBodies = append(rawBodies, string(body))
			text := stripTags(string(body))
			pageTexts = append(pageTexts, truncateRunes(text, 6000))
			intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emailFindRe.FindAllString(string(body), -1), intel.Domain))
			intel.Phones = mergeUnique(intel.Phones, filterPhones(phoneFindRe.FindAllString(text, -1)))
			for _, li := range linkedinRe.FindAllString(string(body), -1) {
				if intel.Socials["linkedin"] == "" {
					intel.Socials["linkedin"] = strings.Split(li, "?")[0]
				}
			}
			intel.Technologies = mergeUnique(intel.Technologies, detectTech(string(body), headers))
			if len(pageTexts) >= 4 {
				break
			}
		}
	}
	intel.Sources = sources

	if place.Emails != "" {
		for _, e := range strings.Split(place.Emails, ",") {
			e = strings.TrimSpace(e)
			if e != "" {
				intel.ExtraEmails = mergeUnique(intel.ExtraEmails, []string{e})
			}
		}
	}
	if place.Phone != "" {
		intel.Phones = mergeUnique(intel.Phones, []string{place.Phone})
	}
	if place.WhatsApp != "" {
		intel.Phones = mergeUnique(intel.Phones, []string{place.WhatsApp})
	}

	// MX（可投域）
	if intel.Domain != "" {
		mx, err := net.LookupMX(intel.Domain)
		if err == nil && len(mx) > 0 {
			intel.HasMX = true
			for i, m := range mx {
				if i >= 5 {
					break
				}
				intel.MXHosts = append(intel.MXHosts, strings.TrimSuffix(m.Host, "."))
			}
		}
	}

	// 真正调用已安装的 theHarvester / SpiderFoot（有域名时）
	harvesterOK, spiderOK := OSINTToolsAvailable()
	if intel.Domain != "" && harvesterOK {
		if h, err := runTheHarvester(ctx, intel.Domain); err == nil {
			applyHarvester(intel, h)
		} else {
			intel.Note = intel.Note + " theHarvester：" + truncateRunes(err.Error(), 100)
		}
	}
	if intel.Domain != "" && spiderOK {
		if ev, err := runSpiderfootLite(ctx, intel.Domain); err == nil {
			applySpiderfoot(intel, ev)
		} else {
			intel.Note = intel.Note + " SpiderFoot：" + truncateRunes(err.Error(), 100)
		}
	}

	// OpenCorporates HTTP API
	if hit := lookupOpenCorporates(ctx, place.Title, place.Address); hit != nil {
		intel.CompanyRegistry = hit
		if hit.RegistryURL != "" {
			intel.Sources = mergeUnique(intel.Sources, []string{hit.RegistryURL})
		}
	}

	seed := strings.TrimSpace(place.Descriptions + "\n" + place.About + "\n" + place.Owner)
	if seed != "" {
		pageTexts = append(pageTexts, truncateRunes(seed, 2000))
	}
	evidenceBlob := strings.ToLower(strings.Join(pageTexts, "\n") + "\n" + strings.Join(rawBodies, "\n"))

	if AITranslateEnabled() && (len(pageTexts) > 0 || place.Title != "") {
		aiOut, err := AICompanyIntel(ctx, place, strings.Join(pageTexts, "\n\n---\n\n"), intel.ExtraEmails)
		if err == nil && aiOut != nil {
			if aiOut.Summary != "" {
				intel.Summary = aiOut.Summary
			}
			if len(aiOut.OrgStructure) > 0 {
				intel.OrgStructure = filterOrgUnits(aiOut.OrgStructure, evidenceBlob)
			}
			if len(aiOut.DecisionMakers) > 0 {
				intel.DecisionMakers = filterDecisionMakers(aiOut.DecisionMakers, evidenceBlob, intel.ExtraEmails)
			}
			intel.Provider = "grsai+" + grsaiModel() + "+osint"
		} else if err != nil {
			intel.Note = intel.Note + " AI：" + err.Error()
		}
	}

	if len(intel.DecisionMakers) == 0 {
		intel.DecisionMakers = heuristicDecisionMakers(pageTexts, intel.ExtraEmails, place)
	}
	if intel.Summary == "" {
		intel.Summary = fmt.Sprintf("%s（%s）", place.Title, strings.TrimSpace(place.Category+" · "+place.Address))
	}
	if len(intel.OrgStructure) == 0 && place.Category != "" {
		intel.OrgStructure = []OrgUnit{{Name: place.Title, Role: place.Category, Evidence: "Google Maps category"}}
	}

	intel.Confidence = scoreConfidence(intel)
	intel.Status = IntelReady
	intel.GeneratedAt = time.Now().UTC()
	_ = s.saveIntel(jobID, intel)
	return intel, nil
}

var intelJobRunning sync.Map // jobID -> struct{}

// StartJobIntel 并发背调任务内全部商户（有官网/域名优先）；不阻塞抓取主流程。
func (s *Service) StartJobIntel(ctx context.Context, jobID string) {
	if _, loaded := intelJobRunning.LoadOrStore(jobID, struct{}{}); loaded {
		return // 已有一轮在跑
	}
	go func() {
		defer intelJobRunning.Delete(jobID)
		s.runJobIntel(context.WithoutCancel(ctx), jobID)
	}()
}

func (s *Service) runJobIntel(ctx context.Context, jobID string) {
	places, err := s.GetPlaces(ctx, jobID)
	if err != nil || len(places) == 0 {
		return
	}
	sem := make(chan struct{}, 3) // 并发上限，避免打爆 OSINT 源
	var wg sync.WaitGroup
	for i := range places {
		p := places[i]
		if p.PlaceID == "" {
			continue
		}
		if cached, ok := s.loadIntel(jobID, p.PlaceID); ok &&
			(cached.Status == IntelReady || cached.Status == IntelSkipped) {
			continue
		}
		// 无任何可查线索：直接 skipped，允许前端展开（仅 Maps 字段）
		if strings.TrimSpace(p.Website) == "" && strings.TrimSpace(p.Emails) == "" &&
			strings.TrimSpace(p.Phone) == "" {
			_ = s.saveIntel(jobID, &PlaceIntel{
				PlaceID: p.PlaceID, Title: p.Title, Status: IntelSkipped,
				GeneratedAt: time.Now().UTC(),
				Note:        "无可公开背调线索（无官网/邮箱/电话）",
				Confidence:  "low",
			})
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(place Place) {
			defer wg.Done()
			defer func() { <-sem }()
			cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()
			if _, err := s.BuildPlaceIntel(cctx, jobID, place); err != nil {
				_ = s.saveIntel(jobID, &PlaceIntel{
					PlaceID: place.PlaceID, Title: place.Title, Website: place.Website,
					Status: IntelFailed, GeneratedAt: time.Now().UTC(),
					Note: err.Error(), Confidence: "low",
				})
			}
		}(p)
	}
	wg.Wait()
}

// JobIntelStatus 任务级背调进度
type JobIntelStatus struct {
	JobID   string `json:"job_id"`
	Total   int    `json:"total"`
	Ready   int    `json:"ready"`
	Pending int    `json:"pending"`
	Running int    `json:"running"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
	Done    bool   `json:"done"`
}

// GetJobIntelStatus 扫描 intel 缓存目录统计进度。
func (s *Service) GetJobIntelStatus(jobID string, placeCount int) JobIntelStatus {
	st := JobIntelStatus{JobID: jobID, Total: placeCount}
	dir := filepath.Join(s.dataFolder, "intel", jobID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		st.Pending = placeCount
		st.Done = placeCount == 0
		return st
	}
	seen := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var pi PlaceIntel
		if json.Unmarshal(b, &pi) != nil {
			continue
		}
		seen++
		switch pi.Status {
		case IntelReady:
			st.Ready++
		case IntelRunning:
			st.Running++
		case IntelFailed:
			st.Failed++
		case IntelSkipped:
			st.Skipped++
		default:
			st.Pending++
		}
	}
	if placeCount > seen {
		st.Pending += placeCount - seen
	}
	finished := st.Ready + st.Failed + st.Skipped
	st.Done = placeCount > 0 && finished >= placeCount && st.Running == 0
	if placeCount == 0 {
		st.Done = true
	}
	return st
}

func fillSocials(intel *PlaceIntel, place Place) {
	add := func(k, v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			intel.Socials[k] = v
		}
	}
	add("facebook", place.Facebook)
	add("instagram", place.Instagram)
	add("linkedin", place.LinkedIn)
	add("twitter", place.Twitter)
	add("tiktok", place.TikTok)
	add("youtube", place.YouTube)
	add("telegram", place.Telegram)
	add("whatsapp", place.WhatsApp)
}

func teamURLs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return []string{raw}
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	base := u.Scheme + "://" + u.Host
	out := []string{raw}
	for _, p := range teamPathHints {
		out = append(out, base+p)
	}
	return uniqueStrings(out)
}

func fetchIntelPage(ctx context.Context, rawURL string) ([]byte, http.Header, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; gmaps-intel/1.0; +https://github.com/gosom/google-maps-scraper)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, resp.Header, fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 400<<10))
	return b, resp.Header, err
}

func detectTech(body string, headers http.Header) []string {
	low := strings.ToLower(body)
	var out []string
	for _, t := range techHints {
		if strings.Contains(low, t.needle) {
			out = append(out, t.label)
		}
	}
	if headers != nil {
		if server := headers.Get("Server"); server != "" {
			out = append(out, "Server:"+server)
		}
		if powered := headers.Get("X-Powered-By"); powered != "" {
			out = append(out, powered)
		}
	}
	return uniqueStrings(out)
}

func lookupOpenCorporates(ctx context.Context, name, address string) *CompanyHit {
	q := strings.TrimSpace(name)
	if q == "" {
		return nil
	}
	// 去掉过长尾缀，提高命中
	if len([]rune(q)) > 80 {
		q = string([]rune(q)[:80])
	}
	u := "https://api.opencorporates.com/v0.4/companies/search?q=" + url.QueryEscape(q) + "&per_page=5"
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "gmaps-intel/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return nil
	}
	var parsed struct {
		Results struct {
			Companies []struct {
				Company struct {
					Name              string `json:"name"`
					CompanyNumber     string `json:"company_number"`
					JurisdictionCode  string `json:"jurisdiction_code"`
					CompanyType       string `json:"company_type"`
					CurrentStatus     string `json:"current_status"`
					IncorporationDate string `json:"incorporation_date"`
					OpencorporatesURL string `json:"opencorporates_url"`
					RegisteredAddress string `json:"registered_address_in_full"`
				} `json:"company"`
			} `json:"companies"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &parsed) != nil || len(parsed.Results.Companies) == 0 {
		return nil
	}
	// 选名称相似度最高且地址有交集的一条；否则取第一条
	best := parsed.Results.Companies[0].Company
	addrLow := strings.ToLower(address)
	nameLow := strings.ToLower(name)
	for _, c := range parsed.Results.Companies {
		co := c.Company
		coName := strings.ToLower(co.Name)
		if strings.Contains(coName, nameLow) || strings.Contains(nameLow, coName) {
			best = co
			break
		}
		if addrLow != "" && co.RegisteredAddress != "" &&
			(strings.Contains(strings.ToLower(co.RegisteredAddress), firstToken(addrLow)) ||
				strings.Contains(addrLow, strings.ToLower(firstToken(co.RegisteredAddress)))) {
			best = co
			break
		}
	}
	return &CompanyHit{
		Name:          best.Name,
		CompanyNumber: best.CompanyNumber,
		Jurisdiction:  best.JurisdictionCode,
		CompanyType:   best.CompanyType,
		CurrentStatus: best.CurrentStatus,
		Incorporation: best.IncorporationDate,
		RegistryURL:   best.OpencorporatesURL,
		Source:        "opencorporates",
	}
}

func firstToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return s
	}
	return fields[0]
}

func stripTags(s string) string {
	re := regexp.MustCompile(`(?s)<script.*?>.*?</script>|<style.*?>.*?</style>|<[^>]+>`)
	s = re.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func filterPublicEmails(in []string, domain string) []string {
	var out []string
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || strings.Contains(e, "example.") || strings.Contains(e, "sentry") ||
			strings.Contains(e, "wixpress") || strings.Contains(e, "schema.org") ||
			strings.HasSuffix(e, ".png") || strings.HasSuffix(e, ".jpg") ||
			strings.Contains(e, "noreply") || strings.Contains(e, "no-reply") {
			continue
		}
		if domain != "" && !strings.HasSuffix(e, "@"+domain) {
			// 仍保留同站外公开邮箱，但优先域名内
			if strings.Count(e, "@") != 1 {
				continue
			}
		}
		out = append(out, e)
	}
	return uniqueStrings(out)
}

func filterPhones(in []string) []string {
	var out []string
	for _, p := range in {
		digits := regexp.MustCompile(`\d`).FindAllString(p, -1)
		if len(digits) < 8 || len(digits) > 15 {
			continue
		}
		out = append(out, strings.TrimSpace(p))
	}
	return uniqueStrings(out)
}

func mergeUnique(a, b []string) []string {
	return uniqueStrings(append(append([]string{}, a...), b...))
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// filterDecisionMakers 丢弃无证据的 AI 编造姓名；邮箱须出现在已知列表或原文。
func filterDecisionMakers(in []DecisionMaker, evidenceLow string, knownEmails []string) []DecisionMaker {
	known := map[string]bool{}
	for _, e := range knownEmails {
		known[strings.ToLower(strings.TrimSpace(e))] = true
	}
	var out []DecisionMaker
	for _, d := range in {
		d.Email = strings.TrimSpace(d.Email)
		d.Name = strings.TrimSpace(d.Name)
		d.Title = strings.TrimSpace(d.Title)
		emailOK := d.Email != "" && (known[strings.ToLower(d.Email)] || strings.Contains(evidenceLow, strings.ToLower(d.Email)))
		nameOK := d.Name != "" && len([]rune(d.Name)) >= 2 && strings.Contains(evidenceLow, strings.ToLower(d.Name))
		titleOK := d.Title != "" && nameTitleRe.MatchString(d.Title)
		if !emailOK && !nameOK {
			// 无姓名证据且无邮箱证据 → 丢弃（防幻觉）
			continue
		}
		if d.Evidence == "" {
			switch {
			case emailOK && nameOK:
				d.Evidence = "name+email in source"
			case emailOK:
				d.Evidence = "email in source"
			default:
				d.Evidence = "name in source"
			}
		}
		if d.Confidence == "" {
			if emailOK && nameOK {
				d.Confidence = "high"
			} else if emailOK || (nameOK && titleOK) {
				d.Confidence = "medium"
			} else {
				d.Confidence = "low"
			}
		}
		if d.Source == "" {
			d.Source = "ai-verified"
		}
		out = append(out, d)
	}
	return out
}

func filterOrgUnits(in []OrgUnit, evidenceLow string) []OrgUnit {
	var out []OrgUnit
	for _, o := range in {
		o.Name = strings.TrimSpace(o.Name)
		if o.Name == "" {
			continue
		}
		if o.Evidence == "" && !strings.Contains(evidenceLow, strings.ToLower(o.Name)) {
			// 无证据的部门名降级保留但标注
			o.Evidence = "inferred (unverified)"
		}
		out = append(out, o)
	}
	return out
}

func scoreConfidence(intel *PlaceIntel) string {
	score := 0
	if len(intel.Sources) > 0 {
		score += 2
	}
	if len(intel.ExtraEmails) > 0 {
		score++
	}
	if intel.HasMX {
		score++
	}
	if intel.CompanyRegistry != nil {
		score += 2
	}
	highDM := 0
	for _, d := range intel.DecisionMakers {
		if d.Confidence == "high" || (d.Email != "" && d.Name != "" && !strings.Contains(d.Name, "核实")) {
			highDM++
		}
	}
	if highDM > 0 {
		score += 2
	} else if len(intel.DecisionMakers) > 0 {
		score++
	}
	switch {
	case score >= 6:
		return "high"
	case score >= 3:
		return "medium"
	default:
		return "low"
	}
}

func heuristicDecisionMakers(pages []string, emails []string, place Place) []DecisionMaker {
	var out []DecisionMaker
	blob := strings.Join(pages, " ")
	if nameTitleRe.MatchString(blob) {
		out = append(out, DecisionMaker{
			Name:       "（页面提及管理/创始相关头衔，需人工核实）",
			Title:      "Management / Founder mention",
			Source:     "heuristic",
			Evidence:   "title keyword in page text",
			Confidence: "low",
		})
	}
	for _, e := range emails {
		local := e
		if i := strings.Index(e, "@"); i > 0 {
			local = e[:i]
		}
		title := "Contact"
		low := strings.ToLower(local)
		conf := "medium"
		switch {
		case strings.Contains(low, "ceo"), strings.Contains(low, "founder"):
			title = "Executive contact"
			conf = "high"
		case strings.Contains(low, "owner"), strings.Contains(low, "direktur"):
			title = "Owner / Director"
			conf = "high"
		case strings.Contains(low, "sales"), strings.Contains(low, "marketing"):
			title = "Sales / Marketing"
		case strings.Contains(low, "info"), strings.Contains(low, "hello"), strings.Contains(low, "contact"):
			title = "General inquiry"
			conf = "low"
		}
		out = append(out, DecisionMaker{
			Name:       local,
			Title:      title,
			Email:      e,
			Phone:      place.Phone,
			Source:     "email-pattern",
			Evidence:   "email harvested from public page/maps",
			Confidence: conf,
		})
	}
	if len(out) == 0 && (place.Phone != "" || place.WhatsApp != "" || place.Emails != "") {
		out = append(out, DecisionMaker{
			Name:       place.Title,
			Title:      "Primary business contact",
			Email:      firstCSV(place.Emails),
			Phone:      firstNonEmpty(place.WhatsApp, place.Phone),
			Source:     "maps",
			Evidence:   "Google Maps listing fields",
			Confidence: "medium",
		})
	}
	return out
}

func firstCSV(s string) string {
	parts := strings.Split(s, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// AICompanyIntel 用 GRSAI 从页面文本归纳架构与决策人（严格禁止编造）。
func AICompanyIntel(ctx context.Context, place Place, pageText string, knownEmails []string) (*PlaceIntel, error) {
	key := grsaiAPIKey()
	if key == "" {
		return nil, fmt.Errorf("AI disabled")
	}

	system := `You are a B2B sales intelligence analyst. From public website text and Google Maps business fields, extract:
1) A 2-3 sentence company summary
2) Org structure units (departments / brands) ONLY if mentioned
3) Decision makers or key contacts (name, title, email, phone, linkedin) ONLY if evidenced in the text.
CRITICAL: Do NOT invent people, titles, or emails. If unsure, omit. Prefer empty arrays over guesses.
Every decision_maker must include evidence (short quote or field name).
Return STRICT JSON:
{"summary":"...","org_structure":[{"name":"...","role":"...","parent":"...","evidence":"..."}],"decision_makers":[{"name":"...","title":"...","email":"...","phone":"...","linkedin":"...","source":"...","evidence":"...","confidence":"high|medium|low"}]}
No markdown.`

	user := fmt.Sprintf(`Business: %s
Category: %s
Address: %s
Phone: %s
WhatsApp: %s
Emails: %s
Website: %s
Known emails: %s

Website excerpts:
%s`, place.Title, place.Category, place.Address, place.Phone, place.WhatsApp, place.Emails, place.Website,
		strings.Join(knownEmails, ", "), truncateRunes(pageText, 12000))

	body, err := json.Marshal(aiChatRequest{
		Model:  grsaiModel(),
		Stream: false,
		Messages: []aiChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, grsaiHost()+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 55 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai status %d: %s", resp.StatusCode, truncateRunes(string(raw), 180))
	}

	var parsed aiChatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("empty ai choices")
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var out PlaceIntel
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
		if start >= 0 && end > start {
			if err2 := json.Unmarshal([]byte(content[start:end+1]), &out); err2 != nil {
				return nil, fmt.Errorf("ai json: %w", err)
			}
		} else {
			return nil, fmt.Errorf("ai json: %w", err)
		}
	}
	return &out, nil
}
