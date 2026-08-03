package web

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// DecisionMaker 决策人 / 关键联系人（须有证据；优先可触达：邮箱/电话/WhatsApp/LinkedIn）。
type DecisionMaker struct {
	Name       string `json:"name"`
	Title      string `json:"title,omitempty"`
	Email      string `json:"email,omitempty"`
	Phone      string `json:"phone,omitempty"`
	WhatsApp   string `json:"whatsapp,omitempty"`
	LinkedIn   string `json:"linkedin,omitempty"`
	Avatar     string `json:"avatar,omitempty"`
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
		Note:        "背调中",
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
		if len(urls) > 10 {
			urls = urls[:10]
		}
		type pageHit struct {
			u       string
			body    []byte
			headers http.Header
		}
		hits := make([]pageHit, len(urls))
		var pageWg sync.WaitGroup
		for i, u := range urls {
			pageWg.Add(1)
			go func(i int, u string) {
				defer pageWg.Done()
				body, headers, err := fetchIntelPage(ctx, u)
				if err != nil || len(body) < 80 {
					return
				}
				hits[i] = pageHit{u: u, body: body, headers: headers}
			}(i, u)
		}
		pageWg.Wait()
		nPages := 0
		for _, h := range hits {
			if len(h.body) == 0 {
				continue
			}
			sources = append(sources, h.u)
			rawBodies = append(rawBodies, string(h.body))
			text := stripTags(string(h.body))
			pageTexts = append(pageTexts, truncateRunes(text, 6000))
			intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emailFindRe.FindAllString(string(h.body), -1), intel.Domain))
			intel.Phones = mergeUnique(intel.Phones, filterPhones(phoneFindRe.FindAllString(text, -1)))
			for _, li := range linkedinRe.FindAllString(string(h.body), -1) {
				if intel.Socials["linkedin"] == "" {
					intel.Socials["linkedin"] = strings.Split(li, "?")[0]
				}
			}
			intel.Technologies = mergeUnique(intel.Technologies, detectTech(string(h.body), h.headers))
			intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, extractPeopleFromText(text, h.u))
			nPages++
			if nPages >= 6 {
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

	// MX（必须带 context，避免坏 DNS 永久挂死）
	if intel.Domain != "" {
		rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
		resolver := &net.Resolver{}
		mx, err := resolver.LookupMX(rctx, intel.Domain)
		rcancel()
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

	// OSINT 工具并行（总预算受限，避免串行叠满数分钟）
	st := ProbeOSINTTools()
	runOSINTEnrichment(ctx, intel, place, st)

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
			intel.Provider = strings.Trim(intel.Provider+"+grsai:"+grsaiModel(), "+")
		}
		// AI 失败不污染对外说明
	}

	if countNamedPeople(intel.DecisionMakers) == 0 {
		// 无真名时再用邮箱角色启发式补「可联系渠道」（不再伪造人名）
		intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, heuristicDecisionMakers(pageTexts, intel.ExtraEmails, place))
	}
	intel.DecisionMakers = dedupeDecisionMakers(intel.DecisionMakers)
	intel.DecisionMakers = attachLinkedInSearchHints(intel.DecisionMakers, place.Title)
	intel.DecisionMakers = sanitizeDecisionMakers(intel.DecisionMakers, place)
	enrichDecisionMakerAvatars(intel.DecisionMakers)
	sortDecisionMakersForOutreach(intel.DecisionMakers)
	if intel.Summary == "" {
		intel.Summary = fmt.Sprintf("%s（%s）", place.Title, strings.TrimSpace(place.Category+" · "+place.Address))
	}
	if len(intel.OrgStructure) == 0 && place.Category != "" {
		intel.OrgStructure = []OrgUnit{{Name: place.Title, Role: place.Category, Evidence: "Google Maps category"}}
	}

	intel.Note = publicFacingNote(intel)
	intel.Confidence = scoreConfidence(intel)
	intel.Status = IntelReady
	intel.GeneratedAt = time.Now().UTC()
	_ = s.saveIntel(jobID, intel)
	return intel, nil
}

