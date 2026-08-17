package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Wikidata social identifiers we dump globally (not the 2500-per-country cap).
var wikidataGlobalSocials = []struct {
	prop     string
	prefix   string
	platform string
}{
	{"P2013", "https://www.facebook.com/", PlatformFacebook},
	{"P2003", "https://www.instagram.com/", PlatformInstagram},
	{"P4264", "https://www.linkedin.com/company/", PlatformLinkedIn},
	{"P7085", "https://www.tiktok.com/@", PlatformTikTok},
	{"P2397", "https://www.youtube.com/channel/", PlatformYouTube},
	{"P2002", "https://x.com/", PlatformX},
	{"P3836", "https://www.pinterest.com/", PlatformPinterest},
}

const (
	wikidataSocialChunkLimit = 80000
	// QleverWikidataSPARQL is a public Wikidata mirror that can dump a
	// property in one shot; query.wikidata.org 504s / rate-limits the same query.
	QleverWikidataSPARQL = "https://qlever.dev/api/wikidata"
)

func (c *Client) ingestWikidataGlobalSocials(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil || strings.TrimSpace(c.WikidataURL) == "" {
		return IngestStats{Source: "wikidata-socials", Took: time.Since(started), Note: "skipped"}
	}
	byQID := map[string]*Merchant{}
	leiByQID := map[string]string{}
	failed := 0
	added := 0
	for _, q := range wikidataGlobalSocials {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: "wikidata-socials", Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "wikidata-socials", started, 0, st.Err)
			return st
		}
		n, err := c.fetchWikidataSocialAll(ctx, byQID, leiByQID, q.prop, q.prefix, q.platform)
		if err != nil {
			failed++
			logIngest("Wikidata socials %s fail: %v", q.prop, err)
			continue
		}
		added += n
	}
	rows := make([]Merchant, 0, len(byQID))
	leiRows := make([]Merchant, 0, 1024)
	for qid, m := range byQID {
		rows = append(rows, *m)
		if lei := leiByQID[qid]; len(lei) >= 18 {
			clone := *m
			clone.ExtID = "gleif:" + lei
			clone.Source = "gleif"
			clone.Profiles = append([]Profile(nil), m.Profiles...)
			for i := range clone.Profiles {
				clone.Profiles[i].ExtID = clone.ExtID
			}
			leiRows = append(leiRows, clone)
		}
	}
	inserted, err := dir.InsertBatch(ctx, rows)
	if err != nil {
		st := IngestStats{Source: "wikidata-socials", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "wikidata-socials", started, 0, st.Err)
		return st
	}
	matched, profiles, attachErr := dir.attachExisting(ctx, leiRows)
	if attachErr != nil && err == nil {
		err = attachErr
	}
	st := IngestStats{
		Source: "wikidata-socials",
		Rows:   inserted,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("orgs=%d bindings=%d lei=%d leiProfiles=%d failed=%d", len(byQID), added, matched, profiles, failed),
	}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, "wikidata-socials", started, inserted, st.Note)
	logIngest("Wikidata socials inserted %d (orgs=%d lei=%d)", inserted, len(byQID), matched)
	return st
}

