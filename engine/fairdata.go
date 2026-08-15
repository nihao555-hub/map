package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

const fairCacheTTL = 6 * time.Hour

type openFair struct {
	Name      string `json:"name"`
	Website   string `json:"website"`
	City      string `json:"city"`
	Country   string `json:"country"`
	Venue     string `json:"venue"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Industry  string `json:"industry"`
	Notes     string `json:"notes"`
	Region    string `json:"region"`
}

type fairCacheEntry struct {
	at    time.Time
	shows []openFair
}

var (
	fairCacheMu sync.Mutex
	fairCache   = map[string]fairCacheEntry{}
)

func (c *Client) searchOpenFairs(ctx context.Context, keyword, term, country string, limit int) ([]Hit, string) {
	if c == nil {
		return nil, ""
	}
	urls := uniqueFoldedStrings([]string{c.FairCalendarURL, c.FairMapURL})
	var (
		all []openFair
		src []string
	)
	for _, rawURL := range urls {
		if strings.TrimSpace(rawURL) == "" {
			continue
		}
		shows, err := c.loadOpenFairs(ctx, rawURL)
		if err != nil || len(shows) == 0 {
			continue
		}
		all = append(all, shows...)
		if strings.Contains(rawURL, "world-map") {
			src = append(src, "github-fair-map")
		} else {
			src = append(src, "github-fair-calendar")
		}
	}
	if len(all) == 0 {
		return nil, ""
	}
	tokens := fairSearchTokens(keyword, term)
	out := make([]Hit, 0, limit)
	seen := map[string]struct{}{}
	for _, fair := range all {
		if !fairMatches(fair, tokens, country) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(fair.Name) + " " + fair.City)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		home := strings.TrimSpace(fair.Website)
		code, label := inferCountryFromText(fair.Country, country)
		hit := Hit{
			ID:           "openfair:" + key,
			Kind:         KindExhibition,
			Platform:     PlatformExhibition,
			Name:         fair.Name,
			Title:        fair.Name,
			HomepageURL:  home,
			MessageURL:   home,
			Country:      code,
			CountryLabel: firstNonEmpty(label, fair.Country),
			Score:        95,
			Snippet:      strings.TrimSpace(strings.Join([]string{fair.Industry, fair.City, fair.StartDate}, " · ")),
			Extra: map[string]string{
				"city":  fair.City,
				"start": fair.StartDate,
				"end":   fair.EndDate,
				"via":   "github",
			},
		}
		out = append(out, hit)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, strings.Join(uniqueStrings(src), "+")
}

func (c *Client) loadOpenFairs(ctx context.Context, rawURL string) ([]openFair, error) {
	fairCacheMu.Lock()
	if ent, ok := fairCache[rawURL]; ok && time.Since(ent.at) < fairCacheTTL {
		shows := ent.shows
		fairCacheMu.Unlock()
		return shows, nil
	}
	fairCacheMu.Unlock()

	raw, err := c.get(ctx, rawURL, map[string]string{
		"Accept":     "application/json",
		"User-Agent": "map-engine/exhibition (https://github.com/nihao555-hub/map)",
	})
	if err != nil {
		return nil, err
	}
	var shows []openFair
	if err := json.Unmarshal(raw, &shows); err != nil {
		return nil, err
	}
	fairCacheMu.Lock()
	fairCache[rawURL] = fairCacheEntry{at: time.Now(), shows: shows}
	fairCacheMu.Unlock()
	return shows, nil
}

func coreFairTokens(keyword, term string) []string {
	out := make([]string, 0, 2)
	for _, t := range []string{keyword, term} {
		t = strings.ToLower(strings.TrimSpace(t))
		if len([]rune(t)) >= 2 {
			out = append(out, t)
		}
	}
	return uniqueFoldedStrings(out)
}

func fairSearchTokens(keyword, term string) []string {
	base := []string{keyword, term}
	blob := strings.ToLower(keyword + " " + term)
	if containsAny(blob, "furniture", "家具", "sofa", "chair", "mattress", "家居") {
		base = append(base, "furniture", "家具", "ciff", "maison", "ambiente", "ligna", "canton", "interior", "consumer goods")
	}
	if containsAny(blob, "shoe", "鞋", "apparel", "fashion", "服装", "纺织") {
		base = append(base, "fashion", "textile", "intertextile", "canton", "magic")
	}
	if containsAny(blob, "led", "light", "lamp", "灯", "照明", "电子", "芯片") {
		base = append(base, "light", "electron", "ces", "computex", "ifa", "gitex", "technology")
	}
	if containsAny(blob, "food", "coffee", "食品", "咖啡", "茶", "海鲜") {
		base = append(base, "food", "sial", "anuga", "gulfood", "beverage")
	}
	if containsAny(blob, "machine", "machinery", "tool", "机械", "机床", "电机", "robot") {
		base = append(base, "manufacturing", "industrial", "hannover", "imts", "emo")
	}
	if containsAny(blob, "medical", "pharma", "hospital", "医疗", "医药", "药械") {
		base = append(base, "healthcare", "medical", "medica", "cmef", "pharmaceutical")
	}
	if containsAny(blob, "auto", "car", "vehicle", "汽车", "汽配") {
		base = append(base, "automotive", "automechanika", "iaa")
	}
	if containsAny(blob, "packag", "包装", "logistic", "物流") {
		base = append(base, "packaging", "interpack", "logistics")
	}
	if containsAny(blob, "build", "construction", "建材", "建筑", "concrete") {
		base = append(base, "construction", "bauma", "big 5")
	}
	if containsAny(blob, "beauty", "cosmetic", "美容", "化妆品") {
		base = append(base, "beauty", "cosmoprof")
	}
	if containsAny(blob, "energy", "solar", "battery", "能源", "光伏", "电池") {
		base = append(base, "energy", "intersolar", "battery")
	}
	if containsAny(blob, "广交会", "广交", "canton") {
		base = append(base, "canton", "general merchandise")
	}
	out := make([]string, 0, len(base))
	for _, t := range base {
		t = strings.ToLower(strings.TrimSpace(t))
		if len([]rune(t)) >= 2 {
			out = append(out, t)
		}
	}
	return uniqueFoldedStrings(out)
}

func fairMatches(fair openFair, tokens []string, country string) bool {
	if strings.TrimSpace(fair.Name) == "" {
		return false
	}
	if selected := strings.ToUpper(strings.TrimSpace(country)); selected != "" {
		if !customsCountryMatches(fair.Country, selected) {
			return false
		}
	}
	blob := strings.ToLower(strings.Join([]string{
		fair.Name, fair.Industry, fair.Notes, fair.City, fair.Country, fair.Region, fair.Venue,
	}, " "))
	for _, tok := range tokens {
		if tok != "" && strings.Contains(blob, tok) {
			return true
		}
	}
	return false
}

func containsAny(blob string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(blob, n) {
			return true
		}
	}
	return false
}