var intelJobRunning sync.Map // jobID -> struct{}
var intelPlaceRunning sync.Map // jobID|placeID -> struct{}

// EnsurePlaceIntelAsync 后台生成单商户背调（避免 GET 一直假 pending）。
func (s *Service) EnsurePlaceIntelAsync(jobID string, place Place) {
	ensurePlaceKey(&place)
	if place.PlaceID == "" {
		return
	}
	key := jobID + "|" + place.PlaceID
	if _, loaded := intelPlaceRunning.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	if cached, ok := s.loadIntel(jobID, place.PlaceID); ok &&
		(cached.Status == IntelReady || cached.Status == IntelSkipped || cached.Status == IntelFailed) {
		intelPlaceRunning.Delete(key)
		return
	}
	_ = s.saveIntel(jobID, &PlaceIntel{
		PlaceID: place.PlaceID, Title: place.Title, Website: place.Website,
		Status: IntelRunning, GeneratedAt: time.Now().UTC(),
		Note: "背调中",
	})
	go func(p Place) {
		defer intelPlaceRunning.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		if _, err := s.BuildPlaceIntel(ctx, jobID, p); err != nil {
			_ = s.saveIntel(jobID, &PlaceIntel{
				PlaceID: p.PlaceID, Title: p.Title, Website: p.Website,
				Status: IntelFailed, GeneratedAt: time.Now().UTC(),
				Note: err.Error(), Confidence: "low",
			})
		}
	}(place)
}

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
		ensurePlaceKey(&p)
		places[i] = p
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
	// 浏览器 UA：部分 .co.id / WAF（Sucuri 等）会拦自定义爬虫标识
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "id-ID,id;q=0.9,en-US;q=0.8,en;q=0.7")
	client := &http.Client{Timeout: 12 * time.Second}
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
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
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
			strings.HasSuffix(e, ".gif") || strings.HasSuffix(e, ".svg") ||
			strings.HasSuffix(e, ".webp") || strings.Contains(e, "@2x.") ||
			strings.Contains(e, "noreply") || strings.Contains(e, "no-reply") ||
			strings.HasPrefix(e, "abuse@") || strings.Contains(e, "@namecheap.") ||
			strings.Contains(e, "@godaddy.") || strings.Contains(e, "@cloudflare.") ||
			strings.Contains(e, "@domainsbyproxy.") || strings.Contains(e, "@privacy") {
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
		p = strings.TrimSpace(p)
		// 印尼电商价格常被误识别成电话（如 130.000 - 15）
		if strings.Count(p, ".") >= 2 || strings.Contains(p, "000 -") {
			continue
		}
		digitRe := regexp.MustCompile(`\d`)
		digits := digitRe.FindAllString(p, -1)
		if len(digits) < 8 || len(digits) > 15 {
			continue
		}
		joined := strings.Join(digits, "")
		if !(strings.HasPrefix(joined, "62") || strings.HasPrefix(joined, "0") || strings.Contains(p, "+")) {
			continue
		}
		out = append(out, p)
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
	named := countNamedPeople(intel.DecisionMakers)
	contactable := 0
	highDM := 0
	for _, d := range intel.DecisionMakers {
		if d.Email != "" || d.Phone != "" || d.WhatsApp != "" || d.LinkedIn != "" {
			contactable++
		}
		if d.Confidence == "high" && looksLikeRealPerson(d) {
			highDM++
		}
	}
	if highDM > 0 {
		score += 3
	} else if named > 0 {
		score += 2
	}
	if contactable > 0 {
		score += 2
	}
	if len(intel.ExtraEmails) > 0 {
		score++
	}
	if len(intel.Phones) > 0 || (intel.Socials != nil && intel.Socials["whatsapp"] != "") {
		score++
	}
	if intel.Socials != nil && intel.Socials["linkedin"] != "" {
		score++
	}
	if intel.CompanyRegistry != nil {
		score += 2
	}
	if len(intel.Sources) >= 3 {
		score++
	}
	// 没有可联系渠道时，不允许标 high（外贸实战无用）
	if contactable == 0 && len(intel.ExtraEmails) == 0 && len(intel.Phones) == 0 {
		if score > 2 {
			score = 2
		}
	}
	switch {
	case score >= 6 && (named > 0 || contactable > 0):
		return "high"
	case score >= 3:
		return "medium"
	default:
		return "low"
	}
}

