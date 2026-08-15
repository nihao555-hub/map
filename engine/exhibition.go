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
	exhibitionDefaultLimit = 40
	exhibitionMaxLimit     = 80
)

var fairTokenRe = regexp.MustCompile(`(?i)(trade fair|trade show|exhibition|expo|messe|salon|\bfairs?\b|展会|博览会|展览会)`)

// searchExhibition finds trade fairs (default) or exhibitors (role=seller)
// from live Wikidata SPARQL plus the same public web indexes 智能引擎 already uses.
func (c *Client) searchExhibition(ctx context.Context, q Query) (Result, error) {
	wantExhibitors := NormalizeRole(q.Role) == RoleSeller
	term := firstNonEmpty(englishProductTerm(q.Keyword, q.Country), q.Keyword)

	if wantExhibitors {
		limit := exhibitorLimit(q.Limit)
		items, src := c.searchFairExhibitors(ctx, q.Keyword, term, q.Country, limit)
		note := exhibitionPolicyNote
		if len(items) == 0 {
			note = strings.TrimSpace(note + " 没有在公开名录里找到参展商名单。")
		}
		return Result{
			Hits:     items,
			Sources:  uniqueStrings([]string{src}),
			Note:     note,
			Expanded: []string{term},
		}, nil
	}

	limit := q.Limit
	if limit <= 0 {
		limit = exhibitionDefaultLimit
	}
	if limit > exhibitionMaxLimit {
		limit = exhibitionMaxLimit
	}

	var (
		mu       sync.Mutex
		hits     []Hit
		warnings []string
		sources  []string
	)
	add := func(items []Hit, src, warn string) {
		mu.Lock()
		defer mu.Unlock()
		if warn != "" {
			warnings = append(warnings, warn)
		}
		if src != "" && len(items) > 0 {
			sources = append(sources, src)
		}
		hits = append(hits, items...)
	}

	g, gctx := errgroup.WithContext(ctx)

	if !wantExhibitors && c != nil && strings.TrimSpace(c.AUMAFairURL) != "" {
		g.Go(func() error {
			items, src := c.searchAUMAFairs(gctx, q.Keyword, term, q.Country, limit)
			add(items, src, "")
			return nil
		})
	}

	if !wantExhibitors && c != nil {
		g.Go(func() error {
			items, src := c.searchOpenFairs(gctx, q.Keyword, term, q.Country, limit)
			add(items, src, "")
			return nil
		})
	}

	if !wantExhibitors && c != nil && strings.TrimSpace(c.WikidataURL) != "" {
		g.Go(func() error {
			items, err := c.searchWikidataFairs(gctx, q.Keyword, term, q.Country, limit)
			if err != nil {
				add(nil, "", err.Error())
				return nil
			}
			add(items, "wikidata", "")
			return nil
		})
	}

	if c != nil && !c.DisablePublic {
		g.Go(func() error {
			items, src, err := c.searchPublicFairs(gctx, q.Keyword, term, q.Country, wantExhibitors)
			if err != nil {
				add(nil, "", err.Error())
				return nil
			}
			add(items, src, "")
			return nil
		})
	}

	_ = g.Wait()

	merged := mergeExhibitionHits(hits, q.Country, limit)
	return Result{
		Hits:     merged,
		Warnings: uniqueStrings(warnings),
		Sources:  uniqueStrings(sources),
		Note:     exhibitionPolicyNote,
		Expanded: []string{term},
	}, nil
}

func englishProductTerm(keyword, country string) string {
	for _, t := range LocalSearchTerms(keyword, firstNonEmpty(country, "US")) {
		if strings.TrimSpace(t) != "" && !hasCJK(t) {
			return t
		}
	}
	return strings.TrimSpace(keyword)
}

func (c *Client) searchWikidataFairs(ctx context.Context, keyword, term, country string, limit int) ([]Hit, error) {
	if c == nil || strings.TrimSpace(c.WikidataURL) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	var lastErr error
	for _, query := range []string{
		wikidataFairEntitySPARQL(keyword, term, country, limit),
		wikidataFairLabelSPARQL(keyword, term, country, limit),
	} {
		hits, err := c.runWikidataSPARQL(ctx, query, country)
		if err != nil {
			lastErr = err
			continue
		}
		if len(hits) > 0 {
			return hits, nil
		}
	}
	return nil, lastErr
}

