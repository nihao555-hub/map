package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	customsDefaultLimit = 50
	customsMaxLimit     = 80
	customsMinYear      = 2015
)

// searchCustoms finds US importers from public bills of lading, and supplements
// with UN Comtrade official trade values plus USITC HS lookup (no API key).
func (c *Client) searchCustoms(ctx context.Context, q Query) (Result, error) {
	if c == nil {
		return Result{Note: customsPolicyNote}, nil
	}

	role := NormalizeRole(q.Role)
	term, match := customsSearchTerm(q.Keyword, q.Country)
	if role == RoleBuyer && !customsCountryIsUS(q.Country) && strings.TrimSpace(q.Country) != "" {
		return Result{
			Note:     "逐票买家目前来自美国海关公开提单。其他国家没有同等公开企业名录。",
			Expanded: []string{term},
		}, nil
	}
	year := customsYear(q.Year)
	limit := q.Limit
	if limit <= 0 {
		limit = customsDefaultLimit
	}
	if limit > customsMaxLimit {
		limit = customsMaxLimit
	}

	hs4 := hs4Digits(term)
	if hs4 == "" {
		hs4 = c.lookupHS4(ctx, term)
	}
	if looksLikeHS(term) {
		match = "hs_code"
	}

	var (
		hits    []Hit
		sources []string
		notes   []string
	)

	if strings.TrimSpace(c.CustomsBaseURL) != "" {
		payload, err := c.fetchLeadFinder(ctx, term, match, year, limit, false)
		if err != nil && year == time.Now().UTC().Year() {
			payload, err = c.fetchLeadFinder(ctx, term, match, year-1, limit, false)
			year = year - 1
		}
		if err != nil {
			if role != RoleSeller {
				hits = c.customsProfileFallback(ctx, q.Keyword, term, year)
				if len(hits) > 0 {
					sources = append(sources, "kirchner")
				}
			}
			if len(hits) == 0 {
				notes = append(notes, "公开提单企业名单接口暂时不可用。逐票企业来自美国海关公开提单，不是全球企业库。")
			}
		} else {
			if len(payload.Profiles) == 0 && len(payload.Importers) > 0 {
				payload.Profiles = c.fetchImporterProfiles(ctx, payload.Importers, year, 12)
			}
			if role == RoleSeller {
				hits = customsSellerHits(payload, q.Country, year)
			} else {
				hits = customsBuyerHits(payload, q.Country, year)
			}
			if len(hits) > 0 {
				sources = append(sources, "kirchner")
			}
		}
	}

	if partners, err := c.searchComtradeOrigins(ctx, hs4, year, q.Country); err == nil && len(partners) > 0 {
		if n := comtradeNote(partners, hs4); n != "" {
			notes = append(notes, n)
		}
		sources = append(sources, "comtrade")
		if role == RoleSeller && len(hits) == 0 {
			hits = comtradeSellerHits(partners, hs4, year)
		}
	}

	note := strings.Join(notes, " ")
	if note == "" {
		note = customsPolicyNote
	}
	expanded := []string{term, strconv.Itoa(year)}
	if hs4 != "" {
		expanded = append(expanded, "HS"+hs4)
	}

	return Result{
		Hits:     clipHits(hits, limit),
		Sources:  uniqueStrings(sources),
		Note:     note,
		Expanded: expanded,
	}, nil
}

// CustomsProfile is one importer's public bill-of-lading summary for the right pane.
type CustomsProfile struct {
	Name            string            `json:"name"`
	Role            string            `json:"role,omitempty"`
	Country         string            `json:"country,omitempty"`
	Address         string            `json:"address,omitempty"`
	YearFrom        int               `json:"year_from,omitempty"`
	YearTo          int               `json:"year_to,omitempty"`
	TotalShipments  int               `json:"total_shipments,omitempty"`
	UniqueSuppliers int               `json:"unique_suppliers,omitempty"`
	HomepageURL     string            `json:"homepage_url,omitempty"`
	Suppliers       []CustomsPartner  `json:"suppliers,omitempty"`
	Carriers        []CustomsPartner  `json:"carriers,omitempty"`
	Origins         []CustomsPartner  `json:"origins,omitempty"`
	Products        []CustomsProduct  `json:"products,omitempty"`
	Shipments       []CustomsShipment `json:"shipments,omitempty"`
	Note            string            `json:"note,omitempty"`
}