func heuristicDecisionMakers(pages []string, emails []string, place Place) []DecisionMaker {
	var out []DecisionMaker
	// 邮箱角色线索：不当成人名，Name 留空，仅作可触达联系人
	for _, e := range emails {
		e = strings.TrimSpace(e)
		if e == "" || !strings.Contains(e, "@") {
			continue
		}
		local := e[:strings.Index(e, "@")]
		title := roleFromEmailLocal(local)
		out = append(out, DecisionMaker{
			Name:       "",
			Title:      title,
			Email:      e,
			Phone:      firstNonEmpty(place.WhatsApp, place.Phone),
			WhatsApp:   place.WhatsApp,
			Source:     "email-pattern",
			Evidence:   "public email on website/maps",
			Confidence: emailRoleConfidence(local),
		})
	}
	if len(out) == 0 {
		phone := firstNonEmpty(place.WhatsApp, place.Phone)
		email := firstCSV(place.Emails)
		if phone != "" || email != "" {
			out = append(out, DecisionMaker{
				Name:       "",
				Title:      "Business contact",
				Email:      email,
				Phone:      phone,
				WhatsApp:   place.WhatsApp,
				LinkedIn:   place.LinkedIn,
				Source:     "maps",
				Evidence:   "Google Maps listing contact fields",
				Confidence: "medium",
			})
		}
	}
	return out
}

func roleFromEmailLocal(local string) string {
	low := strings.ToLower(local)
	switch {
	case strings.Contains(low, "purchas"), strings.Contains(low, "procure"), strings.Contains(low, "buyer"), strings.Contains(low, "sourcing"):
		return "Purchasing / Sourcing"
	case strings.Contains(low, "ceo"), strings.Contains(low, "founder"), strings.Contains(low, "owner"):
		return "Owner / Executive"
	case strings.Contains(low, "direktur"), strings.Contains(low, "director"):
		return "Director"
	case strings.Contains(low, "sales"), strings.Contains(low, "marketing"), strings.Contains(low, "export"), strings.Contains(low, "import"):
		return "Sales / Trade"
	case strings.Contains(low, "info"), strings.Contains(low, "hello"), strings.Contains(low, "contact"), strings.Contains(low, "admin"), strings.Contains(low, "office"):
		return "General inquiry"
	default:
		return "Email contact"
	}
}

func emailRoleConfidence(local string) string {
	low := strings.ToLower(local)
	switch {
	case strings.Contains(low, "purchas"), strings.Contains(low, "procure"), strings.Contains(low, "ceo"), strings.Contains(low, "founder"), strings.Contains(low, "owner"), strings.Contains(low, "direktur"):
		return "high"
	case strings.Contains(low, "info"), strings.Contains(low, "hello"), strings.Contains(low, "contact"), strings.Contains(low, "admin"):
		return "low"
	default:
		return "medium"
	}
}

