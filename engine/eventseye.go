package engine

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"
	"golang.org/x/sync/errgroup"
)

const defaultEventsEyeURL = "https://www.eventseye.com"

var (
	eventsEyeFairPathRe = regexp.MustCompile(`(?i)(?:^|/)f-[a-z0-9][^/]*-\d+-1\.html$`)
	eventsEyeDateRe     = regexp.MustCompile(`(\d{2})/(\d{2})/(\d{4})`)
	eventsEyeVenueRe    = regexp.MustCompile(`^(.*)\(([^)]+)\)\s*$`)
)

type eventsEyeTheme struct {
	slug string
	keys []string
}

// EventsEye industry hubs (https://www.eventseye.com/fairs/trade-shows-by-theme.html).
var eventsEyeThemeCatalog = []eventsEyeTheme{
	{slug: "decoration-furniture-lighting", keys: []string{"furniture", "家具", "sofa", "chair", "mattress", "lighting", "灯", "照明", "interior", "家居", "decor", "wood", "木工"}},
	{slug: "electronics-electrotechnics", keys: []string{"led", "electron", "电子", "chip", "semicon", "光电", "电器"}},
	{slug: "ict-information-communications-technologies", keys: []string{"software", "saas", " ict", "手机", "通信", "telecom"}},
	{slug: "fashion-apparel-textiles-leather-fur", keys: []string{"fashion", "apparel", "textile", "服装", "纺织", "shoe", "鞋", "leather", "皮具"}},
	{slug: "agriculture-food-processing", keys: []string{"food", "coffee", "tea", "食品", "咖啡", "茶", "beverage", "snack", "海鲜", "seafood"}},
	{slug: "healthcare-pharmaceuticals", keys: []string{"medical", "pharma", "hospital", "医疗", "医药", "药械", "clinic"}},
	{slug: "automobile-automotive-industry", keys: []string{"auto", "car", "vehicle", "汽车", "汽配", "motor"}},
	{slug: "techniques-process-equipment", keys: []string{"machine", "machinery", "tool", "机械", "机床", "电机", "robot", "机器人"}},
	{slug: "building-construction-architecture", keys: []string{"build", "construction", "建材", "建筑", "concrete", "bauma"}},
	{slug: "logistics-transport-packaging", keys: []string{"packag", "logistic", "包装", "物流"}},
	{slug: "jewellery-watch-making-gifts", keys: []string{"jewel", "watch", "gift", "珠宝", "手表", "礼品"}},
	{slug: "chemistry-energy-materials", keys: []string{"energy", "solar", "battery", "能源", "光伏", "电池", "化工"}},
	{slug: "consumer-goods", keys: []string{"canton", "广交", "gift", "consumer", "日用"}},
}

