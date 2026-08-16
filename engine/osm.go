package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultOverpassURL = "https://overpass-api.de/api/interpreter"
	osmHitCap          = 1200
)

var overpassMirrors = []string{
	defaultOverpassURL,
	"https://overpass.kumi.systems/api/interpreter",
}

// productShopTags maps a folded product keyword onto OSM shop=* values.
// Counts come from taginfo (lighting ~5.7k, furniture ~101k, shoes ~82k).
var productShopTags = map[string][]string{
	"led灯":         {"lighting"},
	"led light":    {"lighting"},
	"led lamp":     {"lighting"},
	"led lighting": {"lighting"},
	"照明":           {"lighting"},
	"灯饰":           {"lighting"},
	"furniture":    {"furniture"},
	"家具":           {"furniture"},
	"shoes":        {"shoes"},
	"鞋":            {"shoes"},
	"电动工具":         {"doityourself", "hardware"},
	"power tools":  {"doityourself", "hardware"},
}

func shopTagsForKeyword(keyword string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(tags []string) {
		for _, t := range tags {
			if t == "" {
				continue
			}
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	add(productShopTags[foldSearchText(keyword)])
	for _, alias := range productSearchAliases(keyword) {
		add(productShopTags[foldSearchText(alias)])
	}
	return out
}

func (c *Client) searchOSMShops(ctx context.Context, keyword, country string, wanted map[string]bool) ([]Hit, error) {
	if c == nil || strings.TrimSpace(c.OverpassURL) == "" {
		return nil, nil
	}
	tags := shopTagsForKeyword(keyword)
	if len(tags) == 0 {
		return nil, nil
	}
	raw, err := c.fetchOverpass(ctx, overpassShopQuery(tags, country))
	if err != nil {
		return nil, err
	}
	return parseOverpassShops(raw, keyword, country, wanted), nil
}

func overpassShopQuery(tags []string, country string) string {
	var b strings.Builder
	b.WriteString("[out:json][timeout:25];\n")
	area := strings.ToUpper(strings.TrimSpace(LookupCountry(country).Code))
	if area != "" {
		fmt.Fprintf(&b, `area["ISO3166-1"=%q][admin_level=2]->.a;`, area)
		b.WriteByte('\n')
	}
	b.WriteString("(\n")
	for _, tag := range tags {
		if area != "" {
			fmt.Fprintf(&b, `  node["shop"=%q]["name"](area.a);`+"\n", tag)
		} else {
			fmt.Fprintf(&b, `  node["shop"=%q]["name"];`+"\n", tag)
		}
	}
	b.WriteString(");\n")
	fmt.Fprintf(&b, "out tags %d;\n", osmHitCap)
	return b.String()
}

func (c *Client) fetchOverpass(ctx context.Context, query string) ([]byte, error) {
	endpoints := []string{c.OverpassURL}
	if c.OverpassURL == defaultOverpassURL {
		endpoints = append(endpoints, overpassMirrors...)
	}
	form := "data=" + url.QueryEscape(query)
	var last error
	for _, ep := range endpoints {
		if strings.TrimSpace(ep) == "" {
			continue
		}
		raw, err := c.postFormRaw(ctx, ep, form, map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"Accept":       "application/json",
			"User-Agent":   "map-engine/osm (https://github.com/nihao555-hub/map)",
		})
		if err != nil {
			last = err
			continue
		}
		if len(raw) < 20 || !strings.Contains(string(raw), `"elements"`) {
			last = fmt.Errorf("overpass: empty")
			continue
		}
		return raw, nil
	}
	if last == nil {
		last = fmt.Errorf("overpass: no endpoint")
	}
	return nil, last
}

func (c *Client) postFormRaw(ctx context.Context, rawURL, form string, extra map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "map-engine/osm")
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	return c.do(req)
}

type overpassDoc struct {
	Elements []overpassEl `json:"elements"`
}

type overpassEl struct {
	Type string            `json:"type"`
	ID   int64             `json:"id"`
	Tags map[string]string `json:"tags"`
}

