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
	osmHitCap          = 80
	osmBoxCap          = 16
)

type osmBox struct {
	country string
	south   float64
	west    float64
	north   float64
	east    float64
}

// City boxes keep public Overpass queries small enough to finish.
var osmShopBoxes = []osmBox{
	{"DE", 52.35, 13.10, 52.68, 13.77},     // Berlin
	{"DE", 48.05, 11.40, 48.25, 11.72},     // Munich
	{"DE", 53.45, 9.85, 53.65, 10.15},      // Hamburg
	{"US", 40.65, -74.10, 40.85, -73.85},   // New York
	{"US", 33.90, -118.50, 34.20, -118.10}, // Los Angeles
	{"GB", 51.40, -0.25, 51.60, 0.05},      // London
	{"FR", 48.80, 2.20, 48.95, 2.45},       // Paris
	{"IT", 45.40, 9.10, 45.55, 9.30},       // Milan
	{"CN", 31.10, 121.30, 31.40, 121.60},   // Shanghai
	{"MY", 3.00, 101.50, 3.30, 101.85},     // Kuala Lumpur
	{"TH", 13.60, 100.40, 13.90, 100.70},   // Bangkok
	{"NL", 52.30, 4.80, 52.45, 5.00},       // Amsterdam
	{"SG", 1.22, 103.60, 1.47, 104.04},     // Singapore
	{"ID", -6.35, 106.70, -6.10, 106.98},   // Jakarta
	{"ID", -7.32, 112.70, -7.22, 112.80},   // Surabaya
	{"ID", -6.95, 107.57, -6.87, 107.65},   // Bandung
	{"ID", 3.55, 98.64, 3.63, 98.72},       // Medan
	{"ID", -8.72, 115.17, -8.63, 115.26},   // Denpasar
	{"ID", -7.02, 110.38, -6.95, 110.46},   // Semarang
	{"ID", -5.18, 119.38, -5.10, 119.46},   // Makassar
	{"VN", 10.75, 106.65, 10.83, 106.75},   // Ho Chi Minh
	{"VN", 21.00, 105.80, 21.08, 105.90},   // Hanoi
	{"PH", 14.55, 120.96, 14.70, 121.10},   // Manila
	{"KH", 11.52, 104.88, 11.60, 104.96},   // Phnom Penh
}

var overpassMirrors = []string{
	defaultOverpassURL,
	"https://overpass.openstreetmap.fr/api/interpreter",
	"https://overpass.kumi.systems/api/interpreter",
}

// productShopTags maps a folded product keyword onto OSM shop=* values.
// Counts come from taginfo (lighting ~5.7k, furniture ~101k, shoes ~82k).
var productShopTags = map[string][]string{
	"led灯":               {"lighting"},
	"led light":          {"lighting"},
	"led lamp":           {"lighting"},
	"led lighting":       {"lighting"},
	"照明":                 {"lighting"},
	"灯饰":                 {"lighting"},
	"furniture":          {"furniture"},
	"家具":                 {"furniture"},
	"shoes":              {"shoes"},
	"鞋":                  {"shoes"},
	"鞋子":                 {"shoes"},
	"电动工具":               {"doityourself", "hardware"},
	"power tools":        {"doityourself", "hardware"},
	"配电柜":                {"electrical"},
	"配电箱":                {"electrical"},
	"配电盘":                {"electrical"},
	"配电":                 {"electrical"},
	"开关柜":                {"electrical"},
	"switchgear":         {"electrical"},
	"electrical panel":   {"electrical"},
	"distribution board": {"electrical"},
	"panel listrik":      {"electrical"},
	"lemari listrik":     {"electrical"},
	"便利店":                {"convenience"},
	"超市":                 {"supermarket"},
	"服装":                 {"clothes"},
	"衣服":                 {"clothes"},
	"clothes":            {"clothes"},
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
	names := osmNameNeedles(keyword)
	if len(tags) == 0 && len(names) == 0 {
		return nil, nil
	}
	var (
		out  []Hit
		last error
	)
	for _, box := range osmQueryBoxes(country) {
		if ctx.Err() != nil {
			break
		}
		raw, err := c.fetchOverpass(ctx, overpassShopOrNameQuery(tags, names, box))
		if err != nil {
			last = err
			continue
		}
		out = append(out, parseOverpassShops(raw, keyword, box.country, wanted)...)
	}
	if len(out) == 0 {
		return nil, last
	}
	return out, nil
}

