package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const (
	defaultImportYetiURL    = "https://www.importyeti.com"
	defaultImportYetiAPIURL = "https://data.importyeti.com"
	importYetiMaxPages      = 5
	importYetiPageSize      = 10
)

var importYetiPathRe = regexp.MustCompile(`(?i)(?:https?://(?:www\.)?importyeti\.com)?/(company|supplier)/([a-z0-9][a-z0-9-]*)`)

type importYetiRow struct {
	Name         string
	Type         string
	Address      string
	CountryCode  string
	Country      string
	URL          string
	Shipments    int
	MostRecent   string
	TopSuppliers []string
	TopCustomers []string
	Trademarks   []string
}

func (c *Client) importYetiBase() string {
	if c == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(c.ImportYetiURL), "/")
}

func (c *Client) importYetiAPIBase() string {
	if c == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(c.ImportYetiAPIURL), "/")
}

func (c *Client) searchImportYeti(ctx context.Context, term, role, country string, limit int) ([]Hit, string, error) {
	if c == nil || strings.TrimSpace(term) == "" {
		return nil, "", nil
	}
	if limit <= 0 {
		limit = customsDefaultLimit
	}

	var (
		rows []importYetiRow
		src  string
		err  error
	)
	if strings.TrimSpace(c.ImportYetiAPIKey) != "" && c.importYetiAPIBase() != "" {
		rows, err = c.searchImportYetiOfficial(ctx, term, role, limit)
		if err == nil && len(rows) > 0 {
			src = "importyeti-api"
		}
	}
	if len(rows) == 0 && c.importYetiBase() != "" {
		rows, err = c.searchImportYetiPublic(ctx, term, limit)
		if err == nil && len(rows) > 0 {
			src = "importyeti"
		}
	}
	if len(rows) == 0 && c != nil && !c.DisablePublic {
		webHits, webSrc, webErr := c.searchImportYetiWeb(ctx, term, role, country, limit)
		if webErr == nil && len(webHits) > 0 {
			return webHits, webSrc, nil
		}
		if err == nil {
			err = webErr
		}
	}
	if len(rows) == 0 {
		return nil, "", err
	}

	hits := importYetiHits(rows, role, country, limit)
	if len(hits) == 0 {
		return nil, src, nil
	}
	return hits, src, nil
}

func (c *Client) searchImportYetiPublic(ctx context.Context, term string, limit int) ([]importYetiRow, error) {
	base := c.importYetiBase()
	if base == "" {
		return nil, nil
	}
	need := limit
	if need <= 0 {
		need = customsDefaultLimit
	}
	pages := (need + importYetiPageSize - 1) / importYetiPageSize
	if pages < 1 {
		pages = 1
	}
	if pages > importYetiMaxPages {
		pages = importYetiMaxPages
	}

	var out []importYetiRow
	seen := map[string]struct{}{}
	var lastErr error
	for page := 1; page <= pages; page++ {
		u, err := url.Parse(base + "/api/search")
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("q", term)
		q.Set("page", strconv.Itoa(page))
		u.RawQuery = q.Encode()

		raw, err := c.getImportYeti(ctx, u.String())
		if err != nil {
			lastErr = err
			break
		}
		batch := parseImportYetiSearch(raw, base)
		if len(batch) == 0 {
			break
		}
		added := 0
		for _, row := range batch {
			key := strings.ToLower(row.Type + ":" + row.Name + ":" + row.URL)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, row)
			added++
		}
		if added == 0 {
			break
		}
		if len(out) >= need {
			break
		}
	}
	if len(out) == 0 {
		return nil, lastErr
	}
	return out, nil
}

