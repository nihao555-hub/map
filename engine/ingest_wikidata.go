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
			time.Sleep(2 * time.Second)
			raw, err = c.fetchWikidataSPARQL(ctx, wikidataSEACompanySPARQL(cc.QID))
		}
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

var leiContactQueries = []struct {
	prop     string
	prefix   string
	platform string
}{
	{"P856", "", PlatformWebsite},
	{"P2013", "https://www.facebook.com/", PlatformFacebook},
	{"P2003", "https://www.instagram.com/", PlatformInstagram},
	{"P2002", "https://x.com/", PlatformX},
	{"P4264", "https://www.linkedin.com/company/", PlatformLinkedIn},
	{"P7085", "https://www.tiktok.com/@", PlatformTikTok},
}

func (c *Client) ingestWikidataLEI(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil || strings.TrimSpace(c.WikidataURL) == "" {
		return IngestStats{Source: "gleif-lei", Took: time.Since(started), Note: "skipped"}
	}
	byLEI := map[string]*Merchant{}
	failed := 0
	for _, q := range leiContactQueries {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: "gleif-lei", Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "gleif-lei", started, 0, st.Err)
			return st
		}
		raw, err := c.fetchWikidataSPARQL(ctx, wikidataLEIPropSPARQL(q.prop))
		if err != nil {
			time.Sleep(2 * time.Second)
			raw, err = c.fetchWikidataSPARQL(ctx, wikidataLEIPropSPARQL(q.prop))
		}
		if err != nil {
			failed++
			logIngest("Wikidata LEI %s fail: %v", q.prop, err)
			continue
		}
		n := mergeWikidataLEIProp(byLEI, raw, q.prefix, q.platform)
		logIngest("Wikidata LEI %s +%d (entities=%d)", q.prop, n, len(byLEI))
	}
	rows := make([]Merchant, 0, len(byLEI))
	for _, m := range byLEI {
		rows = append(rows, *m)
	}
	matched, profiles, err := dir.attachExisting(ctx, rows)
	st := IngestStats{Source: "gleif-lei", Rows: matched, Took: time.Since(started), Note: fmt.Sprintf("Wikidata P1278, %d profiles, %d failed", profiles, failed)}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, "gleif-lei", started, matched, st.Note)
	return st
}

func wikidataLEIPropSPARQL(prop string) string {
	return fmt.Sprintf(`SELECT ?lei ?val WHERE {
  ?item wdt:P1278 ?lei ;
        wdt:%s ?val .
}
LIMIT 30000`, prop)
}

func mergeWikidataLEIProp(byLEI map[string]*Merchant, raw []byte, prefix, platform string) int {
	var doc struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0
	}
	added := 0
	for _, row := range doc.Results.Bindings {
		lei := strings.ToUpper(strings.TrimSpace(row["lei"].Value))
		val := strings.TrimSpace(row["val"].Value)
		if len(lei) < 18 || len(lei) > 20 || val == "" {
			continue
		}
		m := byLEI[lei]
		if m == nil {
			m = &Merchant{ExtID: "gleif:" + lei, Source: "gleif"}
			byLEI[lei] = m
		}
		if platform == PlatformWebsite {
			if !strings.Contains(val, "wikidata.org") && (m.Homepage == "" || registryOnlyHomepage(m.Homepage)) {
				m.Homepage = val
				added++
			}
			continue
		}
		if prefix != "" && !strings.Contains(val, "://") {
			val = prefix + val
		}
		hit, ok := ParseSocialURL(val, "", "")
		if !ok {
			continue
		}
		m.Profiles = append(m.Profiles, Profile{
			ExtID:    m.ExtID,
			Platform: firstNonEmpty(hit.Platform, platform),
			URL:      hit.HomepageURL,
			Handle:   hit.Handle,
			Verified: false,
			Source:   "wikidata-lei",
		})
		if m.Homepage == "" {
			m.Homepage = hit.HomepageURL
		}
		added++
	}
	return added
}

func parseWikidataLEIMerchants(raw []byte, country string) []Merchant {
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
		lei := strings.ToUpper(strings.TrimSpace(row["lei"].Value))
		if len(lei) < 18 || len(lei) > 20 {
			continue
		}
		if _, ok := seen[lei]; ok {
			continue
		}
		seen[lei] = struct{}{}
		name := strings.TrimSpace(row["itemLabel"].Value)
		extID := "gleif:" + lei
		home := strings.TrimSpace(row["website"].Value)
		if strings.Contains(home, "wikidata.org") {
			home = ""
		}
		m := Merchant{
			ExtID:    extID,
			Source:   "gleif",
			Name:     name,
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
				Source:   "wikidata-lei",
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
		out = append(out, m)
	}
	return out
}

func (d *Directory) attachExisting(ctx context.Context, rows []Merchant) (matched, profiles int, err error) {
	if d == nil || len(rows) == 0 {
		return 0, 0, nil
	}
	var profs []Profile
	for _, row := range rows {
		ok, err := d.HasExtID(ctx, row.ExtID)
		if err != nil {
			return matched, profiles, err
		}
		if !ok {
			continue
		}
		matched++
		if isRealHomepage(row.Homepage) {
			if err := d.SetHomepage(ctx, row.ExtID, row.Homepage); err != nil {
				return matched, profiles, err
			}
		}
		profs = append(profs, row.Profiles...)
	}
	if err := d.UpsertProfiles(ctx, profs); err != nil {
		return matched, profiles, err
	}
	return matched, len(profs), nil
}