// CustomsPartner is a supplier, carrier, or origin country.
type CustomsPartner struct {
	Name      string `json:"name"`
	Country   string `json:"country,omitempty"`
	Shipments int    `json:"shipments,omitempty"`
}

// CustomsProduct is an HS code or product term.
type CustomsProduct struct {
	Code      string `json:"code"`
	Shipments int    `json:"shipments,omitempty"`
}

// CustomsShipment is one recent bill of lading.
type CustomsShipment struct {
	Date      string `json:"date,omitempty"`
	Shipper   string `json:"shipper,omitempty"`
	Consignee string `json:"consignee,omitempty"`
	Product   string `json:"product,omitempty"`
	HSCode    string `json:"hs_code,omitempty"`
	Country   string `json:"country,omitempty"`
	Vessel    string `json:"vessel,omitempty"`
}

// LookupCustomsProfile loads one US importer from Kirchner.
func (c *Client) LookupCustomsProfile(ctx context.Context, name string, year int) (CustomsProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CustomsProfile{}, fmt.Errorf("请输入企业名称")
	}
	if c == nil || strings.TrimSpace(c.CustomsBaseURL) == "" {
		return CustomsProfile{}, fmt.Errorf("海关数据源未配置")
	}
	year = customsYear(year)
	raw, err := c.postJSON(ctx, strings.TrimRight(c.CustomsBaseURL, "/")+"/api/company-profile", map[string]any{
		"name": name, "yr_from": year, "yr_to": year,
	}, kirchnerHeaders())
	if err != nil {
		return CustomsProfile{}, err
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return CustomsProfile{}, err
	}
	prof := CustomsProfile{
		Name:            firstNonEmpty(asString(parsed["name"]), name),
		Role:            RoleBuyer,
		Country:         "美国",
		Address:         asString(parsed["address"]),
		YearFrom:        jsonInt(parsed["from_year"]),
		YearTo:          jsonInt(parsed["to_year"]),
		TotalShipments:  jsonInt(parsed["total_shipments"]),
		UniqueSuppliers: jsonInt(parsed["unique_suppliers"]),
		HomepageURL:     absoluteKirchnerURL(c, asString(parsed["profile_url"])),
		Suppliers:       mapCustomsPartners(parsed["top_suppliers"], 12),
		Carriers:        mapCustomsPartners(parsed["top_carriers"], 8),
		Origins:         mapCustomsOrigins(parsed["top_origin_countries"], 8),
		Products:        append(mapCustomsProducts(parsed["top_products"], 8), mapCustomsProducts(parsed["top_product_terms"], 8)...),
		Shipments:       mapCustomsShipments(parsed["latest_shipments"], 8),
		Note:            customsPolicyNote,
	}
	if prof.YearFrom == 0 {
		prof.YearFrom = year
	}
	if prof.YearTo == 0 {
		prof.YearTo = year
	}
	return prof, nil
}

type leadFinderPayload struct {
	Year      int              `json:"year"`
	Importers []leadFinderRow  `json:"importers"`
	Profiles  []map[string]any `json:"profiles"`
}

type leadFinderRow struct {
	Name              string  `json:"name"`
	TotalShipments    int     `json:"total_shipments"`
	MatchingShipments int     `json:"matching_shipments"`
	FocusPct          float64 `json:"focus_pct"`
	MatchWeightKg     float64 `json:"match_weight_total_kg"`
	ProfileURL        string  `json:"profile_url"`
	APIProfileURL     string  `json:"api_profile_url"`
}

func (c *Client) fetchLeadFinder(ctx context.Context, term, match string, year, limit int, profiles bool) (leadFinderPayload, error) {
	u, err := url.Parse(strings.TrimRight(c.CustomsBaseURL, "/") + "/api/lead-finder")
	if err != nil {
		return leadFinderPayload{}, err
	}
	q := u.Query()
	q.Set("keywords", term)
	q.Set("match", match)
	q.Set("year", strconv.Itoa(year))
	q.Set("month", "full")
	q.Set("limit", strconv.Itoa(limit))
	if profiles {
		q.Set("include_profiles", "1")
	}
	u.RawQuery = q.Encode()

	raw, err := c.get(ctx, u.String(), kirchnerHeaders())
	if err != nil {
		return leadFinderPayload{}, err
	}
	var payload leadFinderPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return leadFinderPayload{}, err
	}
	if payload.Year == 0 {
		payload.Year = year
	}
	return payload, nil
}

