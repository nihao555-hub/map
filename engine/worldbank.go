package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const defaultWorldBankURL = "https://api.worldbank.org/v2"

type worldBankPoint struct {
	Country struct {
		ID    string `json:"id"`
		Value string `json:"value"`
	} `json:"country"`
	Date  string   `json:"date"`
	Value *float64 `json:"value"`
}

func (c *Client) worldBankTradeNote(ctx context.Context, country, role string) string {
	if c == nil || strings.TrimSpace(c.WorldBankURL) == "" {
		return ""
	}
	code := strings.ToUpper(strings.TrimSpace(country))
	if code == "" {
		if NormalizeRole(role) == RoleBuyer {
			code = "US"
		} else {
			return ""
		}
	}
	indicator := "TM.VAL.MRCH.CD.WT"
	label := "商品进口"
	if NormalizeRole(role) == RoleSeller {
		indicator = "TX.VAL.MRCH.CD.WT"
		label = "商品出口"
	}
	pt, err := c.fetchWorldBankIndicator(ctx, code, indicator)
	if err != nil || pt.Value == nil || *pt.Value <= 0 {
		return ""
	}
	name := firstNonEmpty(pt.Country.Value, LookupCountry(code).Label, code)
	return fmt.Sprintf("世界银行：%s %s %s %s（国家口径，不是逐票企业）。",
		name, pt.Date, label, formatUSDAmount(*pt.Value))
}

func (c *Client) fetchWorldBankIndicator(ctx context.Context, iso2, indicator string) (worldBankPoint, error) {
	iso2 = strings.ToLower(strings.TrimSpace(iso2))
	if iso2 == "" || iso2 == "uk" {
		if iso2 == "uk" {
			iso2 = "gb"
		}
	}
	u, err := url.Parse(strings.TrimRight(c.WorldBankURL, "/") + "/country/" + iso2 + "/indicator/" + indicator)
	if err != nil {
		return worldBankPoint{}, err
	}
	q := u.Query()
	q.Set("format", "json")
	q.Set("mrv", "1")
	u.RawQuery = q.Encode()
	raw, err := c.get(ctx, u.String(), map[string]string{
		"Accept":     "application/json",
		"User-Agent": "map-engine/worldbank (https://github.com/nihao555-hub/map)",
	})
	if err != nil {
		return worldBankPoint{}, err
	}
	var doc []json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc) < 2 {
		return worldBankPoint{}, fmt.Errorf("worldbank: unexpected payload")
	}
	var rows []worldBankPoint
	if err := json.Unmarshal(doc[1], &rows); err != nil {
		return worldBankPoint{}, err
	}
	for _, row := range rows {
		if row.Value != nil && *row.Value > 0 {
			return row, nil
		}
	}
	return worldBankPoint{}, fmt.Errorf("worldbank: empty")
}