func (c *Client) searchImportYetiOfficial(ctx context.Context, term, role string, limit int) ([]importYetiRow, error) {
	base := c.importYetiAPIBase()
	if base == "" || strings.TrimSpace(c.ImportYetiAPIKey) == "" {
		return nil, nil
	}
	kind := "company"
	if role == RoleSeller {
		kind = "supplier"
	}
	u, err := url.Parse(base + "/v1.0/" + kind + "/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", term)
	q.Set("query", term)
	u.RawQuery = q.Encode()

	raw, err := c.getImportYetiAPI(ctx, u.String())
	if err != nil {
		return nil, err
	}
	rows := parseImportYetiSearch(raw, defaultImportYetiURL)
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (c *Client) lookupImportYetiProfile(ctx context.Context, name, pageURL string) (CustomsProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" && strings.TrimSpace(pageURL) == "" {
		return CustomsProfile{}, fmt.Errorf("请输入企业名称")
	}

	if slugKind, slug := importYetiSlug(pageURL); slug != "" && strings.TrimSpace(c.ImportYetiAPIKey) != "" && c.importYetiAPIBase() != "" {
		if prof, err := c.fetchImportYetiOfficialProfile(ctx, slugKind, slug, name); err == nil && strings.TrimSpace(prof.Name) != "" {
			return prof, nil
		}
	}

	term := firstNonEmpty(name, importYetiNameFromURL(pageURL))
	rows, err := c.searchImportYetiPublic(ctx, term, importYetiPageSize)
	if len(rows) == 0 && strings.TrimSpace(c.ImportYetiAPIKey) != "" {
		rows, err = c.searchImportYetiOfficial(ctx, term, RoleBuyer, importYetiPageSize)
	}
	if len(rows) == 0 {
		if err != nil {
			return CustomsProfile{}, err
		}
		return CustomsProfile{}, fmt.Errorf("ImportYeti 没有这条企业档案")
	}
	row, ok := pickImportYetiRow(rows, name, pageURL)
	if !ok {
		return CustomsProfile{}, fmt.Errorf("ImportYeti 没有这条企业档案")
	}
	return profileFromImportYeti(row), nil
}

func (c *Client) fetchImportYetiOfficialProfile(ctx context.Context, kind, slug, fallback string) (CustomsProfile, error) {
	if kind == "" {
		kind = "company"
	}
	raw, err := c.getImportYetiAPI(ctx, c.importYetiAPIBase()+"/v1.0/"+kind+"/"+url.PathEscape(slug))
	if err != nil {
		return CustomsProfile{}, err
	}
	rows := parseImportYetiSearch(raw, defaultImportYetiURL)
	if len(rows) == 1 {
		if rows[0].Name == "" {
			rows[0].Name = fallback
		}
		return profileFromImportYeti(rows[0]), nil
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return CustomsProfile{}, err
	}
	data, _ := doc["data"].(map[string]any)
	if data == nil {
		data = doc
	}
	row := importYetiRowFromMap(data, defaultImportYetiURL)
	if row.Name == "" {
		row.Name = firstNonEmpty(asString(data["title"]), fallback)
	}
	if row.URL == "" {
		row.URL = defaultImportYetiURL + "/" + kind + "/" + slug
	}
	if row.Type == "" {
		row.Type = kind
	}
	prof := profileFromImportYeti(row)
	prof.Shipments = mapImportYetiOfficialBols(data["recent_bols"], 8)
	if len(prof.Shipments) > 0 && prof.Shipments[0].Date != "" && row.MostRecent == "" {
		// keep
	}
	return prof, nil
}

