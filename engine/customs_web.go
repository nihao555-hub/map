package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

var (
	customsBrandSplitRe = regexp.MustCompile(`\s*[-–|•]\s*`)
	customsNoiseTitleRe = regexp.MustCompile(`(?i)\b(shoes?|buy|shop|store|official|home|wikipedia|amazon\.com|search)\b`)
)

func (c *Client) searchCustomsWeb(ctx context.Context, term, role, country string, year, limit int) ([]Hit, string) {
	if c == nil || c.DisablePublic || strings.TrimSpace(term) == "" {
		return nil, ""
	}
	queries := customsWebQueries(term, role)
	var (
		mu    sync.Mutex
		names []string
		hits  []Hit
		srcs  []string
	)
	addNames := func(extra []string, src string) {
		mu.Lock()
		defer mu.Unlock()
		if src != "" {
			srcs = append(srcs, src)
		}
		names = append(names, extra...)
	}
	addHits := func(extra []Hit, src string) {
		mu.Lock()
		defer mu.Unlock()
		if src != "" && len(extra) > 0 {
			srcs = append(srcs, src)
		}
		hits = append(hits, extra...)
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(3)
	for _, q := range queries {
		q := q
		g.Go(func() error {
			batch, src, err := c.searchOneIndexExtract(gctx, q, extractOrganicResults, "")
			if err != nil {
				return nil
			}
			var found []string
			for _, h := range batch {
				if iy := importYetiHitsFromWebHit(h, role, country); len(iy) > 0 {
					addHits(iy, "importyeti-web")
				}
				found = append(found, companyCandidatesFromHit(h)...)
			}
			addNames(found, src)
			return nil
		})
	}
	_ = g.Wait()

	cands := uniqueFoldedStrings(names)
	if limit > 0 && len(cands) > customsHydrateMax {
		cands = cands[:customsHydrateMax]
	}
	hydrated := c.hydrateCustomsNames(ctx, cands, country, year)
	if len(hydrated) > 0 {
		hits = append(hits, hydrated...)
		srcs = append(srcs, "kirchner")
	}
	return mergeCustomsHits(hits, limit), strings.Join(uniqueStrings(srcs), "+")
}

func customsWebQueries(term, role string) []string {
	term = strings.TrimSpace(term)
	out := []string{
		term + " US importer consignee",
		`site:importyeti.com ` + term,
		term + ` "INC" importer`,
	}
	if role == RoleSeller {
		out = []string{
			term + " supplier shipper exporter",
			`site:importyeti.com/supplier ` + term,
		}
	}
	return uniqueFoldedStrings(out)
}

func importYetiHitsFromWebHit(h Hit, role, country string) []Hit {
	kind, slug := importYetiSlug(h.HomepageURL)
	if slug == "" {
		return nil
	}
	rowType := kind
	if role == RoleBuyer && rowType == "supplier" {
		return nil
	}
	if role == RoleSeller && rowType == "company" {
		return nil
	}
	name := firstNonEmpty(cleanImportYetiTitle(h.Name), importYetiNameFromSlug(slug))
	row := importYetiRow{
		Name: name,
		Type: rowType,
		URL:  "https://www.importyeti.com/" + rowType + "/" + slug,
	}
	return importYetiHits([]importYetiRow{row}, role, country, 1)
}

func companyCandidatesFromHit(h Hit) []string {
	title := strings.TrimSpace(firstNonEmpty(h.Name, h.Title))
	if title == "" {
		return nil
	}
	var out []string
	if looksLikeCompanyName(title) {
		out = append(out, strings.ToUpper(title))
	}
	parts := customsBrandSplitRe.Split(title, -1)
	brand := strings.TrimSpace(parts[len(parts)-1])
	brand = strings.TrimSpace(strings.Split(brand, ":")[0])
	brand = strings.Trim(brand, " .")
	if cand := brandToConsignee(brand); cand != "" {
		out = append(out, cand)
	}
	if len(parts) > 1 {
		if cand := brandToConsignee(strings.TrimSpace(parts[0])); cand != "" {
			out = append(out, cand)
		}
	}
	return uniqueFoldedStrings(out)
}

func brandToConsignee(brand string) string {
	brand = strings.TrimSpace(brand)
	brand = strings.TrimSuffix(brand, "®")
	if brand == "" || looksLikeHS(brand) {
		return ""
	}
	if customsNoiseTitleRe.MatchString(brand) && !looksLikeCompanyName(brand) {
		words := strings.Fields(brand)
		if len(words) < 2 || len(words) > 4 {
			return ""
		}
	}
	up := strings.ToUpper(brand)
	up = strings.ReplaceAll(up, "'S", "S")
	up = strings.ReplaceAll(up, "’S", "S")
	up = strings.Trim(up, " .,")
	switch up {
	case "", "SHOES", "AMAZON.COM", "AMAZON", "SEARCH", "HOME", "OFFICIAL":
		return ""
	}
	if looksLikeCompanyName(up) {
		return up
	}
	words := strings.Fields(up)
	if len(words) == 0 || len(words) > 4 {
		return ""
	}
	for _, w := range words {
		if len(w) < 2 {
			return ""
		}
	}
	if !strings.HasSuffix(up, " INC") && !strings.HasSuffix(up, " LLC") && !strings.HasSuffix(up, " CORP") {
		up += " INC"
	}
	return up
}

func (c *Client) hydrateCustomsNames(ctx context.Context, names []string, country string, year int) []Hit {
	if c == nil || strings.TrimSpace(c.CustomsBaseURL) == "" {
		return nil
	}
	if !customsCountryIsUS(country) && strings.TrimSpace(country) != "" {
		return nil
	}
	names = uniqueFoldedStrings(names)
	if len(names) > customsHydrateMax {
		names = names[:customsHydrateMax]
	}
	var (
		mu  sync.Mutex
		out []Hit
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(customsHydrateParallel)
	for _, name := range names {
		name := name
		if name == "" || looksLikeHS(name) || !looksLikeCompanyName(name) {
			continue
		}
		g.Go(func() error {
			prof, err := c.lookupKirchnerProfile(gctx, name, year)
			if err != nil || strings.TrimSpace(prof.Name) == "" || prof.TotalShipments == 0 {
				return nil
			}
			hit := hitFromCustomsProfile(prof, year)
			if hit.Extra == nil {
				hit.Extra = map[string]string{}
			}
			hit.Extra["via"] = "kirchner"
			mu.Lock()
			out = append(out, hit)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return out
}

func (c *Client) sellersFromBuyerHits(ctx context.Context, buyers []Hit, country string, year, limit int) []Hit {
	if c == nil || strings.TrimSpace(c.CustomsBaseURL) == "" {
		return nil
	}
	seen := map[string]Hit{}
	order := make([]string, 0)
	add := func(name, origin string, shipments int) {
		name = strings.TrimSpace(name)
		if name == "" || strings.EqualFold(name, "n/a") {
			return
		}
		if !customsCountryMatches(origin, country) && strings.TrimSpace(country) != "" {
			return
		}
		key := strings.ToLower(name)
		if prev, ok := seen[key]; ok {
			if shipments > jsonInt(prev.Extra["shipments"]) {
				prev.Extra["shipments"] = strconv.Itoa(shipments)
				seen[key] = prev
			}
			return
		}
		code, label := inferCountryFromText(origin, country)
		seen[key] = Hit{
			ID:           "customs-seller:" + key,
			Kind:         KindCustoms,
			Platform:     PlatformCustoms,
			Name:         name,
			Title:        name,
			Role:         RoleSeller,
			Country:      code,
			CountryLabel: firstNonEmpty(label, origin),
			Score:        70 + shipments,
			Snippet:      strings.TrimSpace("美国进口提单中的发货人 / 供应商。 " + origin),
			Extra: map[string]string{
				"shipments": strconv.Itoa(shipments),
				"origin":    origin,
				"year":      strconv.Itoa(year),
				"via":       "kirchner",
			},
		}
		order = append(order, key)
	}
	n := 0
	for _, buyer := range buyers {
		if n >= 6 {
			break
		}
		if strings.TrimSpace(buyer.Name) == "" {
			continue
		}
		prof, err := c.lookupKirchnerProfile(ctx, buyer.Name, year)
		if err != nil {
			continue
		}
		n++
		for _, p := range prof.Suppliers {
			add(p.Name, p.Country, p.Shipments)
		}
	}
	out := make([]Hit, 0, len(order))
	for _, key := range order {
		out = append(out, seen[key])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (c *Client) productTrendsNote(ctx context.Context, keyword string, year int) string {
	if c == nil || strings.TrimSpace(c.CustomsBaseURL) == "" || strings.TrimSpace(keyword) == "" {
		return ""
	}
	u, err := url.Parse(strings.TrimRight(c.CustomsBaseURL, "/") + "/api/product-trends")
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("keyword", keyword)
	q.Set("year", strconv.Itoa(year))
	u.RawQuery = q.Encode()
	raw, err := c.get(ctx, u.String(), kirchnerHeaders())
	if err != nil {
		return ""
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	annual, _ := doc["annual"].(map[string]any)
	if annual == nil {
		return ""
	}
	count := jsonInt(annual[strconv.Itoa(year)])
	if count == 0 {
		count = jsonInt(annual[strconv.Itoa(year-1)])
		if count > 0 {
			year = year - 1
		}
	}
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf("Kirchner 公开提单「%s」%d 年约 %s 票（实时计数，不是企业名单）。", keyword, year, withThousands(strconv.Itoa(count)))
}

func mergeCustomsHits(items []Hit, limit int) []Hit {
	seen := map[string]Hit{}
	order := make([]string, 0, len(items))
	for _, hit := range items {
		if strings.TrimSpace(hit.Name) == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(hit.Name))
		if prev, ok := seen[key]; ok {
			if jsonInt(hit.Extra["shipments"]) > jsonInt(prev.Extra["shipments"]) || hit.Score > prev.Score {
				if hit.Extra == nil {
					hit.Extra = map[string]string{}
				}
				for k, v := range prev.Extra {
					if hit.Extra[k] == "" {
						hit.Extra[k] = v
					}
				}
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