// osmNameNeedles are Latin product phrases used when shop=* is missing or
// too narrow (Indonesian electrical shops are often shop=electronics named
// "toko listrik", not shop=electrical).
func osmNameNeedles(keyword string) []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || hasCJK(s) || len(s) < 4 {
			return
		}
		out = append(out, s)
	}
	add(keyword)
	for _, alias := range productSearchAliases(keyword) {
		add(alias)
	}
	return uniqueFoldedStrings(out)
}

func osmQueryBoxes(country string) []osmBox {
	want := strings.ToUpper(strings.TrimSpace(LookupCountry(country).Code))
	out := make([]osmBox, 0, osmBoxCap)
	for _, box := range osmShopBoxes {
		if want != "" && box.country != want {
			continue
		}
		out = append(out, box)
		if len(out) >= osmBoxCap {
			break
		}
	}
	return out
}

func overpassShopQuery(tags []string, box osmBox) string {
	return overpassShopOrNameQuery(tags, nil, box)
}

func overpassShopOrNameQuery(tags, names []string, box osmBox) string {
	var b strings.Builder
	b.WriteString("[out:json][timeout:20];\n(\n")
	for _, tag := range tags {
		fmt.Fprintf(&b, `  node["shop"=%q]["name"](%0.2f,%0.2f,%0.2f,%0.2f);`+"\n",
			tag, box.south, box.west, box.north, box.east)
	}
	if re := overpassNameRegex(names); re != "" {
		fmt.Fprintf(&b, `  node["shop"]["name"~%q,i](%0.2f,%0.2f,%0.2f,%0.2f);`+"\n",
			re, box.south, box.west, box.north, box.east)
		fmt.Fprintf(&b, `  way["shop"]["name"~%q,i](%0.2f,%0.2f,%0.2f,%0.2f);`+"\n",
			re, box.south, box.west, box.north, box.east)
	}
	b.WriteString(");\n")
	fmt.Fprintf(&b, "out tags %d;\n", osmHitCap)
	return b.String()
}

func overpassNameRegex(names []string) string {
	parts := make([]string, 0, len(names))
	for _, name := range uniqueFoldedStrings(names) {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		parts = append(parts, overpassRegexEscape(name))
	}
	return strings.Join(parts, "|")
}

func overpassRegexEscape(s string) string {
	const special = `\^$.|?*+()[]{}`
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (c *Client) fetchOverpass(ctx context.Context, query string) ([]byte, error) {
	return c.fetchOverpassLimit(ctx, query, maxBodyBytes)
}

func (c *Client) fetchOverpassLimit(ctx context.Context, query string, limit int64) ([]byte, error) {
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
		raw, err := c.postFormRawLimit(ctx, ep, form, map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"Accept":       "application/json",
			"User-Agent":   "map-engine/osm (https://github.com/nihao555-hub/map)",
		}, limit)
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
	return c.postFormRawLimit(ctx, rawURL, form, extra, maxBodyBytes)
}

func (c *Client) postFormRawLimit(ctx context.Context, rawURL, form string, extra map[string]string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "map-engine/osm")
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	return c.doLimit(req, limit)
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

func osmTagProfiles(extID, name string, tags map[string]string) []Profile {
	if tags == nil {
		return nil
	}
	type pair struct{ raw, site string }
	pairs := []pair{
		{firstNonEmpty(tags["contact:facebook"], tags["facebook"]), "www.facebook.com"},
		{firstNonEmpty(tags["contact:instagram"], tags["instagram"]), "www.instagram.com"},
		{firstNonEmpty(tags["contact:linkedin"], tags["linkedin"]), "www.linkedin.com/company"},
		{firstNonEmpty(tags["contact:twitter"], tags["twitter"], tags["contact:x"]), "x.com"},
		{firstNonEmpty(tags["contact:youtube"], tags["youtube"]), "www.youtube.com"},
		{firstNonEmpty(tags["contact:tiktok"], tags["tiktok"]), "www.tiktok.com"},
	}
	var out []Profile
	seen := map[string]struct{}{}
	for _, p := range pairs {
		raw := strings.TrimSpace(p.raw)
		if raw == "" {
			continue
		}
		candidates := []string{normalizeOSMContact(raw)}
		if !strings.Contains(raw, "://") && !strings.Contains(raw, "/") {
			candidates = append(candidates, "https://"+p.site+"/"+strings.TrimPrefix(raw, "@"))
		}
		for _, cand := range candidates {
			hit, ok := ParseSocialURL(cand, name, "")
			if !ok || hit.HomepageURL == "" {
				continue
			}
			key := hit.Platform + "|" + strings.ToLower(hit.HomepageURL)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, Profile{
				ExtID:    extID,
				Platform: hit.Platform,
				URL:      hit.HomepageURL,
				Handle:   hit.Handle,
				Source:   "osm-tag",
			})
			break
		}
	}
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