func (c *Client) searchImportYetiWeb(ctx context.Context, term, role, country string, limit int) ([]Hit, string, error) {
	if NormalizeRole(role) == RoleSeller {
		return nil, "", nil
	}
	path := "company"
	query := "site:importyeti.com/" + path + " " + strings.TrimSpace(term)
	batch, src, err := c.searchOneIndexExtract(ctx, query, extractImportYetiResults, "")
	if err != nil {
		return nil, "", err
	}
	var names []string
	out := make([]Hit, 0, len(batch))
	for _, hit := range batch {
		kind, slug := importYetiSlug(hit.HomepageURL)
		if slug == "" {
			continue
		}
		rowType := kind
		if rowType == "" {
			rowType = "company"
		}
		if rowType != "company" {
			continue
		}
		name := firstNonEmpty(cleanImportYetiTitle(hit.Name), importYetiNameFromSlug(slug))
		if name == "" {
			continue
		}
		if looksLikeCompanyName(term) && !companyTokensMatch(term, name+" "+slug) {
			continue
		}
		row := importYetiRow{
			Name: name,
			Type: rowType,
			URL:  "https://www.importyeti.com/" + rowType + "/" + slug,
		}
		converted := importYetiHits([]importYetiRow{row}, role, country, 1)
		if len(converted) == 0 {
			continue
		}
		h := converted[0]
		if jsonInt(h.Extra["shipments"]) > 0 {
			out = append(out, h)
			continue
		}
		names = append(names, name, brandToConsignee(name))
	}
	hydrated := c.hydrateCustomsNames(ctx, filterNamesForTerm(term, names), country, customsYear(0))
	if len(hydrated) > 0 {
		out = append(out, hydrated...)
		src = strings.TrimSpace(firstNonEmpty(src, "importyeti-web") + "+kirchner")
	}
	out = mergeCustomsHits(out, limit)
	if len(out) == 0 {
		return nil, "", nil
	}
	return out, firstNonEmpty(src, "importyeti-web"), nil
}

func (c *Client) getImportYeti(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := newBrowserRequest(ctx, "GET", rawURL, "")
	if err != nil {
		return nil, err
	}
	base := firstNonEmpty(c.importYetiBase(), defaultImportYetiURL)
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Referer", strings.TrimRight(base, "/")+"/")
	req.Header.Set("Origin", base)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if c != nil && strings.TrimSpace(c.ImportYetiCookie) != "" {
		req.Header.Set("Cookie", strings.TrimSpace(c.ImportYetiCookie))
	}
	raw, err := c.do(req)
	if looksLikeImportYetiChallenge(raw) {
		return raw, fmt.Errorf("importyeti: 站点防护拦住了实时检索")
	}
	return raw, err
}

func (c *Client) getImportYetiAPI(ctx context.Context, rawURL string) ([]byte, error) {
	key := ""
	if c != nil {
		key = strings.TrimSpace(c.ImportYetiAPIKey)
	}
	raw, err := c.get(ctx, rawURL, map[string]string{
		"Accept":        "application/json",
		"User-Agent":    browserUA,
		"Authorization": "Bearer " + key,
		"X-API-Key":     key,
	})
	if err != nil {
		return nil, err
	}
	if looksLikeImportYetiChallenge(raw) {
		return raw, fmt.Errorf("importyeti: 站点防护拦住了实时检索")
	}
	return raw, nil
}

func parseImportYetiSearch(raw []byte, base string) []importYetiRow {
	if len(raw) == 0 || looksLikeImportYetiChallenge(raw) {
		return nil
	}
	var top any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil
	}
	items := importYetiJSONItems(top)
	out := make([]importYetiRow, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		row := importYetiRowFromMap(item, base)
		if row.Name == "" {
			continue
		}
		key := strings.ToLower(row.Type + ":" + row.Name + ":" + row.URL)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	return out
}

func importYetiJSONItems(top any) []map[string]any {
	switch t := top.(type) {
	case []any:
		return mapsFromAnySlice(t)
	case map[string]any:
		for _, key := range []string{"results", "data", "companies", "suppliers", "items", "records"} {
			if inner, ok := t[key]; ok {
				if nested := importYetiJSONItems(inner); len(nested) > 0 {
					return nested
				}
			}
		}
		if asString(t["name"]) != "" || asString(t["title"]) != "" {
			return []map[string]any{t}
		}
	}
	return nil
}

func mapsFromAnySlice(arr []any) []map[string]any {
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out
}

