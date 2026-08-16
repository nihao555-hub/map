package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const wikidataCompanyLimit = 80

func (c *Client) searchWikidataCompanies(ctx context.Context, keyword, country string, wanted map[string]bool) ([]Hit, error) {
	if c == nil || strings.TrimSpace(c.WikidataURL) == "" {
		return nil, nil
	}
	term := firstNonEmpty(englishProductTerm(keyword, country), keyword)
	u, err := url.Parse(c.WikidataURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("query", wikidataCompanySPARQL(term, country))
	q.Set("format", "json")
	u.RawQuery = q.Encode()
	raw, err := c.get(ctx, u.String(), map[string]string{
		"Accept":     "application/sparql-results+json",
		"User-Agent": "map-engine/wikidata (https://github.com/nihao555-hub/map)",
	})
	if err != nil {
		return nil, err
	}
	return parseWikidataCompanies(raw, keyword, country, wanted)
}

func wikidataCompanySPARQL(term, country string) string {
	term = strings.TrimSpace(term)
	if term == "" {
		term = "company"
	}
	return fmt.Sprintf(`SELECT ?item ?itemLabel ?website ?facebook ?linkedin ?instagram ?countryLabel WHERE {
  SERVICE wikibase:mwapi {
    bd:serviceParam wikibase:api "EntitySearch" .
    bd:serviceParam wikibase:endpoint "www.wikidata.org" .
    bd:serviceParam mwapi:search %q .
    bd:serviceParam mwapi:language "en" .
    bd:serviceParam mwapi:limit "50" .
    ?item wikibase:apiOutputItem mwapi:item .
  }
  OPTIONAL { ?item wdt:P856 ?website. }
  OPTIONAL { ?item wdt:P2013 ?facebook. }
  OPTIONAL { ?item wdt:P4264 ?linkedin. }
  OPTIONAL { ?item wdt:P2003 ?instagram. }
  OPTIONAL { ?item wdt:P17 ?country. }
  %s
  SERVICE wikibase:label { bd:serviceParam wikibase:language "en,zh". }
}
LIMIT %d`, term, wikidataCountryFilter(country), wikidataCompanyLimit)
}

func parseWikidataCompanies(raw []byte, keyword, country string, wanted map[string]bool) ([]Hit, error) {
	var doc struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(doc.Results.Bindings))
	for _, row := range doc.Results.Bindings {
		name := strings.TrimSpace(row["itemLabel"].Value)
		if name == "" || isGenericProductName(name, keyword) || looksLikeWikidataNonCompany(name) {
			continue
		}
		cc, label := inferCountryFromText(row["countryLabel"].Value, country)
		snippet := strings.TrimSpace(strings.Join([]string{"company", "店铺", label}, " · "))
		addSocial := func(raw, prefix string) {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return
			}
			if !strings.Contains(raw, "://") {
				raw = prefix + raw
			}
			hit, ok := ParseSocialURL(raw, name, snippet)
			if !ok {
				return
			}
			if len(wanted) > 0 && !wanted[hit.Platform] {
				return
			}
			hit.Name = name
			hit.Snippet = snippet
			hit.Country = cc
			hit.CountryLabel = label
			if hit.Extra == nil {
				hit.Extra = map[string]string{}
			}
			hit.Extra["src"] = "wikidata"
			hit.Extra["match"] = "category"
			out = append(out, hit)
		}
		addSocial(row["facebook"].Value, "https://www.facebook.com/")
		addSocial(row["linkedin"].Value, "https://www.linkedin.com/company/")
		addSocial(row["instagram"].Value, "https://www.instagram.com/")

		home := strings.TrimSpace(row["website"].Value)
		if home == "" || strings.Contains(home, "wikidata.org") {
			continue
		}
		out = append(out, Hit{
			ID:           "wd:" + home,
			Kind:         KindPeople,
			Platform:     PlatformWebsite,
			Name:         name,
			Title:        name,
			Snippet:      snippet,
			HomepageURL:  home,
			MessageURL:   home,
			MessageHint:  "打开 Wikidata / 公司官网。系统不会代发。",
			Score:        80,
			Country:      cc,
			CountryLabel: label,
			Extra:        map[string]string{"src": "wikidata", "match": "category"},
		})
	}
	return out, nil
}

func looksLikeWikidataNonCompany(name string) bool {
	n := strings.ToLower(name)
	for _, tok := range []string{
		"apparatus", "device", "method", "treatment", "substrate",
		"patent", "therapy", "composition", "lightpad", "illumination device",
	} {
		if strings.Contains(n, tok) {
			return true
		}
	}
	return false
}
