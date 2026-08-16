package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ASEAN markets we dump from Wikidata (companies that already list a website or social).
var seaWikidataCountries = []struct {
	Code string
	QID  string
}{
	{"TH", "Q869"},
	{"VN", "Q881"},
	{"MY", "Q833"},
	{"ID", "Q252"},
	{"SG", "Q334"},
	{"PH", "Q928"},
	{"KH", "Q424"},
	{"LA", "Q819"},
	{"MM", "Q836"},
	{"BN", "Q921"},
	{"TL", "Q574"},
}

func (c *Client) ingestWikidataSEA(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil || strings.TrimSpace(c.WikidataURL) == "" {
		return IngestStats{Source: "wikidata", Took: time.Since(started), Note: "skipped"}
	}
	inserted := 0
	failed := 0
	for _, cc := range seaWikidataCountries {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: "wikidata", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "wikidata", started, inserted, st.Err)
			return st
		}
		raw, err := c.fetchWikidataSPARQL(ctx, wikidataSEACompanySPARQL(cc.QID))
		if err != nil {
			failed++
			logIngest("Wikidata %s fail: %v", cc.Code, err)
			continue
		}
		rows := parseWikidataSEAMerchants(raw, cc.Code)
		n, err := dir.InsertBatch(ctx, rows)
		if err != nil {
			st := IngestStats{Source: "wikidata", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "wikidata", started, inserted, st.Err)
			return st
		}
		inserted += n
		logIngest("Wikidata %s +%d (total %d)", cc.Code, n, inserted)
	}
	note := fmt.Sprintf("SEA companies, %d failed", failed)
	st := IngestStats{Source: "wikidata", Rows: inserted, Took: time.Since(started), Note: note}
	_ = dir.RecordRun(ctx, "wikidata", started, inserted, note)
	return st
}

func (c *Client) fetchWikidataSPARQL(ctx context.Context, sparql string) ([]byte, error) {
	u, err := url.Parse(c.WikidataURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("query", sparql)
	q.Set("format", "json")
	u.RawQuery = q.Encode()
	return c.get(ctx, u.String(), map[string]string{
		"Accept":     "application/sparql-results+json",
		"User-Agent": "map-engine/wikidata (https://github.com/nihao555-hub/map)",
	})
}

func wikidataSEACompanySPARQL(countryQID string) string {
	return fmt.Sprintf(`SELECT ?item ?itemLabel ?website ?facebook ?instagram ?twitter ?linkedin ?tiktok WHERE {
  ?item wdt:P17 wd:%s .
  ?item wdt:P31 ?class .
  VALUES ?class { wd:Q4830453 wd:Q6881511 wd:Q891723 wd:Q783794 }
  OPTIONAL { ?item wdt:P856 ?website. }
  OPTIONAL { ?item wdt:P2013 ?facebook. }
  OPTIONAL { ?item wdt:P2003 ?instagram. }
  OPTIONAL { ?item wdt:P2002 ?twitter. }
  OPTIONAL { ?item wdt:P4264 ?linkedin. }
  OPTIONAL { ?item wdt:P7085 ?tiktok. }
  FILTER(BOUND(?website) || BOUND(?facebook) || BOUND(?instagram) || BOUND(?twitter) || BOUND(?linkedin) || BOUND(?tiktok))
  SERVICE wikibase:label { bd:serviceParam wikibase:language "en,th,vi,id,ms,zh". }
}
LIMIT 2000`, countryQID)
}

func parseWikidataSEAMerchants(raw []byte, country string) []Merchant {
	var doc struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	out := make([]Merchant, 0, len(doc.Results.Bindings))
	seen := map[string]struct{}{}
	for _, row := range doc.Results.Bindings {
		item := strings.TrimSpace(row["item"].Value)
		qid := item
		if i := strings.LastIndex(item, "/"); i >= 0 {
			qid = item[i+1:]
		}
		name := strings.TrimSpace(row["itemLabel"].Value)
		if qid == "" || name == "" || looksLikeWikidataNonCompany(name) {
			continue
		}
		if _, ok := seen[qid]; ok {
			continue
		}
		seen[qid] = struct{}{}
		extID := "wd:" + qid
		home := strings.TrimSpace(row["website"].Value)
		if strings.Contains(home, "wikidata.org") {
			home = ""
		}
		m := Merchant{
			ExtID:    extID,
			Source:   "wikidata",
			Name:     name,
			Shop:     "company",
			Country:  strings.ToUpper(country),
			Homepage: home,
		}
		add := func(raw, prefix, platform string) {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return
			}
			if !strings.Contains(raw, "://") {
				raw = prefix + raw
			}
			hit, ok := ParseSocialURL(raw, name, "")
			if !ok {
				return
			}
			m.Profiles = append(m.Profiles, Profile{
				ExtID:    extID,
				Platform: firstNonEmpty(hit.Platform, platform),
				URL:      hit.HomepageURL,
				Handle:   hit.Handle,
				Verified: false,
				Source:   "wikidata",
			})
			if m.Homepage == "" {
				m.Homepage = hit.HomepageURL
			}
		}
		add(row["facebook"].Value, "https://www.facebook.com/", PlatformFacebook)
		add(row["instagram"].Value, "https://www.instagram.com/", PlatformInstagram)
		add(row["twitter"].Value, "https://x.com/", PlatformX)
		add(row["linkedin"].Value, "https://www.linkedin.com/company/", PlatformLinkedIn)
		add(row["tiktok"].Value, "https://www.tiktok.com/@", PlatformTikTok)
		if m.Homepage == "" && len(m.Profiles) == 0 {
			continue
		}
		if m.Homepage == "" {
			m.Homepage = m.Profiles[0].URL
		}
		out = append(out, m)
	}
	return out
}