func importYetiRowFromMap(m map[string]any, base string) importYetiRow {
	if m == nil {
		return importYetiRow{}
	}
	row := importYetiRow{
		Name:         firstNonEmpty(asString(m["name"]), asString(m["title"]), asString(m["companyName"]), asString(m["company_name"])),
		Type:         strings.ToLower(firstNonEmpty(asString(m["type"]), asString(m["recordType"]), asString(m["companyType"]), asString(m["company_type"]))),
		Address:      firstNonEmpty(asString(m["address"]), asString(m["address_plain"])),
		CountryCode:  strings.ToUpper(firstNonEmpty(asString(m["countryCode"]), asString(m["country_code"]))),
		Country:      asString(m["country"]),
		URL:          firstNonEmpty(asString(m["detailUrl"]), asString(m["detail_url"]), asString(m["profileUrl"]), asString(m["profile_url"]), asString(m["url"]), asString(m["company_url"]), asString(m["supplier_url"])),
		Shipments:    firstPositiveInt(m["totalShipments"], m["total_shipments"], m["shipments"]),
		MostRecent:   formatImportYetiDate(firstNonEmpty(asString(m["mostRecentShipment"]), asString(m["most_recent_shipment"]))),
		TopSuppliers: stringList(firstNonNil(m["topSuppliers"], m["top_suppliers"])),
		TopCustomers: stringList(firstNonNil(m["topCustomers"], m["top_customers"], m["top_custom"])),
		Trademarks:   stringList(firstNonNil(m["trademarks"], m["trademark"])),
	}
	if row.Type == "importer" {
		row.Type = "company"
	}
	if row.Type == "shipper" || row.Type == "exporter" {
		row.Type = "supplier"
	}
	if row.URL != "" && !strings.HasPrefix(row.URL, "http") {
		row.URL = strings.TrimRight(firstNonEmpty(base, defaultImportYetiURL), "/") + "/" + strings.TrimLeft(row.URL, "/")
	}
	if row.Type == "" {
		if kind, _ := importYetiSlug(row.URL); kind != "" {
			row.Type = kind
		}
	}
	if row.CountryCode == "" && row.Country != "" {
		code, _ := inferCountryFromText(row.Country, "")
		row.CountryCode = code
	}
	if row.Name == "" {
		if _, slug := importYetiSlug(row.URL); slug != "" {
			row.Name = importYetiNameFromSlug(slug)
		}
	}
	return row
}