func (c *Client) runWikidataSPARQL(ctx context.Context, query, country string) ([]Hit, error) {
	u, err := url.Parse(c.WikidataURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("query", query)
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	raw, err := c.get(ctx, u.String(), map[string]string{
		"Accept":     "application/sparql-results+json",
		"User-Agent": "map-engine/exhibition (https://github.com/nihao555-hub/map)",
	})
	if err != nil {
		return nil, err
	}
	return parseWikidataFairs(raw, country)
}

func wikidataFairSPARQL(keyword, term, country string, limit int) string {
	return wikidataFairEntitySPARQL(keyword, term, country, limit)
}

func wikidataFairSearchTerm(keyword, term string) string {
	return firstNonEmpty(term, keyword)
}

func wikidataCountryFilter(country string) string {
	if info := LookupCountry(country); info.Query != "" {
		return fmt.Sprintf(`OPTIONAL { ?item wdt:P17 ?country. }
  OPTIONAL { ?country rdfs:label ?countryEn FILTER(LANG(?countryEn)="en"). }
  FILTER(!BOUND(?countryEn) || CONTAINS(LCASE(?countryEn), LCASE(%q)))`, info.Query)
	}
	return `OPTIONAL { ?item wdt:P17 ?country. }`
}

func wikidataFairEntitySPARQL(keyword, term, country string, limit int) string {
	search := wikidataFairSearchTerm(keyword, term)
	if limit > 30 {
		limit = 30
	}
	if !fairTokenRe.MatchString(search) {
		search = strings.TrimSpace(search + " trade fair")
	}
	return fmt.Sprintf(`SELECT ?item ?itemLabel ?start ?end ?countryLabel ?cityLabel ?website WHERE {
  SERVICE wikibase:mwapi {
    bd:serviceParam wikibase:api "EntitySearch" .
    bd:serviceParam wikibase:endpoint "www.wikidata.org" .
    bd:serviceParam mwapi:search %q .
    bd:serviceParam mwapi:language "en" .
    ?item wikibase:apiOutputItem mwapi:item .
  }
  ?item wdt:P31/wdt:P279* wd:Q57305 .
  OPTIONAL { ?item wdt:P580 ?start. }
  OPTIONAL { ?item wdt:P582 ?end. }
  OPTIONAL { ?item wdt:P276 ?city. }
  OPTIONAL { ?item wdt:P856 ?website. }
  %s
  SERVICE wikibase:label { bd:serviceParam wikibase:language "zh,en". }
}
LIMIT %d`, search, wikidataCountryFilter(country), limit)
}

func wikidataFairLabelSPARQL(keyword, term, country string, limit int) string {
	search := wikidataFairSearchTerm(keyword, term)
	if limit > 30 {
		limit = 30
	}
	return fmt.Sprintf(`SELECT ?item ?itemLabel ?start ?end ?countryLabel ?cityLabel ?website WHERE {
  ?item wdt:P31/wdt:P279* wd:Q57305 .
  ?item rdfs:label ?lab .
  FILTER(LANG(?lab) = "en")
  FILTER(CONTAINS(LCASE(?lab), LCASE(%q)))
  OPTIONAL { ?item wdt:P580 ?start. }
  OPTIONAL { ?item wdt:P582 ?end. }
  OPTIONAL { ?item wdt:P276 ?city. }
  OPTIONAL { ?item wdt:P856 ?website. }
  %s
  SERVICE wikibase:label { bd:serviceParam wikibase:language "zh,en". }
}
LIMIT %d`, search, wikidataCountryFilter(country), limit)
}

func parseWikidataFairs(raw []byte, selected string) ([]Hit, error) {
	var doc struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(doc.Results.Bindings))
	for _, row := range doc.Results.Bindings {
		name := strings.TrimSpace(row["itemLabel"].Value)
		if name == "" {
			continue
		}
		home := strings.TrimSpace(row["website"].Value)
		if home == "" {
			home = strings.TrimSpace(row["item"].Value)
		}
		country := strings.TrimSpace(row["countryLabel"].Value)
		city := strings.TrimSpace(row["cityLabel"].Value)
		start := trimWikidataTime(row["start"].Value)
		end := trimWikidataTime(row["end"].Value)
		code, label := inferCountryFromText(country, selected)
		when := strings.TrimSpace(start)
		if end != "" && end != start {
			when = strings.TrimSpace(start + " – " + end)
		}
		snippet := strings.TrimSpace(strings.Join([]string{label, city, when}, " · "))
		out = append(out, Hit{
			ID:           "fair:" + strings.ToLower(name),
			Kind:         KindExhibition,
			Platform:     PlatformExhibition,
			Name:         name,
			Title:        name,
			Snippet:      snippet,
			HomepageURL:  home,
			MessageURL:   home,
			Country:      code,
			CountryLabel: firstNonEmpty(label, country),
			Score:        90,
			Extra: map[string]string{
				"city":  city,
				"start": start,
				"end":   end,
				"via":   "wikidata",
			},
		})
	}
	return out, nil
}

