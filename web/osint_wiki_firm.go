package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	wikiInfoboxRe   = regexp.MustCompile(`(?is)<table[^>]*class="[^"]*infobox[^"]*"[^>]*>[\s\S]*?</table>`)
	wikiInfoboxRowRe = regexp.MustCompile(`(?is)<tr[^>]*>\s*<th[^>]*>([\s\S]*?)</th>\s*<td[^>]*>([\s\S]*?)</td>`)
	wikiMoneyRe     = regexp.MustCompile(`(?i)(?:US)?\$?\s*([0-9]+(?:\.[0-9]+)?)\s*(million|billion|trillion)?`)
	wikiYearRe      = regexp.MustCompile(`\((20[0-9]{2}|19[0-9]{2})\)`)
	wikiEmployeesRe = regexp.MustCompile(`(?i)([0-9][0-9,]*)\s*(?:\((20[0-9]{2})\))?`)
)

// lookupWikipediaFirmographics parses the English Wikipedia infobox for revenue / employees.
// Used when SEC EDGAR is blocked (common on cloud/datacenter IPs).
func lookupWikipediaFirmographics(ctx context.Context, title string) (*Firmographics, []DecisionMaker, error) {
	title = strings.TrimSpace(title)
	if title == "" || !IdentifiableCompanyName(title) {
		return nil, nil, fmt.Errorf("wiki firmographics: empty title")
	}
	// Resolve page title via opensearch, then fetch HTML.
	searchURL := "https://en.wikipedia.org/w/api.php?action=opensearch&search=" + url.QueryEscape(title) +
		"&limit=5&namespace=0&format=json"
	raw, err := httpGetJSON(ctx, searchURL, 8*time.Second)
	if err != nil {
		return nil, nil, err
	}
	var parsed []any
	if json.Unmarshal(raw, &parsed) != nil || len(parsed) < 2 {
		return nil, nil, fmt.Errorf("wiki opensearch")
	}
	titles, _ := parsed[1].([]any)
	page := ""
	for _, item := range titles {
		s, _ := item.(string)
		if s == "" {
			continue
		}
		if ExternalRecordMatchesBusiness(s, title) || strings.EqualFold(s, title) {
			page = s
			break
		}
	}
	if page == "" && len(titles) > 0 {
		if s, _ := titles[0].(string); s != "" && ExternalRecordMatchesBusiness(s, title) {
			page = s
		}
	}
	if page == "" {
		return nil, nil, fmt.Errorf("wiki: no page for %q", title)
	}
	u := "https://en.wikipedia.org/wiki/" + url.PathEscape(strings.ReplaceAll(page, " ", "_"))
	// PathEscape leaves underscore; decode slash if any
	u = strings.ReplaceAll(u, "%2F", "/")
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "gmaps-intel/1.0 (research; wikipedia-infobox)")
	req.Header.Set("Accept", "text/html")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	htmlBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 400 {
		return nil, nil, fmt.Errorf("wiki status %d", resp.StatusCode)
	}
	box := wikiInfoboxRe.Find(htmlBody)
	if len(box) == 0 {
		return nil, nil, fmt.Errorf("wiki: no infobox")
	}
	fields := map[string]string{}
	for _, m := range wikiInfoboxRowRe.FindAllSubmatch(box, -1) {
		if len(m) < 3 {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(stripTags(string(m[1]))))
		val := strings.TrimSpace(stripTags(string(m[2])))
		val = regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(val, "")
		val = strings.Join(strings.Fields(val), " ")
		if label == "" || val == "" {
			continue
		}
		fields[label] = val
	}
	fg := &Firmographics{
		LegalName: page,
		Source:    "wikipedia",
		FilingURL: u,
	}
	if rev := firstNonEmpty(fields["revenue"], fields["turnover"]); rev != "" {
		if usd, year, ok := parseWikiMoney(rev); ok {
			fg.RevenueUSD = usd
			fg.RevenueYear = year
		}
	}
	if emp := fields["number of employees"]; emp != "" {
		if n, year, ok := parseWikiEmployees(emp); ok {
			fg.Employees = n
			fg.EmployeesAsOf = year
		}
	}
	var makers []DecisionMaker
	if founders := firstNonEmpty(fields["founders"], fields["founder"]); founders != "" {
		for _, name := range splitWikiPeople(founders) {
			if !isLikelyPersonName(name) && len(strings.Fields(name)) < 2 {
				continue
			}
			makers = append(makers, DecisionMaker{
				Name: name, Title: "Founder", Source: "wikipedia",
				Evidence:   u + "#infobox",
				Confidence: "medium",
				LinkedIn: "https://www.linkedin.com/search/results/people/?keywords=" +
					url.QueryEscape(name+" "+title),
			})
		}
	}
	if key := fields["key people"]; key != "" {
		for _, name := range splitWikiPeople(key) {
			if !isLikelyPersonName(name) && len(strings.Fields(name)) < 2 {
				continue
			}
			makers = append(makers, DecisionMaker{
				Name: name, Title: "Key person", Source: "wikipedia",
				Evidence:   u + "#infobox",
				Confidence: "medium",
				LinkedIn: "https://www.linkedin.com/search/results/people/?keywords=" +
					url.QueryEscape(name+" "+title),
			})
		}
	}
	if fg.RevenueUSD == 0 && fg.Employees == 0 && len(makers) == 0 {
		return nil, nil, fmt.Errorf("wiki: empty firmographics")
	}
	return fg, makers, nil
}

func parseWikiMoney(s string) (int64, string, bool) {
	m := wikiMoneyRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0, "", false
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, "", false
	}
	mult := 1.0
	switch strings.ToLower(m[2]) {
	case "million":
		mult = 1e6
	case "billion":
		mult = 1e9
	case "trillion":
		mult = 1e12
	}
	year := ""
	if ym := wikiYearRe.FindStringSubmatch(s); len(ym) > 1 {
		year = ym[1]
	}
	return int64(f * mult), year, true
}

func parseWikiEmployees(s string) (int, string, bool) {
	m := wikiEmployeesRe.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0, "", false
	}
	n, err := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	if err != nil || n <= 0 {
		return 0, "", false
	}
	year := ""
	if len(m) > 2 {
		year = m[2]
	}
	return n, year, true
}

func splitWikiPeople(s string) []string {
	s = strings.ReplaceAll(s, "·", ",")
	s = strings.ReplaceAll(s, ";", ",")
	s = strings.ReplaceAll(s, " and ", ",")
	s = strings.ReplaceAll(s, "&", ",")
	parts := strings.Split(s, ",")
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, ".")
		if i := strings.Index(p, "("); i > 0 {
			p = strings.TrimSpace(p[:i])
		}
		if p == "" || seen[strings.ToLower(p)] {
			return
		}
		if strings.Contains(p, "{") || strings.Contains(strings.ToLower(p), "mw-parser") {
			return
		}
		if len(p) < 3 || len(p) > 60 {
			return
		}
		seen[strings.ToLower(p)] = true
		out = append(out, p)
	}
	twoPeopleRe := regexp.MustCompile(`^([A-Z][a-z]+(?:\s+[A-Z][a-z]+)+)\s+([A-Z][a-z]+(?:\s+[A-Z][a-z]+)+)$`)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if m := twoPeopleRe.FindStringSubmatch(p); len(m) == 3 {
			add(m[1])
			add(m[2])
			continue
		}
		add(p)
		if len(out) >= 6 {
			break
		}
	}
	return out
}