func customsBuyerHits(payload leadFinderPayload, country string, year int) []Hit {
	if !customsCountryIsUS(country) && strings.TrimSpace(country) != "" {
		return nil
	}
	profiles := indexCustomsProfiles(payload.Profiles)
	out := make([]Hit, 0, len(payload.Importers))
	for _, row := range payload.Importers {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			continue
		}
		home := strings.TrimSpace(row.ProfileURL)
		extra := map[string]string{
			"shipments": strconv.Itoa(row.TotalShipments),
			"matching":  strconv.Itoa(row.MatchingShipments),
			"focus":     strconv.FormatFloat(row.FocusPct, 'f', 0, 64),
			"year":      strconv.Itoa(year),
		}
		if row.MatchWeightKg > 0 {
			extra["weight_kg"] = strconv.FormatFloat(row.MatchWeightKg, 'f', 0, 64)
		}
		if p := profiles[strings.ToLower(name)]; p != nil {
			product, hs := topProductExtra(p["top_products"])
			if term, _ := topProductExtra(p["top_product_terms"]); term != "" {
				product = term
			}
			if product != "" {
				extra["product"] = product
			}
			if hs != "" {
				extra["hs"] = hs
			}
			if extra["address"] == "" {
				extra["address"] = asString(p["address"])
			}
			ships := mapCustomsShipments(p["latest_shipments"], 1)
			if len(ships) > 0 && ships[0].Date != "" {
				extra["last_date"] = ships[0].Date
				if extra["product"] == "" {
					extra["product"] = ships[0].Product
				}
				if extra["hs"] == "" {
					extra["hs"] = ships[0].HSCode
				}
			}
		}
		hit := Hit{
			ID:           "customs:" + strings.ToLower(name),
			Kind:         KindCustoms,
			Platform:     PlatformCustoms,
			Name:         name,
			Title:        name,
			Role:         RoleBuyer,
			Country:      "US",
			CountryLabel: "美国",
			HomepageURL:  home,
			MessageURL:   home,
			Score:        80 + row.MatchingShipments,
			Snippet:      fmt.Sprintf("匹配提单 %d · 全部 %d · 专注度 %.0f%%", row.MatchingShipments, row.TotalShipments, row.FocusPct),
			Extra:        extra,
		}
		out = append(out, hit)
	}
	return out
}

func indexCustomsProfiles(profiles []map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(profiles))
	for _, p := range profiles {
		name := strings.ToLower(strings.TrimSpace(asString(p["name"])))
		if name == "" {
			continue
		}
		out[name] = p
	}
	return out
}

func topProductExtra(raw any) (product, hs string) {
	arr, _ := raw.([]any)
	if len(arr) == 0 {
		return "", ""
	}
	m, _ := arr[0].(map[string]any)
	if m == nil {
		return "", ""
	}
	product = firstNonEmpty(asString(m["term"]), asString(m["product"]), asString(m["description"]))
	hs = firstNonEmpty(asString(m["hs_code"]), asString(m["code"]))
	if product == "" {
		product = hs
	}
	return product, hs
}