func trimWikidataTime(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 && s[4] == '-' {
		return s[:10]
	}
	return s
}

func (c *Client) searchPublicFairs(ctx context.Context, keyword, term, country string, exhibitors bool) ([]Hit, string, error) {
	queries := exhibitionQueries(keyword, term, country, exhibitors)
	var (
		merged []Hit
		srcs   []string
	)
	for _, q := range queries {
		batch, src, err := c.searchOneIndexExtract(ctx, q, extractOrganicResults, "")
		if err != nil {
			continue
		}
		if src != "" {
			srcs = append(srcs, src)
		}
		for _, h := range batch {
			h.Kind = KindExhibition
			h.Platform = PlatformExhibition
			if exhibitors {
				h.Role = RoleSeller
			}
			if keepExhibitionHit(h, exhibitors) {
				merged = append(merged, h)
			}
		}
	}
	return merged, strings.Join(uniqueStrings(srcs), "+"), nil
}

func exhibitionQueries(keyword, term, country string, exhibitors bool) []string {
	geo := strings.TrimSpace(LookupCountry(country).Query)
	base := firstNonEmpty(term, keyword)
	var out []string
	if exhibitors {
		out = []string{
			base + " exhibitor list",
			base + " trade show exhibitor",
			keyword + " 展会 参展商",
			"site:10times.com " + base + " exhibitor",
		}
		if geo != "" {
			out = append([]string{base + " exhibitor " + geo}, out...)
		}
	} else {
		out = []string{
			base + " trade fair",
			base + " trade show 2026",
			keyword + " 展会",
			"site:auma.de " + base + " fair",
			"site:10times.com " + base + " tradeshow",
		}
		if geo != "" {
			out = append([]string{base + " trade fair " + geo}, out...)
		}
	}
	return uniqueFoldedStrings(out)
}

func isExhibitionDirectory(home, title string) bool {
	home = strings.ToLower(strings.TrimSpace(home))
	title = strings.ToLower(title)
	if u, err := url.Parse(home); err == nil {
		host := strings.ToLower(u.Host)
		path := strings.Trim(u.Path, "/")
		segs := strings.Split(path, "/")
		if strings.Contains(host, "10times.com") {
			if path == "" || path == "tradeshows" || path == "top100" || strings.HasPrefix(path, "top100/") {
				return true
			}
			if len(segs) == 1 && (strings.Contains(title, "calendar") || strings.Contains(title, "directory") ||
				strings.Contains(title, "trade shows") || strings.Contains(title, "exhibitions")) {
				return true
			}
		}
		if strings.Contains(host, "eventseye.com") && (path == "" || strings.Contains(path, "countries") || strings.Contains(path, "calendar")) {
			return true
		}
	}
	return false
}