func (c *Client) searchEventsEyeFairs(ctx context.Context, keyword, term, country string, limit int) ([]Hit, string) {
	if c == nil || strings.TrimSpace(c.EventsEyeURL) == "" {
		return nil, ""
	}
	if limit <= 0 {
		limit = exhibitionDefaultLimit
	}
	urls := c.eventsEyeFetchURLs(keyword, term)
	if len(urls) == 0 {
		return nil, ""
	}

	var (
		mu  sync.Mutex
		all []Hit
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(2)
	for _, rawURL := range urls {
		rawURL := rawURL
		g.Go(func() error {
			raw, err := c.getHTMLReferer(gctx, rawURL, c.EventsEyeURL+"/")
			if err != nil || len(raw) == 0 {
				return nil
			}
			hits := parseEventsEyeFairs(raw, c.EventsEyeURL)
			if len(hits) == 0 {
				return nil
			}
			mu.Lock()
			all = append(all, hits...)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	if len(all) == 0 {
		return nil, ""
	}

	core := coreFairTokens(keyword, term)
	tokens := fairSearchTokens(keyword, term)
	out := make([]Hit, 0, limit)
	seen := map[string]struct{}{}
	// Core keyword in the name first, then in the blurb, then expanded industry tokens.
	for _, pass := range []int{0, 1, 2} {
		for _, hit := range all {
			key := strings.ToLower(strings.TrimSpace(hit.HomepageURL))
			if key == "" {
				key = strings.ToLower(hit.Name)
			}
			if _, ok := seen[key]; ok {
				continue
			}
			if selected := strings.ToUpper(strings.TrimSpace(country)); selected != "" && hit.Country != "" && hit.Country != selected {
				if !customsCountryMatches(hit.CountryLabel+" "+hit.Country, selected) {
					continue
				}
			}
			blob := hit.Name + " " + hit.Snippet + " " + hit.Extra["city"]
			nameCore := len(core) > 0 && fairTextMatches(hit.Name, core)
			blobCore := len(core) > 0 && fairTextMatches(blob, core)
			wide := len(tokens) > 0 && fairTextMatches(blob, tokens)
			if !nameCore && !blobCore && !wide {
				continue
			}
			switch pass {
			case 0:
				if !nameCore {
					continue
				}
				hit.Score = 94
			case 1:
				if nameCore || !blobCore {
					continue
				}
				hit.Score = 90
			default:
				if nameCore || blobCore {
					continue
				}
				hit.Score = 84
			}
			seen[key] = struct{}{}
			out = append(out, hit)
			if len(out) >= limit {
				return out, "eventseye"
			}
		}
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, "eventseye"
}

func (c *Client) eventsEyeFetchURLs(keyword, term string) []string {
	base := strings.TrimRight(strings.TrimSpace(c.EventsEyeURL), "/")
	if base == "" {
		return nil
	}
	q := firstNonEmpty(term, keyword)
	themes := eventsEyeThemeSlugs(keyword, term)
	out := make([]string, 0, len(themes)+1)
	for _, slug := range themes {
		out = append(out, base+"/fairs/t1_trade-shows_"+slug+".html")
	}
	if len(themes) == 0 || looksLikeNamedFair(q) {
		out = append(out, eventsEyeSearchURL(base, q))
	}
	return uniqueFoldedStrings(out)
}

func eventsEyeThemeSlugs(keyword, term string) []string {
	blob := strings.ToLower(keyword + " " + term)
	var out []string
	for _, theme := range eventsEyeThemeCatalog {
		if containsAny(blob, theme.keys...) {
			out = append(out, theme.slug)
		}
		if len(out) >= 2 {
			break
		}
	}
	return out
}

func eventsEyeSearchURL(base, term string) string {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/cgi-bin/tsearch.pl")
	if err != nil {
		return base
	}
	q := u.Query()
	q.Set("keywords", strings.TrimSpace(term))
	q.Set("lang", "1")
	u.RawQuery = q.Encode()
	return u.String()
}

func looksLikeNamedFair(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if fairTokenRe.MatchString(s) {
		return true
	}
	return len(strings.Fields(s)) >= 2 && !hasCJK(s)
}

func parseEventsEyeFairs(raw []byte, base string) []Hit {
	html := decodeHTMLBody(raw)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var out []Hit
	seen := map[string]struct{}{}
	doc.Find("table.results tr, table.tradeshows tr").Each(func(_ int, row *goquery.Selection) {
		link := row.Find(`a[href*="f-"]`).First()
		href, _ := link.Attr("href")
		name := strings.TrimSpace(link.Find("b").First().Text())
		if name == "" {
			name = strings.TrimSpace(link.Text())
		}
		desc := strings.TrimSpace(link.Find("i").First().Text())
		home := resolveEventsEyeFairURL(base, href)
		if name == "" || home == "" || !eventsEyeFairPathRe.MatchString(home) {
			return
		}
		if _, ok := seen[home]; ok {
			return
		}
		seen[home] = struct{}{}

		cells := row.Find("td")
		venue := ""
		when := ""
		if cells.Length() >= 4 {
			venue = strings.TrimSpace(cells.Eq(2).Find("a").First().Text())
			if venue == "" {
				venue = strings.TrimSpace(cells.Eq(2).Text())
			}
			when = strings.TrimSpace(cells.Eq(3).Text())
		} else if cells.Length() >= 2 {
			venue = strings.TrimSpace(cells.Eq(1).Text())
		}
		city, nation := parseEventsEyeVenue(venue)
		start, end := parseEventsEyeDate(when)
		code, label := inferCountryFromText(nation, "")
		snippet := strings.TrimSpace(strings.Join([]string{firstNonEmpty(city, venue), firstNonEmpty(label, nation), start, clipText(desc, 140)}, " · "))
		out = append(out, Hit{
			ID:           "eventseye:" + strings.ToLower(home),
			Kind:         KindExhibition,
			Platform:     PlatformExhibition,
			Name:         name,
			Title:        name,
			HomepageURL:  home,
			MessageURL:   home,
			Country:      code,
			CountryLabel: firstNonEmpty(label, nation),
			Score:        93,
			Snippet:      snippet,
			Source:       "eventseye",
			Extra: map[string]string{
				"city":  city,
				"start": start,
				"end":   end,
				"via":   "eventseye",
			},
		})
	})
	return out
}

func resolveEventsEyeFairURL(base, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	root := strings.TrimRight(firstNonEmpty(base, defaultEventsEyeURL), "/")
	if strings.HasPrefix(href, "/fairs/") {
		return root + href
	}
	if strings.HasPrefix(href, "f-") {
		return root + "/fairs/" + href
	}
	if strings.HasPrefix(href, "/") {
		return root + href
	}
	return root + "/fairs/" + href
}

func parseEventsEyeVenue(raw string) (city, country string) {
	raw = strings.Join(strings.Fields(raw), " ")
	if raw == "" {
		return "", ""
	}
	if m := eventsEyeVenueRe.FindStringSubmatch(raw); len(m) == 3 {
		city = strings.TrimSpace(strings.Trim(m[1], ", "))
		country = strings.TrimSpace(m[2])
		return city, country
	}
	return raw, ""
}

func parseEventsEyeDate(raw string) (start, end string) {
	m := eventsEyeDateRe.FindStringSubmatch(raw)
	if len(m) != 4 {
		return "", ""
	}
	// EventsEye prints MM/DD/YYYY.
	return m[3] + "-" + m[1] + "-" + m[2], ""
}

func decodeHTMLBody(raw []byte) string {
	r, err := charset.NewReader(bytes.NewReader(raw), "text/html")
	if err != nil {
		return string(raw)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func clipText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if n <= 0 || len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
