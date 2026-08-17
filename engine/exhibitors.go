package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
)

const (
	exhibitorDefaultLimit = 120
	exhibitorMaxLimit     = 300
	exhibitorListPagesMax = 4
	defaultEmageIndexURL  = "https://exhibitors.emagecompany.com/"
)

var (
	exhibitorListPathRe = regexp.MustCompile(`(?i)(exhibitor|exhibitors|exhibitor-list|exhibitor-directory|参展商)`)
	exhibitorJunkNameRe = regexp.MustCompile(`(?i)^(公司名|公司名称|展商公司名称|展位号|所在国家.*|company|exhibitor|name|booth|stand|home page|english|繁体|简体)$`)
	exhibitorMenuNameRe = regexp.MustCompile(`名录|名单|数据样本|关于我们|在线订购|网站地图|隐私|版权|免费资源|世界买家|黄页|行业|省区|城市数据|国际名录|特别名录|home page`)
)

// FairExhibitors is the exhibitor roster for one fair.
type FairExhibitors struct {
	Fair string `json:"fair"`
	URL  string `json:"url,omitempty"`
	Hits []Hit  `json:"hits"`
	Note string `json:"note,omitempty"`
}

func (c *Client) searchFairExhibitors(ctx context.Context, keyword, term, country string, limit int) ([]Hit, string) {
	if c == nil || c.DisablePublic {
		return nil, ""
	}
	if limit <= 0 {
		limit = exhibitorDefaultLimit
	}
	if limit > exhibitorMaxLimit {
		limit = exhibitorMaxLimit
	}

	pages := c.discoverExhibitorListPages(ctx, keyword, term, country)
	if len(pages) == 0 {
		return nil, ""
	}
	if len(pages) > exhibitorListPagesMax {
		pages = pages[:exhibitorListPagesMax]
	}

	var (
		mu   sync.Mutex
		hits []Hit
		srcs []string
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(3)
	for _, page := range pages {
		page := page
		g.Go(func() error {
			items, src := c.fetchExhibitorList(gctx, page.URL, page.Fair, limit)
			if len(items) == 0 {
				return nil
			}
			mu.Lock()
			hits = append(hits, items...)
			if src != "" {
				srcs = append(srcs, src)
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return mergeExhibitorHits(hits, country, limit), strings.Join(uniqueStrings(srcs), "+")
}

type exhibitorListPage struct {
	URL  string
	Fair string
}

func (c *Client) discoverExhibitorListPages(ctx context.Context, keyword, term, country string) []exhibitorListPage {
	var (
		mu    sync.Mutex
		pages []exhibitorListPage
	)
	add := func(rawURL, fair string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" || !strings.HasPrefix(rawURL, "http") {
			return
		}
		if isExhibitionDirectory(rawURL, fair) && !isExhibitorListURL(rawURL, fair) {
			return
		}
		if !isExhibitorListURL(rawURL, fair) && !strings.Contains(strings.ToLower(rawURL), "emagecompany.com") {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, p := range pages {
			if strings.EqualFold(p.URL, rawURL) {
				return
			}
		}
		pages = append(pages, exhibitorListPage{URL: rawURL, Fair: firstNonEmpty(fair, keyword)})
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for _, idx := range emageIndexURLs(keyword, term) {
			for _, p := range c.emageListsFromIndex(gctx, idx, keyword, term) {
				add(p.URL, p.Fair)
			}
		}
		return nil
	})
	g.Go(func() error {
		if c.EventsEyeURL == "" {
			return nil
		}
		fairs, _ := c.searchEventsEyeFairs(gctx, keyword, term, country, 6)
		for i, fair := range fairs {
			if i >= 3 {
				break
			}
			name := strings.TrimSpace(fair.Name)
			if name == "" {
				continue
			}
			batch, _, err := c.searchOneIndexExtract(gctx, name+" exhibitor list", extractOrganicResults, "")
			if err != nil {
				continue
			}
			for _, h := range batch {
				if isExhibitorListURL(h.HomepageURL, h.Name+" "+h.Title) {
					add(h.HomepageURL, firstNonEmpty(cleanFairTitle(h.Name), name))
				}
			}
		}
		return nil
	})
	g.Go(func() error {
		queries := []string{
			`site:exhibitors.emagecompany.com ` + firstNonEmpty(term, keyword),
			firstNonEmpty(term, keyword) + " 参展商名单",
			firstNonEmpty(term, keyword) + " exhibitor list",
			keyword + " 展会 参展商名单",
		}
		if geo := strings.TrimSpace(LookupCountry(country).Query); geo != "" {
			queries = append(queries, firstNonEmpty(term, keyword)+" exhibitor list "+geo)
		}
		for _, q := range uniqueFoldedStrings(queries) {
			batch, _, err := c.searchOneIndexExtract(gctx, q, extractOrganicResults, "")
			if err != nil {
				continue
			}
			for _, h := range batch {
				if isExhibitorListURL(h.HomepageURL, h.Name+" "+h.Title) {
					add(h.HomepageURL, firstNonEmpty(cleanFairTitle(h.Name), keyword))
				}
			}
		}
		return nil
	})
	_ = g.Wait()
	return pages
}

func emageIndexURLs(keyword, term string) []string {
	blob := strings.ToLower(keyword + " " + term)
	out := []string{defaultEmageIndexURL}
	switch {
	case containsAny(blob, "furniture", "家具", "sofa", "chair", "wood", "木工", "mattress"):
		out = append(out, defaultEmageIndexURL+"wood/")
	case containsAny(blob, "machine", "motor", "robot", "机械", "电机", "机器人"):
		out = append(out, defaultEmageIndexURL+"machinery/")
	case containsAny(blob, "led", "light", "lamp", "电子", "光电", "it", "ai"):
		out = append(out, defaultEmageIndexURL+"it/")
	}
	return uniqueFoldedStrings(out)
}

func (c *Client) emageListsFromIndex(ctx context.Context, idx, keyword, term string) []exhibitorListPage {
	raw, err := c.getHTMLReferer(ctx, idx, defaultEmageIndexURL)
	if err != nil || len(raw) == 0 {
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil
	}
	tokens := fairSearchTokens(keyword, term)
	var out []exhibitorListPage
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		title := strings.TrimSpace(s.Text())
		if title == "" || !strings.Contains(title, "参展商") {
			return
		}
		abs := resolveHTTPURL(idx, href)
		if abs == "" {
			return
		}
		if strings.Contains(strings.ToLower(idx), "emagecompany.com") && !strings.Contains(strings.ToLower(abs), "emagecompany.com") {
			return
		}
		if len(tokens) > 0 && !fairTextMatches(title+" "+abs, tokens) {
			return
		}
		out = append(out, exhibitorListPage{URL: abs, Fair: strings.TrimSuffix(strings.TrimSpace(title), "参展商名单")})
	})
	if len(out) > exhibitorListPagesMax {
		out = out[:exhibitorListPagesMax]
	}
	return out
}

// LookupFairExhibitors loads the public exhibitor roster for one fair.
func (c *Client) LookupFairExhibitors(ctx context.Context, name, pageURL string, limit int) (FairExhibitors, error) {
	name = strings.TrimSpace(name)
	pageURL = strings.TrimSpace(pageURL)
	if name == "" && pageURL == "" {
		return FairExhibitors{}, fmt.Errorf("请输入展会名称")
	}
	if c == nil || c.DisablePublic {
		return FairExhibitors{}, fmt.Errorf("展会数据源未配置")
	}
	if limit <= 0 {
		limit = exhibitorDefaultLimit
	}
	if limit > exhibitorMaxLimit {
		limit = exhibitorMaxLimit
	}

	var pages []exhibitorListPage
	if isExhibitorListURL(pageURL, name) {
		pages = append(pages, exhibitorListPage{URL: pageURL, Fair: name})
	}
	if pageURL != "" {
		for _, path := range []string{"exhibitors", "en/exhibitors", "exhibitor-list", "fair/exhibitor-list", "exhibitor-directory"} {
			pages = append(pages, exhibitorListPage{URL: joinHTTPPath(pageURL, path), Fair: name})
		}
	}
	if name != "" {
		pages = append(pages, c.discoverExhibitorListPages(ctx, name, englishProductTerm(name, ""), "")...)
	}

	seen := map[string]struct{}{}
	var best []Hit
	srcURL := pageURL
	tried := 0
	for _, p := range pages {
		if _, ok := seen[p.URL]; ok {
			continue
		}
		seen[p.URL] = struct{}{}
		tried++
		items, _ := c.fetchExhibitorList(ctx, p.URL, firstNonEmpty(p.Fair, name), limit)
		if len(items) > len(best) {
			best = items
			srcURL = p.URL
		}
		if len(best) >= 40 || tried >= 6 {
			break
		}
	}
	merged := mergeExhibitorHits(best, "", limit)
	note := exhibitionPolicyNote
	if len(merged) == 0 {
		note = "没有在公开网页上找到这家展会的参展商名单。可打开官网自行查看。"
	}
	return FairExhibitors{
		Fair: firstNonEmpty(name, srcURL),
		URL:  srcURL,
		Hits: merged,
		Note: note,
	}, nil
}

func (c *Client) fetchExhibitorList(ctx context.Context, listURL, fair string, limit int) ([]Hit, string) {
	if c == nil || strings.TrimSpace(listURL) == "" {
		return nil, ""
	}
	raw, err := c.getHTMLReferer(ctx, listURL, listURL)
	if err != nil || len(raw) == 0 {
		return nil, ""
	}
	hits := parseExhibitorDocument(raw, listURL, fair)
	if endpoint := exhibitorJSONEndpoint(raw, listURL); endpoint != "" {
		if extra := c.fetchExhibitorJSON(ctx, endpoint, fair); len(extra) > 0 {
			hits = append(hits, extra...)
		}
	}
	hits = mergeExhibitorHits(hits, "", limit)
	if len(hits) == 0 {
		return nil, ""
	}
	src := "exhibitor-html"
	if strings.Contains(strings.ToLower(listURL), "emagecompany.com") {
		src = "emage"
	}
	return hits, src
}

func (c *Client) fetchExhibitorJSON(ctx context.Context, endpoint, fair string) []Hit {
	raw, err := c.get(ctx, endpoint, map[string]string{
		"Accept":     "application/json,text/plain,*/*",
		"User-Agent": browserUA,
	})
	if err != nil || len(raw) == 0 {
		return nil
	}
	return parseExhibitorJSON(raw, endpoint, fair)
}

func exhibitorJSONEndpoint(raw []byte, pageURL string) string {
	html := string(raw)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	base := ""
	doc.Find("[data-exhibitors-endpoint], [data-json-exhibitor-url]").Each(func(_ int, s *goquery.Selection) {
		if base != "" {
			return
		}
		base = firstNonEmpty(s.AttrOr("data-exhibitors-endpoint", ""), s.AttrOr("data-json-exhibitor-url", ""))
	})
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	abs := resolveHTTPURL(pageURL, base)
	if !strings.HasSuffix(abs, ".json") {
		abs = strings.TrimRight(abs, "/") + "/all.json"
	}
	return abs
}

func parseExhibitorDocument(raw []byte, listURL, fair string) []Hit {
	if len(raw) == 0 {
		return nil
	}
	trim := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[") {
		return parseExhibitorJSON(raw, listURL, fair)
	}
	hits := parseExhibitorTables(raw, listURL, fair)
	hits = append(hits, parseExhibitorAnchors(raw, listURL, fair)...)
	return hits
}

func parseExhibitorJSON(raw []byte, listURL, fair string) []Hit {
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	rows := exhibitorJSONRows(doc)
	out := make([]Hit, 0, len(rows))
	for _, row := range rows {
		name := firstNonEmpty(asString(row["title"]), asString(row["name"]), asString(row["company"]), asString(row["exhibitor"]))
		if !keepExhibitorName(name) {
			continue
		}
		home := firstNonEmpty(asString(row["url"]), asString(row["website"]), asString(row["homepage"]))
		if home != "" {
			home = resolveHTTPURL(listURL, home)
		}
		booth := firstNonEmpty(asString(row["stand"]), asString(row["booth"]), asString(row["stand_number"]))
		out = append(out, exhibitorHit(name, fair, home, booth, asString(row["country"]), listURL))
	}
	return out
}

func exhibitorJSONRows(doc any) []map[string]any {
	switch t := doc.(type) {
	case []any:
		out := make([]map[string]any, 0, len(t))
		for _, item := range t {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"exhibitors", "data", "items", "results", "list"} {
			if nested := exhibitorJSONRows(t[key]); len(nested) > 0 {
				return nested
			}
		}
	}
	return nil
}

func parseExhibitorTables(raw []byte, listURL, fair string) []Hit {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil
	}
	var best []Hit
	doc.Find("table").Each(func(_ int, table *goquery.Selection) {
		rows := table.ChildrenFiltered("tbody").ChildrenFiltered("tr")
		if rows.Length() == 0 {
			rows = table.ChildrenFiltered("tr")
		}
		if rows.Length() < 3 {
			return
		}
		nameIdx, boothIdx, countryIdx := -1, -1, -1
		header := rows.First()
		header.ChildrenFiltered("th, td").Each(func(i int, cell *goquery.Selection) {
			label := strings.ToLower(strings.TrimSpace(cell.Text()))
			switch {
			case nameIdx < 0 && (strings.Contains(label, "公司") || strings.Contains(label, "展商") || strings.Contains(label, "company") || strings.Contains(label, "exhibitor") || label == "name"):
				nameIdx = i
			case boothIdx < 0 && (strings.Contains(label, "展位") || strings.Contains(label, "booth") || strings.Contains(label, "stand")):
				boothIdx = i
			case countryIdx < 0 && (strings.Contains(label, "国家") || strings.Contains(label, "地区") || strings.Contains(label, "country") || strings.Contains(label, "展区")):
				countryIdx = i
			}
		})
		if nameIdx < 0 {
			nameIdx = 0
		}
		if boothIdx < 0 {
			return
		}
		var hits []Hit
		rows.Each(func(ri int, row *goquery.Selection) {
			if ri == 0 && nameIdx >= 0 {
				return
			}
			cells := row.ChildrenFiltered("td, th")
			if cells.Length() == 0 {
				return
			}
			cellText := func(i int) string {
				if i < 0 || i >= cells.Length() {
					return ""
				}
				return strings.TrimSpace(cells.Eq(i).Text())
			}
			name := cellText(nameIdx)
			if !keepExhibitorName(name) {
				return
			}
			home := ""
			if a := cells.Eq(nameIdx).Find("a[href]"); a.Length() > 0 {
				home = resolveHTTPURL(listURL, a.AttrOr("href", ""))
			}
			hits = append(hits, exhibitorHit(name, fair, home, cellText(boothIdx), cellText(countryIdx), listURL))
		})
		if len(hits) > len(best) {
			best = hits
		}
	})
	return best
}

func parseExhibitorAnchors(raw []byte, listURL, fair string) []Hit {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil
	}
	var out []Hit
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href := strings.TrimSpace(s.AttrOr("href", ""))
		name := strings.TrimSpace(s.Text())
		if !keepExhibitorName(name) {
			return
		}
		low := strings.ToLower(href)
		if !strings.Contains(low, "/exhibitors/") && !strings.Contains(low, "/exhibitor/") {
			return
		}
		if strings.Contains(low, "for-exhibitors") || strings.Contains(low, "why-exhibit") {
			return
		}
		home := resolveHTTPURL(listURL, href)
		if home == "" {
			return
		}
		out = append(out, exhibitorHit(name, fair, home, "", "", listURL))
	})
	return out
}

