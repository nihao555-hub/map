package engine

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"unicode"
)

type usitcRow struct {
	Htsno       string `json:"htsno"`
	Description string `json:"description"`
}

func (c *Client) lookupHS4(ctx context.Context, keyword string) string {
	if c == nil || strings.TrimSpace(c.USITCURL) == "" {
		return ""
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" || looksLikeHS(keyword) {
		return hs4Digits(keyword)
	}
	u, err := url.Parse(c.USITCURL)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("keyword", keyword)
	u.RawQuery = q.Encode()
	raw, err := c.get(ctx, u.String(), map[string]string{
		"Accept":     "application/json",
		"User-Agent": browserUA,
	})
	if err != nil {
		return ""
	}
	var rows []usitcRow
	if json.Unmarshal(raw, &rows) != nil || len(rows) == 0 {
		return ""
	}
	return pickHS4(keyword, rows)
}

func pickHS4(keyword string, rows []usitcRow) string {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	scores := map[string]int{}
	for _, row := range rows {
		digits := hsDigits(row.Htsno)
		if len(digits) < 4 {
			continue
		}
		hs4 := digits[:4]
		if strings.HasPrefix(hs4, "98") || strings.HasPrefix(hs4, "99") {
			continue
		}
		n := 1
		if kw != "" && strings.Contains(strings.ToLower(row.Description), kw) {
			n += 4
		}
		scores[hs4] += n
	}
	best, bestN := "", 0
	for hs4, n := range scores {
		if n > bestN || (n == bestN && (best == "" || hs4 < best)) {
			best, bestN = hs4, n
		}
	}
	return best
}

func hs4Digits(s string) string {
	digits := hsDigits(s)
	if len(digits) >= 4 {
		return digits[:4]
	}
	return digits
}

func hsDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