func importYetiHits(rows []importYetiRow, role, country string, limit int) []Hit {
	role = NormalizeRole(role)
	out := make([]Hit, 0, len(rows))
	for _, row := range rows {
		if role == RoleBuyer {
			if row.Type != "" && row.Type != "company" {
				continue
			}
			if !customsCountryIsUS(country) && strings.TrimSpace(country) != "" {
				continue
			}
		} else {
			if row.Type != "" && row.Type != "supplier" {
				continue
			}
			origin := strings.TrimSpace(row.Country + " " + row.CountryCode + " " + row.Address)
			if !customsCountryMatches(origin, country) && strings.TrimSpace(country) != "" {
				if row.CountryCode != "" && !strings.EqualFold(row.CountryCode, country) {
					continue
				}
				if row.CountryCode == "" {
					continue
				}
			}
		}
		hit := hitFromImportYeti(row, role)
		if hit.Name == "" {
			continue
		}
		out = append(out, hit)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func hitFromImportYeti(row importYetiRow, role string) Hit {
	role = NormalizeRole(role)
	code, label := importYetiCountry(row, role)
	extra := map[string]string{
		"via":       "importyeti",
		"shipments": strconv.Itoa(row.Shipments),
		"matching":  strconv.Itoa(row.Shipments),
		"address":   row.Address,
		"last_date": row.MostRecent,
		"type":      row.Type,
	}
	if len(row.Trademarks) > 0 {
		extra["product"] = row.Trademarks[0]
	}
	if len(row.TopSuppliers) > 0 {
		extra["partner"] = row.TopSuppliers[0]
	}
	home := strings.TrimSpace(row.URL)
	snippet := fmt.Sprintf("ImportYeti 公开提单 %d", row.Shipments)
	if row.MostRecent != "" {
		snippet += " · 最近 " + row.MostRecent
	}
	if row.Address != "" {
		snippet += " · " + row.Address
	}
	return Hit{
		ID:           "importyeti:" + strings.ToLower(firstNonEmpty(home, row.Name)),
		Kind:         KindCustoms,
		Platform:     PlatformCustoms,
		Name:         row.Name,
		Title:        row.Name,
		Role:         role,
		Country:      code,
		CountryLabel: label,
		HomepageURL:  home,
		MessageURL:   home,
		Score:        90 + minInt(row.Shipments, 20),
		Snippet:      snippet,
		Source:       "importyeti",
		Extra:        extra,
	}
}

func profileFromImportYeti(row importYetiRow) CustomsProfile {
	role := RoleBuyer
	if row.Type == "supplier" {
		role = RoleSeller
	}
	code, label := importYetiCountry(row, role)
	partners := make([]CustomsPartner, 0, len(row.TopSuppliers)+len(row.TopCustomers))
	for _, name := range row.TopSuppliers {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		partners = append(partners, CustomsPartner{Name: name})
	}
	if role == RoleSeller {
		partners = partners[:0]
		for _, name := range row.TopCustomers {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			partners = append(partners, CustomsPartner{Name: name})
		}
	}
	products := make([]CustomsProduct, 0, len(row.Trademarks))
	for _, mark := range row.Trademarks {
		mark = strings.TrimSpace(mark)
		if mark == "" {
			continue
		}
		products = append(products, CustomsProduct{Code: mark})
	}
	ships := []CustomsShipment{}
	if row.MostRecent != "" {
		ship := CustomsShipment{Date: row.MostRecent}
		if role == RoleBuyer && len(row.TopSuppliers) > 0 {
			ship.Shipper = row.TopSuppliers[0]
			ship.Consignee = row.Name
		}
		if role == RoleSeller && len(row.TopCustomers) > 0 {
			ship.Consignee = row.TopCustomers[0]
			ship.Shipper = row.Name
		}
		ships = append(ships, ship)
	}
	return CustomsProfile{
		Name:           row.Name,
		Role:           role,
		Country:        firstNonEmpty(label, code),
		Address:        row.Address,
		TotalShipments: row.Shipments,
		HomepageURL:    row.URL,
		Suppliers:      partners,
		Products:       products,
		Shipments:      ships,
		Note:           customsPolicyNote,
	}
}

func importYetiCountry(row importYetiRow, role string) (code, label string) {
	if role == RoleBuyer && (row.Type == "company" || row.Type == "") {
		return "US", "美国"
	}
	if row.CountryCode != "" {
		info := LookupCountry(row.CountryCode)
		return info.Code, firstNonEmpty(row.Country, info.Label, row.CountryCode)
	}
	return inferCountryFromText(firstNonEmpty(row.Country, row.Address), "")
}

func pickImportYetiRow(rows []importYetiRow, name, pageURL string) (importYetiRow, bool) {
	wantName := strings.ToLower(strings.TrimSpace(name))
	if strings.TrimSpace(pageURL) != "" {
		for _, row := range rows {
			if strings.EqualFold(strings.TrimRight(row.URL, "/"), strings.TrimRight(pageURL, "/")) {
				return row, true
			}
			_, slug := importYetiSlug(pageURL)
			_, got := importYetiSlug(row.URL)
			if slug != "" && slug == got {
				return row, true
			}
		}
	}
	if wantName != "" {
		for _, row := range rows {
			if strings.ToLower(row.Name) == wantName {
				return row, true
			}
		}
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.Name), wantName) || strings.Contains(wantName, strings.ToLower(row.Name)) {
				return row, true
			}
		}
	}
	if len(rows) == 1 {
		return rows[0], true
	}
	return importYetiRow{}, false
}

