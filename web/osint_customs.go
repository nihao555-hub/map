package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// TradeIntel 美国海关提单（ImportYeti / 同源开放数据）贸易背调。
// 外贸公式第一步「海关定公司」：证实是否真实进口、主要供应商、HS、近期提单。
type TradeIntel struct {
	Source         string          `json:"source,omitempty"` // importyeti | kirchner
	Role           string          `json:"role,omitempty"`   // importer | supplier
	Name           string          `json:"name,omitempty"`
	ProfileURL     string          `json:"profile_url,omitempty"`
	Website        string          `json:"website,omitempty"`
	Phone          string          `json:"phone,omitempty"`
	Address        string          `json:"address,omitempty"`
	Country        string          `json:"country,omitempty"`
	TotalShipments int             `json:"total_shipments,omitempty"`
	DateStart      string          `json:"date_start,omitempty"`
	DateEnd        string          `json:"date_end,omitempty"`
	TopSuppliers   []TradePartner  `json:"top_suppliers,omitempty"`
	TopHSCodes     []TradeHSCode   `json:"top_hs_codes,omitempty"`
	RecentBOLs     []TradeShipment `json:"recent_bols,omitempty"`
	Summary        string          `json:"summary,omitempty"`
}

// TradePartner 海关提单里的供应商/贸易伙伴。
type TradePartner struct {
	Name      string `json:"name"`
	Country   string `json:"country,omitempty"`
	Shipments int    `json:"shipments,omitempty"`
	Profile   string `json:"profile_url,omitempty"`
}

// TradeHSCode HS 编码汇总。
type TradeHSCode struct {
	Code        string `json:"code"`
	Description string `json:"description,omitempty"`
	Shipments   int    `json:"shipments,omitempty"`
}

// TradeShipment 近期提单摘要。
type TradeShipment struct {
	Date         string `json:"date,omitempty"`
	Shipper      string `json:"shipper,omitempty"`
	Consignee    string `json:"consignee,omitempty"`
	Product      string `json:"product,omitempty"`
	HSCode       string `json:"hs_code,omitempty"`
	Country      string `json:"country,omitempty"`
	BillOfLading string `json:"bill_of_lading,omitempty"`
}

var nonSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

func importYetiAPIKey() string {
	return strings.TrimSpace(os.Getenv("IMPORTYETI_API_KEY"))
}

func importYetiEnabled() bool {
	// 默认可试；设 IMPORTYETI_DISABLE=1 可关
	return strings.TrimSpace(os.Getenv("IMPORTYETI_DISABLE")) != "1"
}

// runCustomsEnrichment 海关定公司：ImportYeti 优先，Kirchner 美国提单开放 API 兜底。
func runCustomsEnrichment(ctx context.Context, intel *PlaceIntel, place Place, mu *sync.Mutex) {
	if !importYetiEnabled() && strings.TrimSpace(os.Getenv("KIRCHNER_DISABLE")) == "1" {
		return
	}
	budget, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()

	trade, err := lookupUSCustomsTrade(budget, place.Title, intel.Domain)
	if err != nil || trade == nil || trade.TotalShipments == 0 && trade.Name == "" {
		return
	}
	mu.Lock()
	applyTradeIntel(intel, trade)
	mu.Unlock()

	// 海关锁定公司法定名后，再用领英定采购/老板
	legal := firstNonEmpty(trade.Name, place.Title)
	people, coURL, _ := lookupLinkedInPeople(budget, legal, intel.Domain)
	if brand := companyBrandToken(legal); brand != "" && brand != legal {
		more, co2, _ := lookupLinkedInPeople(budget, brand, intel.Domain)
		people = mergeDecisionMakers(people, more)
		if coURL == "" {
			coURL = co2
		}
	}
	if len(people) == 0 && len(trade.TopSuppliers) > 0 {
		// 对活跃供应商品牌也补一刀搜人（外贸找上游/对手时常有用）
		for i, s := range trade.TopSuppliers {
			if i >= 2 || s.Name == "" || strings.EqualFold(s.Name, "Missing in source document") {
				continue
			}
			more, _, _ := lookupLinkedInPeople(budget, s.Name, "")
			people = mergeDecisionMakers(people, more)
		}
	}
	if len(people) == 0 && coURL == "" {
		return
	}
	mu.Lock()
	if len(people) > 0 {
		intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, people)
		intel.Sources = mergeUnique(intel.Sources, []string{"linkedin:customs"})
		intel.Provider = strings.Trim(intel.Provider+"+linkedin", "+")
	}
	if coURL != "" {
		if intel.Socials == nil {
			intel.Socials = map[string]string{}
		}
		if intel.Socials["linkedin"] == "" {
			intel.Socials["linkedin"] = coURL
		}
	}
	mu.Unlock()
}