func exhibitorHit(name, fair, home, booth, geo, listURL string) Hit {
	name = strings.Join(strings.Fields(name), " ")
	fair = strings.TrimSpace(fair)
	code, label := "", ""
	if strings.TrimSpace(geo) != "" {
		code, label = inferCountryFromText(geo, "")
		if label == "" || label == "不限" {
			label = strings.TrimSpace(geo)
		}
	}
	snippet := strings.TrimSpace(strings.Join([]string{fair, booth, label}, " · "))
	extra := map[string]string{
		"fair":  fair,
		"booth": booth,
		"via":   "exhibitor-list",
		"list":  listURL,
	}
	return Hit{
		ID:           "exhibitor:" + strings.ToLower(fair+"|"+name),
		Kind:         KindExhibition,
		Platform:     PlatformExhibition,
		Name:         name,
		Title:        name,
		Role:         RoleSeller,
		HomepageURL:  firstNonEmpty(home, listURL),
		MessageURL:   firstNonEmpty(home, listURL),
		Country:      code,
		CountryLabel: label,
		Score:        88,
		Snippet:      snippet,
		Extra:        extra,
	}
}

func keepExhibitorName(s string) bool {
	s = strings.Join(strings.Fields(s), " ")
	compact := strings.ReplaceAll(s, " ", "")
	if s == "" || exhibitorJunkNameRe.MatchString(s) || exhibitorMenuNameRe.MatchString(s) || exhibitorMenuNameRe.MatchString(compact) {
		return false
	}
	runes := []rune(s)
	if len(runes) < 2 || len(runes) > 80 {
		return false
	}
	letters := 0
	for _, r := range runes {
		if r > 127 || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			letters++
		}
	}
	return letters >= 2
}

