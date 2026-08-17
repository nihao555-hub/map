package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	comtradeUSAReporter = "842"
	comtradeMaxPartners = 8
)

type comtradePreview struct {
	Count int              `json:"count"`
	Data  []comtradeRecord `json:"data"`
}

type comtradeRecord struct {
	PartnerCode  int     `json:"partnerCode"`
	PartnerISO   string  `json:"partnerISO"`
	PartnerDesc  string  `json:"partnerDesc"`
	CmdCode      string  `json:"cmdCode"`
	CmdDesc      string  `json:"cmdDesc"`
	Period       string  `json:"period"`
	PrimaryValue float64 `json:"primaryValue"`
}

func (c *Client) searchComtradeOrigins(ctx context.Context, hs4 string, year int, country string) ([]comtradeRecord, error) {
	return c.fetchComtrade(ctx, hs4, year, country, comtradeUSAReporter, "M")
}

func (c *Client) fetchComtrade(ctx context.Context, hs4 string, year int, country, reporter, flow string) ([]comtradeRecord, error) {
	if c == nil || strings.TrimSpace(c.ComtradeURL) == "" || hs4 == "" || reporter == "" {
		return nil, nil
	}
	period := comtradePeriod(year)
	u, err := url.Parse(strings.TrimRight(c.ComtradeURL, "/") + "/C/A/HS")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("reporterCode", reporter)
	q.Set("period", strconv.Itoa(period))
	q.Set("cmdCode", hs4)
	q.Set("flowCode", flow)
	q.Set("maxRecords", "100")
	q.Set("includeDesc", "true")
	u.RawQuery = q.Encode()

	raw, err := c.get(ctx, u.String(), map[string]string{
		"Accept":     "application/json",
		"User-Agent": "map-engine/comtrade (https://github.com/nihao555-hub/map)",
	})
	if err != nil {
		return nil, err
	}
	var doc comtradePreview
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make([]comtradeRecord, 0, len(doc.Data))
	for _, row := range doc.Data {
		if row.PartnerCode == 0 || row.PrimaryValue <= 0 {
			continue
		}
		desc := strings.ToLower(row.PartnerDesc)
		if desc == "" || strings.Contains(desc, "world") || strings.Contains(desc, "nes") ||
			strings.Contains(desc, "areas") {
			continue
		}
		if !customsCountryMatches(row.PartnerDesc+" "+row.PartnerISO, country) && strings.TrimSpace(country) != "" {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PrimaryValue > out[j].PrimaryValue })
	if len(out) > comtradeMaxPartners {
		out = out[:comtradeMaxPartners]
	}
	return out, nil
}

func comtradePeriod(year int) int {
	now := time.Now().UTC().Year()
	if year >= 2015 && year <= now-1 {
		return year
	}
	if now-2 >= 2015 {
		return now - 2
	}
	return now - 1
}

func comtradeSellerHits(rows []comtradeRecord, hs4 string, year int) []Hit {
	out := make([]Hit, 0, len(rows))
	for _, row := range rows {
		code, label := inferCountryFromText(row.PartnerDesc, "")
		if label == "" {
			label = row.PartnerDesc
		}
		amt := formatUSDAmount(row.PrimaryValue)
		hit := Hit{
			ID:           "comtrade:" + strings.ToLower(firstNonEmpty(row.PartnerISO, row.PartnerDesc)),
			Kind:         KindCustoms,
			Platform:     PlatformCustoms,
			Name:         label + "（货源国）",
			Title:        label + "（货源国）",
			Role:         RoleSeller,
			Country:      code,
			CountryLabel: label,
			Score:        50,
			Snippet:      "联合国 Comtrade 国家口径贸易额，不是逐票企业。",
			Extra: map[string]string{
				"hs":         firstNonEmpty(row.CmdCode, hs4),
				"product":    row.CmdDesc,
				"amount_usd": amt,
				"year":       firstNonEmpty(row.Period, strconv.Itoa(year)),
				"via":        "comtrade",
			},
		}
		out = append(out, hit)
	}
	return out
}

func comtradeNote(rows []comtradeRecord, hs4 string) string {
	if len(rows) == 0 {
		return ""
	}
	parts := make([]string, 0, 5)
	for i, row := range rows {
		if i >= 5 {
			break
		}
		name := firstNonEmpty(row.PartnerDesc, row.PartnerISO)
		parts = append(parts, name+" "+formatUSDAmount(row.PrimaryValue))
	}
	hs := firstNonEmpty(rows[0].CmdCode, hs4)
	year := rows[0].Period
	return fmt.Sprintf("Comtrade %s 美国进口 HS%s 主要来源：%s。这是联合国商品贸易统计（国家口径），不是逐票企业库。",
		year, hs, strings.Join(parts, "、"))
}

func formatUSDAmount(v float64) string {
	n := int64(v + 0.5)
	if n < 0 {
		n = 0
	}
	s := strconv.FormatInt(n, 10)
	return "$" + withThousands(s)
}

func withThousands(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}
	pre := n % 3
	if pre == 0 {
		pre = 3
	}
	var b strings.Builder
	b.WriteString(s[:pre])
	for i := pre; i < n; i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
}