func lookupUSCustomsTrade(ctx context.Context, title, domain string) (*TradeIntel, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("empty title")
	}
	if importYetiEnabled() {
		if t, err := lookupImportYeti(ctx, title, domain); err == nil && t != nil {
			return t, nil
		}
	}
	if strings.TrimSpace(os.Getenv("KIRCHNER_DISABLE")) == "1" {
		return nil, fmt.Errorf("customs unavailable")
	}
	return lookupKirchnerCompany(ctx, title)
}

func lookupImportYeti(ctx context.Context, title, domain string) (*TradeIntel, error) {
	slugs := discoverImportYetiSlugs(ctx, title, domain)
	var lastErr error
	for _, item := range slugs {
		t, err := fetchImportYetiProfile(ctx, item.role, item.slug)
		if err != nil {
			lastErr = err
			continue
		}
		if t != nil {
			return t, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("importyeti: no match for %q", title)
}

type iySlug struct {
	role string // company | supplier
	slug string
}

func discoverImportYetiSlugs(ctx context.Context, title, domain string) []iySlug {
	seen := map[string]bool{}
	var out []iySlug
	add := func(role, slug string) {
		slug = strings.Trim(strings.ToLower(slug), "-")
		if slug == "" || seen[role+"/"+slug] {
			return
		}
		seen[role+"/"+slug] = true
		out = append(out, iySlug{role: role, slug: slug})
	}

	// 1) DDG X-Ray：site:importyeti.com/company|supplier
	brand := companyBrandToken(title)
	queries := []string{
		fmt.Sprintf(`site:importyeti.com/company "%s"`, title),
		fmt.Sprintf(`site:importyeti.com/supplier "%s"`, title),
	}
	if brand != "" && !strings.EqualFold(brand, title) && IdentifiableCompanyName(brand) {
		queries = append(queries,
			fmt.Sprintf(`site:importyeti.com/company "%s"`, brand),
			fmt.Sprintf(`site:importyeti.com/supplier "%s"`, brand),
		)
	}
	for _, q := range queries {
		html, err := fetchDDGHTML(ctx, q)
		if err != nil || html == "" {
			continue
		}
		for _, m := range regexp.MustCompile(`(?i)importyeti\.com/(company|supplier)/([a-z0-9\-]+)`).FindAllStringSubmatch(html, -1) {
			if len(m) >= 3 {
				add(strings.ToLower(m[1]), m[2])
			}
		}
		for _, m := range regexp.MustCompile(`(?i)importyeti\.com%(?:2F|/)(company|supplier)%(?:2F|/)([a-z0-9\-]+)`).FindAllStringSubmatch(html, -1) {
			if len(m) >= 3 {
				add(strings.ToLower(m[1]), m[2])
			}
		}
	}

	// 2) 启发式 slug（美国进口商常用 company；海外厂常用 supplier）
	for _, cand := range importYetiSlugCandidates(title, domain, brand) {
		add("company", cand)
		add("supplier", cand)
	}
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func importYetiSlugCandidates(title, domain, brand string) []string {
	var raw []string
	raw = append(raw, title, brand)
	if domain != "" {
		raw = append(raw, strings.Split(domain, ".")[0])
	}
	var out []string
	seen := map[string]bool{}
	for _, r := range raw {
		s := slugifyImportYeti(r)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		// 常见后缀变体
		for _, suf := range []string{"-inc", "-llc", "-corp", "-co", "-usa", "-indonesia"} {
			if !strings.HasSuffix(s, suf) {
				v := s + suf
				if !seen[v] {
					seen[v] = true
					out = append(out, v)
				}
			}
		}
	}
	return out
}

func slugifyImportYeti(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`(?i)\b(pt\.?|cv\.?|tbk\.?|ltd\.?|limited|inc\.?|corp\.?|llc|gmbh|co\.?|company|group)\b`).ReplaceAllString(s, " ")
	s = nonSlugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	if len(s) < 2 {
		return ""
	}
	return s
}

func fetchImportYetiProfile(ctx context.Context, role, slug string) (*TradeIntel, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "supplier" {
		role = "company"
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, fmt.Errorf("empty slug")
	}
	u := fmt.Sprintf("https://data.importyeti.com/v1.0/%s/%s", role, url.PathEscape(slug))
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gmaps-intel/1.0 (+https://github.com/gosom/google-maps-scraper)")
	if key := importYetiAPIKey(); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("X-API-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, fmt.Errorf("importyeti auth/rate %d (set IMPORTYETI_API_KEY for higher quota)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("importyeti status %d", resp.StatusCode)
	}
	var parsed struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Data == nil {
		return nil, fmt.Errorf("importyeti json: %w", err)
	}
	return mapImportYetiData(parsed.Data, role, slug), nil
}

func mapImportYetiData(data map[string]any, role, slug string) *TradeIntel {
	t := &TradeIntel{
		Source:     "importyeti",
		Role:       role,
		Name:       asString(data["title"]),
		Website:    asString(data["website"]),
		Phone:      asString(data["phone_number"]),
		Address:    firstNonEmpty(asString(data["address_plain"]), asString(data["address"])),
		Country:    asString(data["country"]),
		ProfileURL: fmt.Sprintf("https://www.importyeti.com/%s/%s", role, slug),
	}
	t.TotalShipments = asInt(data["total_shipments"])
	if dr, ok := data["date_range"].(map[string]any); ok {
		t.DateStart = asString(dr["start_date"])
		t.DateEnd = asString(dr["end_date"])
	}
	// suppliers
	if arr, ok := data["suppliers_table"].([]any); ok {
		for _, item := range arr {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			name := asString(m["supplier_name"])
			if name == "" || strings.EqualFold(name, "Missing in source document") {
				continue
			}
			key := asString(m["key"])
			profile := ""
			if key != "" {
				profile = "https://www.importyeti.com" + key
				if !strings.HasPrefix(key, "/") {
					profile = "https://www.importyeti.com/supplier/" + slugifyImportYeti(name)
				}
			}
			t.TopSuppliers = append(t.TopSuppliers, TradePartner{
				Name:      name,
				Country:   firstNonEmpty(asString(m["country"]), asString(m["supplier_address_country"])),
				Shipments: asInt(m["shipments_12m"]),
				Profile:   profile,
			})
			if len(t.TopSuppliers) >= 8 {
				break
			}
		}
	}
	if arr, ok := data["hs_codes"].([]any); ok {
		for _, item := range arr {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			code := asString(m["hs_code"])
			if code == "" {
				continue
			}
			t.TopHSCodes = append(t.TopHSCodes, TradeHSCode{
				Code:        code,
				Description: asString(m["description"]),
				Shipments:   asInt(m["shipments_12m"]),
			})
			if len(t.TopHSCodes) >= 8 {
				break
			}
		}
	}
	if arr, ok := data["recent_bols"].([]any); ok {
		for _, item := range arr {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			t.RecentBOLs = append(t.RecentBOLs, TradeShipment{
				Date:         asString(m["date_formatted"]),
				Shipper:      asString(m["Shipper_Name"]),
				Consignee:    asString(m["Consignee_Name"]),
				Product:      truncateRunes(asString(m["Product_Description"]), 120),
				HSCode:       asString(m["HS_Code"]),
				Country:      asString(m["Country"]),
				BillOfLading: asString(m["Bill_of_Lading"]),
			})
			if len(t.RecentBOLs) >= 5 {
				break
			}
		}
	}
	// phones/emails buried in other_addresses_contact_info
	if arr, ok := data["other_addresses_contact_info"].([]any); ok {
		for _, item := range arr {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			ci, _ := m["contact_info_data"].(map[string]any)
			if ci == nil {
				continue
			}
			if t.Phone == "" {
				if phones, ok := ci["phone_numbers"].([]any); ok && len(phones) > 0 {
					t.Phone = asString(phones[0])
				}
			}
		}
	}
	t.Summary = formatTradeSummary(t)
	return t
}

func lookupKirchnerCompany(ctx context.Context, title string) (*TradeIntel, error) {
	if !IdentifiableCompanyName(title) {
		return nil, fmt.Errorf("kirchner: business name too generic to match")
	}
	names := []string{title}
	if b := companyBrandToken(title); b != "" && !strings.EqualFold(b, title) && IdentifiableCompanyName(b) {
		names = append(names, b, strings.ToUpper(b))
	}
	year := time.Now().UTC().Year()
	var lastErr error
	for _, name := range names {
		for _, yr := range []int{year, year - 1} {
			t, err := fetchKirchnerProfile(ctx, name, yr, yr)
			if err != nil {
				lastErr = err
				continue
			}
			if t == nil || (t.TotalShipments == 0 && len(t.TopHSCodes) == 0) {
				continue
			}
			// Kirchner matches on name prefixes, so a generic query can return a
			// different importer. Keep only profiles that echo the business name.
			if !ExternalRecordMatchesBusiness(t.Name, title) {
				lastErr = fmt.Errorf("kirchner: profile %q does not match %q", t.Name, title)
				continue
			}
			return t, nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("kirchner: no trade match")
}

func fetchKirchnerProfile(ctx context.Context, name string, yrFrom, yrTo int) (*TradeIntel, error) {
	body, _ := json.Marshal(map[string]any{
		"name": name, "yr_from": yrFrom, "yr_to": yrTo,
	})
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.kirchnerdata.com/api/company-profile", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gmaps-intel/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("kirchner status %d", resp.StatusCode)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if asString(parsed["name"]) == "" && asInt(parsed["total_shipments"]) == 0 {
		return nil, fmt.Errorf("kirchner empty")
	}
	t := &TradeIntel{
		Source:         "kirchner",
		Role:           "importer",
		Name:           asString(parsed["name"]),
		Address:        asString(parsed["address"]),
		Country:        asString(parsed["country"]),
		TotalShipments: asInt(parsed["total_shipments"]),
		DateStart:      fmt.Sprintf("%d", yrFrom),
		DateEnd:        fmt.Sprintf("%d", yrTo),
		ProfileURL:     "https://www.kirchnerdata.com/",
	}
	if arr, ok := parsed["top_products"].([]any); ok {
		for _, item := range arr {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			t.TopHSCodes = append(t.TopHSCodes, TradeHSCode{
				Code:      asString(m["hs_code"]),
				Shipments: asInt(m["count"]),
			})
			if len(t.TopHSCodes) >= 8 {
				break
			}
		}
	}
	if arr, ok := parsed["top_suppliers"].([]any); ok {
		for _, item := range arr {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			name := firstNonEmpty(asString(m["name"]), asString(m["supplier"]))
			if name == "" {
				name = asString(m["supplier_name"])
			}
			if name == "" {
				continue
			}
			t.TopSuppliers = append(t.TopSuppliers, TradePartner{
				Name:      name,
				Country:   firstNonEmpty(asString(m["country"]), asString(m["origin"])),
				Shipments: asInt(m["count"]),
			})
			if len(t.TopSuppliers) >= 8 {
				break
			}
		}
	}
	t.Summary = formatTradeSummary(t)
	return t, nil
}

func formatTradeSummary(t *TradeIntel) string {
	if t == nil {
		return ""
	}
	role := "美国海关进口记录"
	if t.Role == "supplier" {
		role = "美国海关供应商记录（对美出口提单）"
	}
	parts := []string{fmt.Sprintf("%s：%s", role, firstNonEmpty(t.Name, "未知主体"))}
	if t.TotalShipments > 0 {
		parts = append(parts, fmt.Sprintf("累计提单 %d", t.TotalShipments))
	}
	if t.DateStart != "" || t.DateEnd != "" {
		parts = append(parts, fmt.Sprintf("区间 %s–%s", t.DateStart, t.DateEnd))
	}
	if len(t.TopSuppliers) > 0 {
		parts = append(parts, "主要伙伴 "+t.TopSuppliers[0].Name)
	}
	if len(t.TopHSCodes) > 0 {
		parts = append(parts, "HS "+t.TopHSCodes[0].Code)
	}
	parts = append(parts, "来源 "+t.Source)
	return strings.Join(parts, " · ")
}

func applyTradeIntel(intel *PlaceIntel, trade *TradeIntel) {
	if intel == nil || trade == nil {
		return
	}
	intel.Trade = trade
	src := "importyeti"
	if trade.Source == "kirchner" {
		src = "kirchner:us-bol"
	}
	intel.Sources = mergeUnique(intel.Sources, []string{src})
	if trade.ProfileURL != "" {
		intel.Sources = mergeUnique(intel.Sources, []string{trade.ProfileURL})
	}
	intel.Provider = strings.Trim(intel.Provider+"+"+trade.Source, "+")
	if trade.Phone != "" {
		intel.Phones = mergeUnique(intel.Phones, []string{trade.Phone})
	}
	if trade.Website != "" && intel.Website == "" {
		intel.Website = trade.Website
	}
	// 贸易伙伴写入架构（真实供应链，不是假部门）；最多 3 家避免刷屏
	nSup := 0
	for _, s := range trade.TopSuppliers {
		if s.Name == "" {
			continue
		}
		if nSup >= 3 {
			break
		}
		intel.OrgStructure = append(intel.OrgStructure, OrgUnit{
			Name:     s.Name,
			Role:     "customs supplier",
			Parent:   firstNonEmpty(trade.Name, intel.Title),
			Evidence: fmt.Sprintf("US BOL via %s (%d shipments)", trade.Source, s.Shipments),
		})
		nSup++
	}
	if len(intel.OrgStructure) > 10 {
		intel.OrgStructure = intel.OrgStructure[:10]
	}
	if trade.Summary != "" {
		if intel.Summary == "" {
			intel.Summary = trade.Summary
		} else if !strings.Contains(intel.Summary, "海关") && !strings.Contains(intel.Summary, "提单") {
			intel.Summary = trade.Summary + " " + intel.Summary
		}
	}
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case json.Number:
		return t.String()
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func asInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	case string:
		var n int
		fmt.Sscanf(strings.TrimSpace(t), "%d", &n)
		return n
	default:
		return 0
	}
}