// sanitizeDecisionMakers 去掉假人名 / 占位文案，补齐 WhatsApp，并给可联人打分。
func sanitizeDecisionMakers(in []DecisionMaker, place Place) []DecisionMaker {
	var out []DecisionMaker
	for _, d := range in {
		d.Name = strings.TrimSpace(d.Name)
		d.Title = strings.TrimSpace(d.Title)
		d.Email = strings.TrimSpace(d.Email)
		d.Phone = strings.TrimSpace(d.Phone)
		d.WhatsApp = strings.TrimSpace(d.WhatsApp)
		d.LinkedIn = strings.Split(strings.TrimSpace(d.LinkedIn), "?")[0]
		hadOwnChannel := d.Email != "" || d.Phone != "" || d.WhatsApp != "" || d.LinkedIn != ""
		// 丢弃占位 / 垃圾名（先清名，再决定是否继承门店电话）
		if isJunkPersonName(d.Name) {
			d.Name = ""
		}
		// 邮箱 local 当人名 → 清空名，保留邮箱
		if d.Email != "" {
			local := d.Email
			if i := strings.Index(d.Email, "@"); i > 0 {
				local = d.Email[:i]
			}
			if strings.EqualFold(d.Name, local) {
				d.Name = ""
				if d.Title == "" || d.Title == "Contact" || isJunkTitle(d.Title) {
					d.Title = roleFromEmailLocal(local)
				}
			}
		}
		// 公司名当人名
		if d.Name != "" && place.Title != "" && strings.EqualFold(d.Name, strings.TrimSpace(place.Title)) {
			d.Name = ""
			if d.Title == "" || isJunkTitle(d.Title) {
				d.Title = "Business contact"
			}
		}
		// 仅对「本来就有渠道或真名」的联系人补门店 WhatsApp/电话，避免假决策人继承成空壳
		if d.Name != "" || hadOwnChannel {
			if d.WhatsApp == "" && looksLikeWhatsApp(d.Phone) {
				d.WhatsApp = d.Phone
			}
			if d.WhatsApp == "" && place.WhatsApp != "" && (d.Email != "" || d.Name == "") {
				d.WhatsApp = place.WhatsApp
			}
			if d.Phone == "" {
				d.Phone = firstNonEmpty(d.WhatsApp, place.Phone)
			}
		}
		if d.Name == "" && d.Email == "" && d.Phone == "" && d.WhatsApp == "" && d.LinkedIn == "" {
			continue
		}
		// 清名后无真名且原本无任何渠道 → 丢（假「需人工核实」类）
		if d.Name == "" && !hadOwnChannel {
			continue
		}
		if d.Name == "" && d.Title == "" {
			d.Title = "Contact"
		}
		// 垃圾职称清理
		if isJunkTitle(d.Title) {
			if d.Email != "" {
				local := d.Email
				if i := strings.Index(d.Email, "@"); i > 0 {
					local = d.Email[:i]
				}
				d.Title = roleFromEmailLocal(local)
			} else if d.LinkedIn != "" {
				d.Title = "LinkedIn contact"
			} else {
				d.Title = "Contact"
			}
		}
		out = append(out, d)
	}
	return out
}

func isJunkTitle(title string) bool {
	low := strings.ToLower(strings.TrimSpace(title))
	if low == "" {
		return false
	}
	junk := []string{
		"management / founder mention", "mentioned officer", "name from osint",
		"primary business contact", "title keyword",
	}
	for _, j := range junk {
		if low == j || strings.Contains(low, j) {
			return true
		}
	}
	return false
}

// attachLinkedInSearchHints 给有真名但无个人主页的联系人补 LinkedIn 搜人链接，方便一键建联。
func attachLinkedInSearchHints(makers []DecisionMaker, company string) []DecisionMaker {
	company = strings.TrimSpace(company)
	for i := range makers {
		if makers[i].LinkedIn != "" {
			continue
		}
		if !looksLikeRealPerson(makers[i]) {
			continue
		}
		q := makers[i].Name
		if company != "" {
			q += " " + company
		}
		makers[i].LinkedIn = "https://www.linkedin.com/search/results/people/?keywords=" + url.QueryEscape(q)
		if makers[i].Evidence == "" {
			makers[i].Evidence = "LinkedIn people search hint"
		}
	}
	return makers
}

func isJunkPersonName(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return false
	}
	low := strings.ToLower(n)
	if strings.Contains(n, "核实") || strings.Contains(n, "（") || strings.Contains(n, "(") && strings.Contains(low, "mention") {
		return true
	}
	if strings.Contains(low, "management / founder") || strings.Contains(low, "primary business contact") {
		return true
	}
	if strings.Contains(low, "mentioned officer") && len(strings.Fields(n)) < 2 {
		return true
	}
	junkExact := map[string]bool{
		"contact": true, "admin": true, "info": true, "sales": true, "owner": true,
		"manager": true, "unknown": true, "n/a": true, "-": true,
	}
	return junkExact[low]
}

func looksLikeWhatsApp(s string) bool {
	d := regexp.MustCompile(`[^\d+]`).ReplaceAllString(s, "")
	return strings.HasPrefix(d, "+") || (len(d) >= 10 && strings.HasPrefix(d, "62"))
}