func parseOverpassShops(raw []byte, keyword, country string, wanted map[string]bool) []Hit {
	var doc overpassDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	out := make([]Hit, 0, len(doc.Elements))
	for _, el := range doc.Elements {
		hits := osmElementHits(el, keyword, country, wanted)
		for i := range hits {
			if hits[i].Extra != nil {
				hits[i].Extra["q"] = keyword
			}
		}
		out = append(out, hits...)
	}
	return out
}

func osmElementHits(el overpassEl, keyword, country string, wanted map[string]bool) []Hit {
	tags := el.Tags
	if tags == nil {
		return nil
	}
	name := firstNonEmpty(tags["name"], tags["name:en"], tags["name:zh"], tags["operator"])
	if strings.TrimSpace(name) == "" {
		return nil
	}
	shop := tags["shop"]
	city := firstNonEmpty(tags["addr:city"], tags["addr:town"], tags["addr:suburb"])
	cc := strings.ToUpper(strings.TrimSpace(firstNonEmpty(tags["addr:country"], LookupCountry(country).Code)))
	_, label := inferCountryFromText(cc, country)
	snippet := strings.TrimSpace(strings.Join([]string{
		shop + " shop", "店铺", city, label,
	}, " · "))
	base := Hit{
		Kind:         KindPeople,
		Name:         name,
		Title:        name,
		Snippet:      snippet,
		Score:        88,
		Country:      cc,
		CountryLabel: label,
		Extra: map[string]string{
			"src":   "osm",
			"shop":  shop,
			"match": "category",
		},
	}

	var out []Hit
	addSocial := func(raw, site string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		candidates := []string{normalizeOSMContact(raw)}
		if !strings.Contains(raw, "://") && !strings.Contains(raw, "/") {
			candidates = append(candidates, "https://"+site+"/"+strings.TrimPrefix(raw, "@"))
		}
		var hit Hit
		ok := false
		for _, cand := range candidates {
			hit, ok = ParseSocialURL(cand, name, snippet)
			if ok {
				break
			}
		}
		if !ok {
			return
		}
		if len(wanted) > 0 && !wanted[hit.Platform] {
			return
		}
		hit.Name = name
		hit.Title = name
		hit.Snippet = snippet
		hit.Score = 92
		hit.Country = cc
		hit.CountryLabel = label
		if hit.Extra == nil {
			hit.Extra = map[string]string{}
		}
		hit.Extra["src"] = "osm"
		hit.Extra["shop"] = shop
		hit.Extra["match"] = "category"
		out = append(out, hit)
	}
	addSocial(firstNonEmpty(tags["contact:facebook"], tags["facebook"]), "www.facebook.com")
	addSocial(firstNonEmpty(tags["contact:instagram"], tags["instagram"]), "www.instagram.com")
	addSocial(firstNonEmpty(tags["contact:linkedin"], tags["linkedin"]), "www.linkedin.com/company")
	addSocial(firstNonEmpty(tags["contact:twitter"], tags["twitter"], tags["contact:x"]), "x.com")

	home := firstNonEmpty(tags["website"], tags["contact:website"], tags["url"])
	if home == "" {
		if el.Type == "" {
			el.Type = "node"
		}
		home = fmt.Sprintf("https://www.openstreetmap.org/%s/%d", el.Type, el.ID)
	} else if !strings.Contains(home, "://") {
		home = "https://" + strings.TrimPrefix(home, "//")
	}
	web := base
	web.ID = "osm:" + el.Type + ":" + strconv.FormatInt(el.ID, 10)
	web.Platform = PlatformWebsite
	web.HomepageURL = home
	web.MessageURL = home
	web.MessageHint = "打开商家官网或 OpenStreetMap 页。系统不会代发。"
	out = append(out, web)
	return out
}

func normalizeOSMContact(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") || strings.Contains(raw, ".") {
		if !strings.Contains(raw, "://") {
			return "https://" + strings.TrimPrefix(raw, "//")
		}
		return raw
	}
	return raw
}