func customsSellerHits(payload leadFinderPayload, country string, year int) []Hit {
	seen := map[string]Hit{}
	order := make([]string, 0)
	add := func(name, origin string, shipments int) {
		name = strings.TrimSpace(name)
		if name == "" || strings.EqualFold(name, "n/a") || strings.EqualFold(name, "missing in source document") {
			return
		}
		if !customsCountryMatches(origin, country) {
			return
		}
		key := strings.ToLower(name)
		if prev, ok := seen[key]; ok {
			if shipments > jsonInt(prev.Extra["shipments"]) {
				prev.Extra["shipments"] = strconv.Itoa(shipments)
				seen[key] = prev
			}
			return
		}
		code, label := inferCountryFromText(origin, country)
		hit := Hit{
			ID:           "customs-seller:" + key,
			Kind:         KindCustoms,
			Platform:     PlatformCustoms,
			Name:         name,
			Title:        name,
			Role:         RoleSeller,
			Country:      code,
			CountryLabel: firstNonEmpty(label, origin),
			Score:        70 + shipments,
			Snippet:      strings.TrimSpace("美国进口提单中的发货人 / 供应商。 " + origin),
			Extra: map[string]string{
				"shipments": strconv.Itoa(shipments),
				"origin":    origin,
				"year":      strconv.Itoa(year),
			},
		}
		seen[key] = hit
		order = append(order, key)
	}

	if len(payload.Profiles) > 0 {
		for _, prof := range payload.Profiles {
			for _, p := range mapCustomsPartners(prof["top_suppliers"], 12) {
				add(p.Name, p.Country, p.Shipments)
			}
			for _, p := range mapCustomsOrigins(prof["top_origin_countries"], 4) {
				_ = p
			}
		}
	}

	out := make([]Hit, 0, len(order))
	for _, key := range order {
		out = append(out, seen[key])
	}
	return out
}

func customsSearchTerm(keyword, country string) (term, match string) {
	keyword = strings.TrimSpace(keyword)
	if looksLikeHS(keyword) {
		return strings.ReplaceAll(strings.ReplaceAll(keyword, ".", ""), " ", ""), "hs_code"
	}
	for _, t := range LocalSearchTerms(keyword, firstNonEmpty(country, "US")) {
		if t != "" && !hasCJK(t) {
			return t, "product_desc"
		}
	}
	return keyword, "product_desc"
}

func looksLikeHS(s string) bool {
	n := 0
	for _, r := range s {
		if r == '.' || r == ' ' {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
		n++
	}
	return n >= 4 && n <= 10
}

func customsYear(year int) int {
	now := time.Now().UTC().Year()
	if year >= customsMinYear && year <= now+1 {
		return year
	}
	if time.Now().UTC().Month() <= 2 {
		return now - 1
	}
	return now
}

func customsCountryIsUS(code string) bool {
	c := strings.ToUpper(strings.TrimSpace(code))
	return c == "" || c == "US" || c == "USA" || c == "UNITED STATES"
}

func customsCountryMatches(origin, selected string) bool {
	selected = strings.ToUpper(strings.TrimSpace(selected))
	if selected == "" {
		return true
	}
	info := LookupCountry(selected)
	blob := strings.ToUpper(origin + " " + info.Code + " " + info.Label + " " + info.Query)
	origin = strings.ToUpper(strings.TrimSpace(origin))
	if origin == "" {
		return false
	}
	if strings.Contains(origin, selected) {
		return true
	}
	if info.Query != "" && strings.Contains(origin, strings.ToUpper(info.Query)) {
		return true
	}
	if info.Label != "" && strings.Contains(blob, strings.ToUpper(info.Label)) {
		return strings.Contains(origin, strings.ToUpper(info.Query)) || strings.Contains(origin, selected)
	}
	return false
}

func inferCountryFromText(origin, selected string) (code, label string) {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return selected, LookupCountry(selected).Label
	}
	up := strings.ToUpper(origin)
	bestLen := 0
	var best CountryInfo
	for _, c := range SearchCountries {
		if c.Code == "" {
			continue
		}
		for _, cand := range []string{c.Query, c.Label, c.Code} {
			cand = strings.ToUpper(strings.TrimSpace(cand))
			if cand == "" {
				continue
			}
			matched := up == cand || strings.Contains(up, cand)
			if !matched {
				continue
			}
			if len(cand) == 2 && up != cand && !isoTokenEqual(up, cand) {
				continue
			}
			if len(cand) >= bestLen {
				bestLen = len(cand)
				best = c
			}
		}
	}
	if best.Code != "" {
		return best.Code, best.Label
	}

	return selected, firstNonEmpty(origin, LookupCountry(selected).Label)
}

func isoTokenEqual(up, code string) bool {
	for _, part := range strings.FieldsFunc(up, func(r rune) bool {
		return r == ' ' || r == ',' || r == '/' || r == '-' || r == '|'
	}) {
		if part == code {
			return true
		}
	}

	return false
}