func enrichDecisionMakerAvatars(makers []DecisionMaker) {
	for i := range makers {
		if makers[i].Avatar != "" {
			continue
		}
		if makers[i].Email != "" {
			makers[i].Avatar = gravatarURL(makers[i].Email)
			continue
		}
		if makers[i].Name != "" {
			makers[i].Avatar = uiAvatarURL(makers[i].Name)
		}
	}
}

func gravatarURL(email string) string {
	e := strings.TrimSpace(strings.ToLower(email))
	sum := md5.Sum([]byte(e))
	return "https://www.gravatar.com/avatar/" + hex.EncodeToString(sum[:]) + "?d=identicon&s=96"
}

func uiAvatarURL(name string) string {
	return "https://ui-avatars.com/api/?name=" + url.QueryEscape(name) + "&background=3b82f6&color=fff&size=96"
}

func sortDecisionMakersForOutreach(makers []DecisionMaker) {
	score := func(d DecisionMaker) int {
		s := 0
		if looksLikeRealPerson(d) {
			s += 50
		}
		if d.Email != "" {
			s += 20
		}
		if d.WhatsApp != "" || d.Phone != "" {
			s += 15
		}
		if d.LinkedIn != "" {
			s += 15
		}
		switch strings.ToLower(d.Confidence) {
		case "high":
			s += 10
		case "medium":
			s += 5
		}
		title := strings.ToLower(d.Title)
		for _, kw := range []string{"purchas", "procure", "buyer", "sourcing", "owner", "ceo", "founder", "direktur", "director", "import", "export"} {
			if strings.Contains(title, kw) {
				s += 8
				break
			}
		}
		return s
	}
	sort.SliceStable(makers, func(i, j int) bool {
		return score(makers[i]) > score(makers[j])
	})
}

// publicFacingNote 只保留对销售有用的一句话，去掉 OSINT/SpiderFoot 调试噪声。
func publicFacingNote(intel *PlaceIntel) string {
	named := countNamedPeople(intel.DecisionMakers)
	emails := len(intel.ExtraEmails)
	for _, d := range intel.DecisionMakers {
		if d.Email != "" {
			emails++
		}
	}
	phones := len(intel.Phones)
	li := 0
	if intel.Socials != nil && intel.Socials["linkedin"] != "" {
		li++
	}
	for _, d := range intel.DecisionMakers {
		if d.LinkedIn != "" {
			li++
		}
		if d.Phone != "" || d.WhatsApp != "" {
			phones++
		}
	}
	switch {
	case named > 0 && (emails > 0 || phones > 0 || li > 0):
		return fmt.Sprintf("已找到 %d 位可核验联系人，可直接邮件/WhatsApp/LinkedIn 触达。", named)
	case emails > 0 || phones > 0:
		return "暂无具名决策人，但已拿到公开邮箱/电话，可先用渠道邮箱触达。"
	case li > 0:
		return "已定位 LinkedIn 线索，建议先加决策人再建联。"
	default:
		return "公开源暂未挖到可核验决策人；可改深度模式或配置 HUNTER_API_KEY / AHU_PROXY。"
	}
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

	system := `You are a B2B export sales intelligence analyst (外贸获客). From public website/Google Maps text, extract ONLY evidenced facts useful for outreach:
1) One short company summary (2 sentences max: what they buy/sell, market, location).
2) Org units only if explicitly named on the page.
3) Decision makers / key contacts: prefer Purchasing Manager, Procurement, Buyer, Owner, Director, Founder, Direktur, Import/Export Manager. Include name, title, email, phone, linkedin ONLY if present in the text.
NEVER invent people. NEVER use email local-parts as names. If only a generic email exists, omit decision_makers.
Every decision_maker must include evidence (short quote or field name). Prefer empty arrays over guesses.
Return STRICT JSON only (no markdown):
{"summary":"...","org_structure":[{"name":"...","role":"...","parent":"...","evidence":"..."}],"decision_makers":[{"name":"...","title":"...","email":"...","phone":"...","linkedin":"...","source":"...","evidence":"...","confidence":"high|medium|low"}]}`

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