func keepExhibitionHit(h Hit, exhibitors bool) bool {
	home := strings.ToLower(h.HomepageURL)
	if home == "" {
		return false
	}
	if isExhibitionDirectory(home, h.Name+" "+h.Title) {
		return false
	}
	for _, bad := range []string{
		"duckduckgo.com", "bing.com", "google.com", "brave.com",
		"facebook.com", "instagram.com", "tiktok.com", "youtube.com/watch",
		"x.com/", "twitter.com", "wikipedia.org", "wikidata.org",
		"kraken.com", "binance.com", "coinbase.com",
		"tradefest.io", "expoassist.com",
	} {
		if strings.Contains(home, bad) {
			return false
		}
	}
	blob := strings.ToLower(h.Name + " " + h.Title + " " + h.Snippet + " " + home)
	if strings.Contains(blob, "crypto") || strings.Contains(blob, "margin trading") ||
		strings.Contains(blob, "forex") || strings.Contains(blob, "bitcoin") {
		return false
	}
	if exhibitors {
		hasExhibitor := strings.Contains(blob, "exhibitor") ||
			strings.Contains(blob, "exhibitors") ||
			strings.Contains(blob, "参展") ||
			strings.Contains(blob, "booth")
		return hasExhibitor && fairTokenRe.MatchString(blob)
	}
	title := strings.ToLower(h.Name + " " + h.Title)
	if strings.Contains(title, "calendar") || strings.Contains(title, "directory") ||
		strings.Contains(title, "complete guide") || strings.Contains(title, "list of") ||
		strings.Contains(title, "trade shows") || strings.Contains(title, "exhibitions calendar") ||
		strings.Contains(title, "fairs calendar") || strings.Contains(title, "top furniture fairs") ||
		strings.Contains(title, "时间表") || strings.Contains(title, "排期") {
		return false
	}
	for _, host := range []string{"expoassist.", "globalfurniturefairs.com", "worldfurnitureonline.com", "jufair.com"} {
		if strings.Contains(home, host) {
			return false
		}
	}
	return fairTokenRe.MatchString(blob)
}

func extractOrganicResults(raw []byte, source string) []Hit {
	html := string(raw)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []Hit
	add := func(href, title, snippet string) {
		href = strings.TrimSpace(href)
		href = decodeBingRedirect(href)
		title = strings.TrimSpace(title)
		if href == "" || title == "" {
			return
		}
		if strings.HasPrefix(href, "/l/?kh=") || strings.Contains(href, "uddg=") {
			if u, err := url.Parse(href); err == nil {
				if v := u.Query().Get("uddg"); v != "" {
					href = v
				}
			}
		}
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			return
		}
		if _, ok := seen[href]; ok {
			return
		}
		seen[href] = struct{}{}
		if len(snippet) > 220 {
			snippet = snippet[:220]
		}
		out = append(out, Hit{
			ID:          "web:" + href,
			Name:        title,
			Title:       title,
			Snippet:     snippet,
			HomepageURL: href,
			MessageURL:  href,
			Source:      source,
			Score:       60,
			Extra:       map[string]string{"via": source},
		})
	}
	doc.Find("a.result__a, li.b_algo h2 a, #b_results h2 a, a[data-testid='result-title-a']").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		title := strings.TrimSpace(s.Text())
		snippet := strings.TrimSpace(s.Parent().Text())
		add(href, title, snippet)
	})
	return out
}

func mergeExhibitionHits(items []Hit, country string, limit int) []Hit {
	seen := map[string]Hit{}
	order := make([]string, 0, len(items))
	for _, hit := range items {
		if hit.Name == "" {
			continue
		}
		if selected := strings.ToUpper(strings.TrimSpace(country)); selected != "" && hit.Country != "" && hit.Country != selected {
			if !customsCountryMatches(hit.CountryLabel+" "+hit.Country, selected) {
				continue
			}
		}
		key := strings.ToLower(strings.TrimSpace(hit.Name))
		if hit.HomepageURL != "" {
			key = strings.ToLower(hit.HomepageURL)
		}
		if prev, ok := seen[key]; ok {
			if hit.Score > prev.Score {
				seen[key] = hit
			}
			continue
		}
		seen[key] = hit
		order = append(order, key)
	}
	out := make([]Hit, 0, len(order))
	for _, key := range order {
		out = append(out, seen[key])
	}
	return clipHits(out, limit)
}
