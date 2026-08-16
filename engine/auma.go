package engine

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	aumaDateRe  = regexp.MustCompile(`(\d{2})\.(\d{2})\.(\d{4})`)
	aumaRangeRe = regexp.MustCompile(`(\d{2})\.(\d{2})\.(?:(\d{4}))?\s*[-–]\s*(\d{2})\.(\d{2})\.(\d{4})`)
)

func (c *Client) searchAUMAFairs(ctx context.Context, keyword, term, country string, limit int) ([]Hit, string) {
	if c == nil || strings.TrimSpace(c.AUMAFairURL) == "" {
		return nil, ""
	}
	raw, err := c.getHTMLReferer(ctx, aumaSearchURL(c.AUMAFairURL, firstNonEmpty(term, keyword)), "https://www.auma.de/")
	if err != nil || len(raw) == 0 {
		return nil, ""
	}
	fairs := parseAUMAFairs(raw)
	if len(fairs) == 0 {
		return nil, ""
	}
	tokens := fairSearchTokens(keyword, term)
	out := make([]Hit, 0, limit)
	for _, hit := range fairs {
		if selected := strings.ToUpper(strings.TrimSpace(country)); selected != "" && hit.Country != "" && hit.Country != selected {
			if !customsCountryMatches(hit.CountryLabel+" "+hit.Country, selected) {
				continue
			}
		}
		if len(tokens) > 0 && !fairTextMatches(hit.Name+" "+hit.Extra["city"]+" "+hit.HomepageURL, tokens) {
			continue
		}
		out = append(out, hit)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, "auma"
}

func parseAUMAFairs(raw []byte) []Hit {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return nil
	}
	var out []Hit
	doc.Find("tr.trade-fair-result__row").Each(func(_ int, s *goquery.Selection) {
		link := s.Find("a.trade-fair-result__link")
		name := strings.TrimSpace(link.Text())
		href, _ := link.Attr("href")
		if name == "" || href == "" {
			return
		}
		if !strings.HasPrefix(href, "http") {
			href = "https://www.auma.de" + href
		}
		when := strings.TrimSpace(s.Find(".trade-fair-result__cell--strTermin").Text())
		city := strings.TrimSpace(s.Find(".trade-fair-result__cell--strStadt").Text())
		nation := strings.TrimSpace(s.Find(".trade-fair-result__cell--strLand").Text())
		start, end := parseAUMADates(when)
		code, label := inferCountryFromText(nation, "")
		out = append(out, Hit{
			ID:           "auma:" + strings.ToLower(href),
			Kind:         KindExhibition,
			Platform:     PlatformExhibition,
			Name:         name,
			Title:        name,
			HomepageURL:  href,
			MessageURL:   href,
			Country:      code,
			CountryLabel: firstNonEmpty(label, nation),
			Score:        92,
			Snippet:      strings.TrimSpace(strings.Join([]string{city, nation, when}, " · ")),
			Source:       "auma",
			Extra: map[string]string{
				"city":  city,
				"start": start,
				"end":   end,
				"via":   "auma",
			},
		})
	})
	return out
}

func parseAUMADates(raw string) (start, end string) {
	raw = strings.TrimSpace(raw)
	if m := aumaRangeRe.FindStringSubmatch(raw); len(m) == 7 {
		year1 := m[3]
		if year1 == "" {
			year1 = m[6]
		}
		return year1 + "-" + m[2] + "-" + m[1], m[6] + "-" + m[5] + "-" + m[4]
	}
	if m := aumaDateRe.FindStringSubmatch(raw); len(m) == 4 {
		iso := m[3] + "-" + m[2] + "-" + m[1]
		return iso, ""
	}
	return "", ""
}

func fairTextMatches(blob string, tokens []string) bool {
	if len(tokens) == 0 {
		return true
	}
	blob = strings.ToLower(blob)
	for _, tok := range tokens {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if tok == "" {
			continue
		}
		if strings.Contains(blob, tok) {
			return true
		}
	}
	return false
}

func aumaSearchURL(base, term string) string {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return base
	}
	q := u.Query()
	if term != "" {
		q.Set("searchTerm", term)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