func isExhibitorListURL(rawURL, title string) bool {
	blob := strings.ToLower(rawURL + " " + title)
	if strings.Contains(blob, "emagecompany.com") && (strings.Contains(blob, "参展商") || strings.HasSuffix(strings.ToLower(rawURL), ".html")) {
		return true
	}
	if strings.Contains(blob, "for-exhibitors") || strings.Contains(blob, "why-exhibit") || strings.Contains(blob, "book-stand") {
		return false
	}
	return exhibitorListPathRe.MatchString(blob) && (strings.Contains(blob, "list") || strings.Contains(blob, "directory") || strings.Contains(blob, "名单") || strings.Contains(blob, "/exhibitors") || strings.Contains(blob, "exhibitor-list"))
}

func mergeExhibitorHits(items []Hit, country string, limit int) []Hit {
	seen := map[string]Hit{}
	order := make([]string, 0, len(items))
	for _, hit := range items {
		if !keepExhibitorName(hit.Name) {
			continue
		}
		if selected := strings.ToUpper(strings.TrimSpace(country)); selected != "" && hit.Country != "" && hit.Country != selected {
			if !customsCountryMatches(hit.CountryLabel+" "+hit.Country, selected) {
				continue
			}
		}
		key := strings.ToLower(strings.TrimSpace(hit.Name)) + "|" + strings.ToLower(hit.Extra["fair"])
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = hit
		order = append(order, key)
	}
	out := make([]Hit, 0, len(order))
	for _, key := range order {
		out = append(out, seen[key])
	}
	hasBooth := false
	for _, h := range out {
		if strings.TrimSpace(h.Extra["booth"]) != "" {
			hasBooth = true
			break
		}
	}
	if hasBooth {
		filtered := make([]Hit, 0, len(out))
		for _, h := range out {
			if strings.TrimSpace(h.Extra["booth"]) != "" {
				filtered = append(filtered, h)
			}
		}
		out = filtered
	}
	return clipHits(out, limit)
}

func resolveHTTPURL(base, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "#") {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if ref.IsAbs() {
		if ref.Scheme != "http" && ref.Scheme != "https" {
			return ""
		}
		return ref.String()
	}
	bu, err := url.Parse(base)
	if err != nil || bu.Host == "" {
		return ""
	}
	return bu.ResolveReference(ref).String()
}

func joinHTTPPath(base, path string) string {
	base = strings.TrimSpace(base)
	path = strings.Trim(strings.TrimSpace(path), "/")
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil {
		return strings.TrimRight(base, "/") + "/" + path
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func cleanFairTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "参展商名单")
	s = strings.TrimSuffix(s, "Exhibitor List")
	s = strings.TrimSuffix(s, "exhibitor list")
	return strings.TrimSpace(s)
}

func exhibitorLimit(limit int) int {
	if limit <= 0 {
		return exhibitorDefaultLimit
	}
	if limit > exhibitorMaxLimit {
		return exhibitorMaxLimit
	}
	return limit
}
