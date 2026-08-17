package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	wikidataWorldChunkLimit = 20000
	wikidataWorldMaxOffset  = 800000
)

func (c *Client) ingestWikidataWorldCompanies(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil || dir == nil || strings.TrimSpace(c.WikidataURL) == "" {
		return IngestStats{Source: "wikidata-world", Took: time.Since(started), Note: "skipped"}
	}
	orig := c.WikidataURL
	if !strings.Contains(strings.ToLower(c.WikidataURL), "qlever") {
		c.WikidataURL = QleverWikidataSPARQL
	}
	defer func() { c.WikidataURL = orig }()

	inserted := 0
	leiMatched := 0
	leiProfiles := 0
	failed := 0
	bindings := 0
	for offset := 0; offset < wikidataWorldMaxOffset; offset += wikidataWorldChunkLimit {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: "wikidata-world", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "wikidata-world", started, inserted, st.Err)
			return st
		}
		raw, err := c.fetchWikidataSPARQL(ctx, wikidataWorldCompanySPARQL(offset))
		if err != nil {
			time.Sleep(2 * time.Second)
			raw, err = c.fetchWikidataSPARQL(ctx, wikidataWorldCompanySPARQL(offset))
		}
		if err != nil {
			failed++
			logIngest("Wikidata world offset=%d fail: %v", offset, err)
			if offset == 0 {
				st := IngestStats{Source: "wikidata-world", Took: time.Since(started), Err: err.Error()}
				_ = dir.RecordRun(ctx, "wikidata-world", started, 0, st.Err)
				return st
			}
			break
		}
		rows, leiRows := parseWikidataWorldCompanies(raw)
		bindings += len(rows)
		n, err := dir.InsertBatch(ctx, rows)
		if err != nil {
			st := IngestStats{Source: "wikidata-world", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "wikidata-world", started, inserted, st.Err)
			return st
		}
		inserted += n
		matched, profiles, attachErr := dir.attachExisting(ctx, leiRows)
		if attachErr != nil {
			st := IngestStats{Source: "wikidata-world", Rows: inserted, Took: time.Since(started), Err: attachErr.Error()}
			_ = dir.RecordRun(ctx, "wikidata-world", started, inserted, st.Err)
			return st
		}
		leiMatched += matched
		leiProfiles += profiles
		logIngest("Wikidata world offset=%d +%d (inserted=%d lei=%d)", offset, len(rows), inserted, leiMatched)
		if len(rows) < wikidataWorldChunkLimit {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	st := IngestStats{
		Source: "wikidata-world",
		Rows:   inserted,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("bindings=%d lei=%d leiProfiles=%d failed=%d", bindings, leiMatched, leiProfiles, failed),
	}
	_ = dir.RecordRun(ctx, "wikidata-world", started, inserted, st.Note)
	logIngest("Wikidata world inserted %d (lei=%d)", inserted, leiMatched)
	return st
}

func wikidataWorldCompanySPARQL(offset int) string {
	if offset < 0 {
		offset = 0
	}
	return fmt.Sprintf(`PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX wdt: <http://www.wikidata.org/prop/direct/>
PREFIX wd: <http://www.wikidata.org/entity/>
SELECT ?item ?itemLabel ?cc ?website ?lei ?facebook ?instagram ?linkedin ?twitter ?tiktok WHERE {
  ?item wdt:P31 ?class .
  VALUES ?class { wd:Q4830453 wd:Q6881511 wd:Q891723 wd:Q783794 }
  ?item wdt:P856 ?website .
  FILTER NOT EXISTS { ?item wdt:P31 wd:Q5 }
  OPTIONAL { ?item rdfs:label ?itemLabel . FILTER(LANG(?itemLabel) = "en") }
  OPTIONAL { ?item wdt:P17 ?country . ?country wdt:P297 ?cc }
  OPTIONAL { ?item wdt:P1278 ?lei }
  OPTIONAL { ?item wdt:P2013 ?facebook }
  OPTIONAL { ?item wdt:P2003 ?instagram }
  OPTIONAL { ?item wdt:P4264 ?linkedin }
  OPTIONAL { ?item wdt:P2002 ?twitter }
  OPTIONAL { ?item wdt:P7085 ?tiktok }
}
LIMIT %d OFFSET %d`, wikidataWorldChunkLimit, offset)
}

func parseWikidataWorldCompanies(raw []byte) (rows, leiRows []Merchant) {
	var doc struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil
	}
	seen := map[string]struct{}{}
	for _, row := range doc.Results.Bindings {
		item := strings.TrimSpace(row["item"].Value)
		qid := item
		if i := strings.LastIndex(item, "/"); i >= 0 {
			qid = item[i+1:]
		}
		name := strings.TrimSpace(row["itemLabel"].Value)
		if qid == "" || qid == "Q5" || name == "" || looksLikeWikidataNonCompany(name) {
			continue
		}
		if _, ok := seen[qid]; ok {
			continue
		}
		seen[qid] = struct{}{}
		home := strings.TrimSpace(row["website"].Value)
		if strings.Contains(home, "wikidata.org") || !isRealHomepage(home) {
			home = ""
		}
		extID := "wd:" + qid
		m := Merchant{
			ExtID:    extID,
			Source:   "wikidata",
			Name:     name,
			Shop:     "company",
			Country:  strings.ToUpper(strings.TrimSpace(row["cc"].Value)),
			Homepage: home,
		}
		add := func(rawURL, prefix, platform string) {
			rawURL = strings.TrimSpace(rawURL)
			if rawURL == "" {
				return
			}
			if !strings.Contains(rawURL, "://") {
				rawURL = prefix + rawURL
			}
			hit, ok := ParseSocialURL(rawURL, name, "")
			if !ok {
				return
			}
			m.Profiles = append(m.Profiles, Profile{
				ExtID:    extID,
				Platform: firstNonEmpty(hit.Platform, platform),
				URL:      hit.HomepageURL,
				Handle:   hit.Handle,
				Source:   "wikidata-world",
			})
			if m.Homepage == "" {
				m.Homepage = hit.HomepageURL
			}
		}
		if home != "" {
			m.Profiles = append(m.Profiles, Profile{
				ExtID:    extID,
				Platform: PlatformWebsite,
				URL:      home,
				Source:   "wikidata-world",
			})
		}
		add(row["facebook"].Value, "https://www.facebook.com/", PlatformFacebook)
		add(row["instagram"].Value, "https://www.instagram.com/", PlatformInstagram)
		add(row["linkedin"].Value, "https://www.linkedin.com/company/", PlatformLinkedIn)
		add(row["twitter"].Value, "https://x.com/", PlatformX)
		add(row["tiktok"].Value, "https://www.tiktok.com/@", PlatformTikTok)
		if m.Homepage == "" && len(m.Profiles) == 0 {
			continue
		}
		if m.Homepage == "" && len(m.Profiles) > 0 {
			m.Homepage = m.Profiles[0].URL
		}
		rows = append(rows, m)
		if lei := strings.ToUpper(strings.TrimSpace(row["lei"].Value)); len(lei) >= 18 && len(lei) <= 20 {
			clone := m
			clone.ExtID = "gleif:" + lei
			clone.Source = "gleif"
			clone.Profiles = append([]Profile(nil), m.Profiles...)
			for i := range clone.Profiles {
				clone.Profiles[i].ExtID = clone.ExtID
			}
			leiRows = append(leiRows, clone)
		}
	}
	return rows, leiRows
}
