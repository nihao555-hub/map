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
}

const wikidataSocialChunkLimit = 8000

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
		// Unprefixed property dumps 504 on query.wikidata.org; always shard by value.
		hitAny := false
		for _, prefix := range socialValuePrefixes() {
			if err := ctx.Err(); err != nil {
				break
			}
			n2, _, err := c.fetchWikidataSocialPrefix(ctx, byQID, leiByQID, q.prop, q.prefix, q.platform, prefix)
			if err != nil {
				failed++
				logIngest("Wikidata socials %s %s fail: %v", q.prop, prefix, err)
				continue
			}
			hitAny = true
			added += n2
		}
		if !hitAny {
			logIngest("Wikidata socials %s no shards", q.prop)
		}
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

func (c *Client) fetchWikidataSocialPrefix(ctx context.Context, byQID map[string]*Merchant, leiByQID map[string]string, prop, prefix, platform, valuePrefix string) (int, bool, error) {
	raw, err := c.fetchWikidataSPARQL(ctx, wikidataGlobalSocialSPARQL(prop, valuePrefix))
	if err != nil {
		time.Sleep(2 * time.Second)
		raw, err = c.fetchWikidataSPARQL(ctx, wikidataGlobalSocialSPARQL(prop, valuePrefix))
	}
	if err != nil {
		return 0, false, err
	}
	n := mergeWikidataGlobalSocial(byQID, leiByQID, raw, prefix, platform)
	logIngest("Wikidata socials %s %s +%d (orgs=%d)", prop, valuePrefix, n, len(byQID))
	time.Sleep(700 * time.Millisecond)
	return n, n >= wikidataSocialChunkLimit, nil
}

func wikidataGlobalSocialSPARQL(prop, valuePrefix string) string {
	filter := ""
	if valuePrefix != "" {
		filter = fmt.Sprintf(`FILTER(STRSTARTS(LCASE(STR(?val)), "%s"))`, strings.ToLower(valuePrefix))
	}
	return fmt.Sprintf(`SELECT ?item ?itemLabel ?lei ?cc ?val WHERE {
  ?item wdt:%s ?val .
  FILTER NOT EXISTS { ?item wdt:P31 wd:Q5 }
  %s
  OPTIONAL { ?item wdt:P1278 ?lei }
  OPTIONAL { ?item wdt:P17 ?country . ?country wdt:P297 ?cc }
  SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
}
LIMIT %d`, prop, filter, wikidataSocialChunkLimit)
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
		if prefix != "" && !strings.Contains(val, "://") {
			val = prefix + val
		}
		hit, ok := ParseSocialURL(val, name, "")
		if !ok {
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