func extractImportYetiResults(raw []byte, source string) []Hit {
	hits := extractOrganicResults(raw, source)
	out := make([]Hit, 0, len(hits))
	seen := map[string]struct{}{}
	blob := string(raw)
	if decoded, err := url.QueryUnescape(blob); err == nil {
		blob = decoded
	}
	for _, hit := range hits {
		kind, slug := importYetiSlug(hit.HomepageURL)
		if slug == "" {
			continue
		}
		key := kind + "/" + slug
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		hit.HomepageURL = "https://www.importyeti.com/" + kind + "/" + slug
		hit.MessageURL = hit.HomepageURL
		hit.Name = firstNonEmpty(cleanImportYetiTitle(hit.Name), importYetiNameFromSlug(slug))
		out = append(out, hit)
	}
	for _, m := range importYetiPathRe.FindAllStringSubmatch(blob, 40) {
		kind := strings.ToLower(m[1])
		slug := strings.ToLower(m[2])
		key := kind + "/" + slug
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		home := "https://www.importyeti.com/" + kind + "/" + slug
		out = append(out, Hit{
			Name:        importYetiNameFromSlug(slug),
			HomepageURL: home,
			MessageURL:  home,
			Source:      source,
		})
	}
	return out
}

func importYetiSlug(rawURL string) (kind, slug string) {
	m := importYetiPathRe.FindStringSubmatch(strings.TrimSpace(rawURL))
	if len(m) != 3 {
		return "", ""
	}
	return strings.ToLower(m[1]), strings.ToLower(m[2])
}

func importYetiNameFromURL(rawURL string) string {
	_, slug := importYetiSlug(rawURL)
	return importYetiNameFromSlug(slug)
}

func importYetiNameFromSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}

func cleanImportYetiTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " | ImportYeti")
	s = strings.TrimSuffix(s, " - ImportYeti")
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " | "); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func formatImportYetiDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) >= 10 && s[4] == '-' {
		return s[:10]
	}
	parts := strings.Split(s, "/")
	if len(parts) == 3 {
		day := strings.TrimSpace(parts[0])
		month := strings.TrimSpace(parts[1])
		year := strings.TrimSpace(parts[2])
		if len(year) == 4 && len(day) <= 2 && len(month) <= 2 {
			return year + "-" + pad2(month) + "-" + pad2(day)
		}
	}
	return s
}

func pad2(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

func looksLikeImportYetiChallenge(raw []byte) bool {
	s := strings.ToLower(string(raw))
	return strings.Contains(s, "just a moment") ||
		strings.Contains(s, "cf-mitigated") ||
		strings.Contains(s, "challenges.cloudflare.com")
}

func firstPositiveInt(vals ...any) int {
	for _, v := range vals {
		if n := jsonInt(v); n > 0 {
			return n
		}
	}
	return 0
}

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func stringList(v any) []string {
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(asString(item)); s != "" {
				out = append(out, s)
				continue
			}
			if m, ok := item.(map[string]any); ok {
				if s := firstNonEmpty(asString(m["name"]), asString(m["title"])); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		return []string{s}
	default:
		return nil
	}
}

func mapImportYetiOfficialBols(raw any, limit int) []CustomsShipment {
	arr, _ := raw.([]any)
	out := make([]CustomsShipment, 0, len(arr))
	for _, item := range arr {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		out = append(out, CustomsShipment{
			Date:      formatImportYetiDate(firstNonEmpty(asString(m["date_formatted"]), asString(m["arrival_date"]))),
			Shipper:   firstNonEmpty(asString(m["Shipper_Name"]), asString(m["shipper_name"])),
			Consignee: firstNonEmpty(asString(m["Consignee_Name"]), asString(m["consignee_name"])),
			Product:   firstNonEmpty(asString(m["Product_Description"]), asString(m["product_description"])),
			HSCode:    firstNonEmpty(asString(m["HS_Code"]), asString(m["hs_code"])),
			Country:   firstNonEmpty(asString(m["Country"]), asString(m["country"])),
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