func clipHits(hits []Hit, limit int) []Hit {
	if limit > 0 && len(hits) > limit {
		return hits[:limit]
	}
	return hits
}

func jsonInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func mapCustomsPartners(raw any, limit int) []CustomsPartner {
	arr, _ := raw.([]any)
	out := make([]CustomsPartner, 0, len(arr))
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		name := firstNonEmpty(asString(m["name"]), asString(m["supplier"]))
		if name == "" {
			continue
		}
		out = append(out, CustomsPartner{
			Name:      name,
			Country:   firstNonEmpty(asString(m["country"]), asString(m["origin"])),
			Shipments: jsonInt(m["count"]) + jsonInt(m["shipments"]),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func mapCustomsOrigins(raw any, limit int) []CustomsPartner {
	arr, _ := raw.([]any)
	out := make([]CustomsPartner, 0, len(arr))
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		name := firstNonEmpty(asString(m["country"]), asString(m["name"]))
		if name == "" {
			continue
		}
		out = append(out, CustomsPartner{Name: name, Country: name, Shipments: jsonInt(m["count"]) + jsonInt(m["shipments"])})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func mapCustomsProducts(raw any, limit int) []CustomsProduct {
	arr, _ := raw.([]any)
	out := make([]CustomsProduct, 0, len(arr))
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		code := firstNonEmpty(asString(m["hs_code"]), asString(m["code"]), asString(m["term"]))
		if code == "" {
			continue
		}
		out = append(out, CustomsProduct{Code: code, Shipments: jsonInt(m["count"]) + jsonInt(m["shipments"])})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func mapCustomsShipments(raw any, limit int) []CustomsShipment {
	arr, _ := raw.([]any)
	out := make([]CustomsShipment, 0, len(arr))
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		out = append(out, CustomsShipment{
			Date:      formatKirchnerDate(firstNonEmpty(asString(m["date"]), asString(m["actual_arrival_date"]))),
			Shipper:   firstNonEmpty(asString(m["shipper"]), asString(m["shipper_name"]), asString(m["exporter"])),
			Consignee: firstNonEmpty(asString(m["consignee"]), asString(m["consignee_name"]), asString(m["importer"])),
			Product:   stripMarkup(firstNonEmpty(asString(m["product"]), asString(m["product_desc"]), asString(m["description"]))),
			HSCode:    firstNonEmpty(asString(m["hs_code"]), asString(m["hs"])),
			Country:   firstNonEmpty(asString(m["country"]), asString(m["origin"])),
			Vessel:    firstNonEmpty(asString(m["vessel"]), asString(m["vessel_name"])),
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func kirchnerHeaders() map[string]string {
	return map[string]string{
		"User-Agent": browserUA,
		"Accept":     "application/json",
	}
}

func formatKirchnerDate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 8 {
		ok := true
		for _, r := range s {
			if r < '0' || r > '9' {
				ok = false
				break
			}
		}
		if ok {
			return s[:4] + "-" + s[4:6] + "-" + s[6:8]
		}
	}
	if len(s) >= 10 && s[4] == '-' {
		return s[:10]
	}
	return s
}

func stripMarkup(s string) string {
	s = strings.ReplaceAll(s, "<br/>", " ")
	s = strings.ReplaceAll(s, "<br />", " ")
	s = strings.ReplaceAll(s, "<br>", " ")
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (c *Client) customsProfileFallback(ctx context.Context, keyword, term string, year int) []Hit {
	for _, name := range uniqueFoldedStrings([]string{strings.TrimSpace(keyword), strings.TrimSpace(term)}) {
		if name == "" || looksLikeHS(name) || !looksLikeCompanyName(name) {
			continue
		}
		prof, err := c.LookupCustomsProfile(ctx, name, year)
		if err != nil || strings.TrimSpace(prof.Name) == "" || prof.TotalShipments == 0 {
			continue
		}
		return []Hit{hitFromCustomsProfile(prof, year)}
	}
	return nil
}

func looksLikeCompanyName(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	up := strings.ToUpper(s)
	for _, tok := range []string{
		" INC", " INC.", " LLC", " LTD", " LTD.", " CORP", " CORP.", " GMBH",
		" CO.", " COMPANY", " LIMITED", " GROUP", " HOLDINGS", " INDUSTRIES",
		" INTERNATIONAL", " DISTRIBUTION", " TRADING", " IMPORTS", " IMPORT ",
		" EXPORT", " LOGISTICS", " ENTERPRISE", " FACTORY", " MANUFACTUR",
		"公司", "有限", "集团", "实业", "贸易",
	} {
		if strings.Contains(up, strings.ToUpper(tok)) || strings.Contains(s, tok) {
			return true
		}
	}
	for _, f := range strings.Fields(up) {
		switch strings.Trim(f, ".,") {
		case "INC", "LLC", "LTD", "CORP", "GMBH":
			return true
		}
	}
	return false
}

func hitFromCustomsProfile(prof CustomsProfile, year int) Hit {
	extra := map[string]string{
		"shipments": strconv.Itoa(prof.TotalShipments),
		"matching":  strconv.Itoa(prof.TotalShipments),
		"year":      strconv.Itoa(year),
		"address":   prof.Address,
	}
	for _, p := range prof.Products {
		if extra["hs"] == "" && looksLikeHS(p.Code) {
			extra["hs"] = p.Code
		}
		if extra["product"] == "" && p.Code != "" && !looksLikeHS(p.Code) {
			extra["product"] = p.Code
		}
	}
	if extra["product"] == "" && len(prof.Products) > 0 {
		extra["product"] = prof.Products[0].Code
	}
	if len(prof.Shipments) > 0 {
		if prof.Shipments[0].Date != "" {
			extra["last_date"] = prof.Shipments[0].Date
		}
		if extra["product"] == "" {
			extra["product"] = prof.Shipments[0].Product
		}
		if extra["hs"] == "" {
			extra["hs"] = prof.Shipments[0].HSCode
		}
	}
	return Hit{
		ID:           "customs:" + strings.ToLower(prof.Name),
		Kind:         KindCustoms,
		Platform:     PlatformCustoms,
		Name:         prof.Name,
		Title:        prof.Name,
		Role:         RoleBuyer,
		Country:      "US",
		CountryLabel: "美国",
		HomepageURL:  prof.HomepageURL,
		MessageURL:   prof.HomepageURL,
		Score:        80 + prof.TotalShipments,
		Snippet:      fmt.Sprintf("公开提单 %d · 供应商 %d", prof.TotalShipments, prof.UniqueSuppliers),
		Extra:        extra,
	}
}

func (c *Client) fetchImporterProfiles(ctx context.Context, rows []leadFinderRow, year, max int) []map[string]any {
	if max <= 0 || max > 12 {
		max = 12
	}
	if len(rows) < max {
		max = len(rows)
	}
	out := make([]map[string]any, 0, max)
	for i := 0; i < max; i++ {
		if ctx.Err() != nil {
			break
		}
		row := rows[i]
		u := absoluteKirchnerURL(c, row.APIProfileURL)
		if parsed, err := url.Parse(u); err != nil || parsed.Host == "" || (c != nil && !sameHTTPHost(c.CustomsBaseURL, u)) {
			u = ""
		}
		if u == "" && c != nil && row.Name != "" {
			u = strings.TrimRight(c.CustomsBaseURL, "/") + "/api/company-profile/" + url.PathEscape(row.Name) + "/" + strconv.Itoa(year) + "/" + strconv.Itoa(year)
		}
		if u == "" {
			continue
		}
		raw, err := c.get(ctx, u, kirchnerHeaders())
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil || asString(m["name"]) == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

func absoluteKirchnerURL(c *Client, u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	base := defaultKirchnerURL
	if c != nil && c.CustomsBaseURL != "" {
		base = strings.TrimRight(c.CustomsBaseURL, "/")
	}
	if strings.HasPrefix(u, "/") {
		return base + u
	}
	return base + "/" + u
}

func sameHTTPHost(base, raw string) bool {
	a, err1 := url.Parse(base)
	b, err2 := url.Parse(raw)
	if err1 != nil || err2 != nil || a.Host == "" || b.Host == "" {
		return false
	}
	return strings.EqualFold(a.Host, b.Host)
}