func (c *Client) fetchWikidataSocialAll(ctx context.Context, byQID map[string]*Merchant, leiByQID map[string]string, prop, prefix, platform string) (int, error) {
	total := 0
	for offset := 0; offset < 400000; offset += wikidataSocialChunkLimit {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		raw, err := c.fetchWikidataSPARQL(ctx, wikidataGlobalSocialSPARQL(prop, "", offset))
		if err != nil {
			time.Sleep(2 * time.Second)
			raw, err = c.fetchWikidataSPARQL(ctx, wikidataGlobalSocialSPARQL(prop, "", offset))
		}
		if err != nil {
			if offset == 0 {
				return c.fetchWikidataSocialSharded(ctx, byQID, leiByQID, prop, prefix, platform)
			}
			return total, err
		}
		n := mergeWikidataGlobalSocial(byQID, leiByQID, raw, prefix, platform)
		total += n
		logIngest("Wikidata socials %s offset=%d +%d (orgs=%d)", prop, offset, n, len(byQID))
		if shouldShardWikidataSocial(n, offset) {
			return c.fetchWikidataSocialSharded(ctx, byQID, leiByQID, prop, prefix, platform)
		}
		if n < wikidataSocialChunkLimit {
			return total, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return total, nil
}

func (c *Client) fetchWikidataSocialSharded(ctx context.Context, byQID map[string]*Merchant, leiByQID map[string]string, prop, prefix, platform string) (int, error) {
	total := 0
	var last error
	for _, shard := range socialValuePrefixes() {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		raw, err := c.fetchWikidataSPARQL(ctx, wikidataGlobalSocialSPARQL(prop, shard, 0))
		if err != nil {
			last = err
			logIngest("Wikidata socials %s %s fail: %v", prop, shard, err)
			continue
		}
		n := mergeWikidataGlobalSocial(byQID, leiByQID, raw, prefix, platform)
		total += n
		logIngest("Wikidata socials %s %s +%d (orgs=%d)", prop, shard, n, len(byQID))
		time.Sleep(700 * time.Millisecond)
	}
	if total == 0 && last != nil {
		return 0, last
	}
	return total, nil
}

func shouldShardWikidataSocial(n, offset int) bool {
	return offset == 0 && n == 0
}

func wikidataGlobalSocialSPARQL(prop, valuePrefix string, offset int) string {
	filter := ""
	if valuePrefix != "" {
		filter = fmt.Sprintf(`FILTER(STRSTARTS(LCASE(STR(?val)), "%s"))`, strings.ToLower(valuePrefix))
	}
	if offset < 0 {
		offset = 0
	}
	return fmt.Sprintf(`PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX wdt: <http://www.wikidata.org/prop/direct/>
PREFIX wd: <http://www.wikidata.org/entity/>
SELECT ?item ?itemLabel ?lei ?cc ?val WHERE {
  ?item wdt:%s ?val .
  FILTER NOT EXISTS { ?item wdt:P31 wd:Q5 }
  %s
  OPTIONAL { ?item rdfs:label ?itemLabel . FILTER(LANG(?itemLabel) = "en") }
  OPTIONAL { ?item wdt:P1278 ?lei }
  OPTIONAL { ?item wdt:P17 ?country . ?country wdt:P297 ?cc }
}
LIMIT %d OFFSET %d`, prop, filter, wikidataSocialChunkLimit, offset)
}

func socialValuePrefixes() []string {
	alpha := "0123456789abcdefghijklmnopqrstuvwxyz"
	out := make([]string, 0, len(alpha))
	for i := 0; i < len(alpha); i++ {
		out = append(out, alpha[i:i+1])
	}
	return out
}

func mergeWikidataGlobalSocial(byQID map[string]*Merchant, leiByQID map[string]string, raw []byte, prefix, platform string) int {
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
		item := strings.TrimSpace(row["item"].Value)
		qid := item
		if i := strings.LastIndex(item, "/"); i >= 0 {
			qid = item[i+1:]
		}
		name := strings.TrimSpace(row["itemLabel"].Value)
		if qid == "" || name == "" || looksLikeWikidataNonCompany(name) {
			continue
		}
		val := strings.TrimSpace(row["val"].Value)
		if val == "" {
			continue
		}
		m := byQID[qid]
		if m == nil {
			m = &Merchant{
				ExtID:   "wd:" + qid,
				Source:  "wikidata",
				Name:    name,
				Shop:    "company",
				Country: strings.ToUpper(strings.TrimSpace(row["cc"].Value)),
			}
			byQID[qid] = m
		}
		if m.Name == "" {
			m.Name = name
		}
		if m.Country == "" {
			m.Country = strings.ToUpper(strings.TrimSpace(row["cc"].Value))
		}
		if lei := strings.ToUpper(strings.TrimSpace(row["lei"].Value)); len(lei) >= 18 && len(lei) <= 20 {
			leiByQID[qid] = lei
		}
		if platform == PlatformWebsite {
			if strings.Contains(val, "wikidata.org") || !isRealHomepage(val) {
				continue
			}
			if m.Homepage == "" || registryOnlyHomepage(m.Homepage) {
				m.Homepage = val
			}
			m.Profiles = append(m.Profiles, Profile{
				ExtID:    m.ExtID,
				Platform: PlatformWebsite,
				URL:      val,
				Source:   "wikidata-social",
			})
			added++
			continue
		}
		if prefix != "" && !strings.Contains(val, "://") {
			val = prefix + val
		}
		hit, ok := ParseSocialURL(val, name, "")
		if !ok {
			continue
		}
		m.Profiles = append(m.Profiles, Profile{
			ExtID:    m.ExtID,
			Platform: firstNonEmpty(hit.Platform, platform),
			URL:      hit.HomepageURL,
			Handle:   hit.Handle,
			Source:   "wikidata-social",
		})
		if m.Homepage == "" {
			m.Homepage = hit.HomepageURL
		}
		added++
	}
	return added
}
