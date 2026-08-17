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
	{"P7120", "https://www.douyin.com/user/", PlatformDouyin},
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

var wikidataShortVideoSocials = []struct {
	prop     string
	prefix   string
	platform string
}{
	{"P7085", "https://www.tiktok.com/@", PlatformTikTok},
	{"P7120", "https://www.douyin.com/user/", PlatformDouyin},
}

func (c *Client) ingestWikidataShortVideo(ctx context.Context, dir *Directory) IngestStats {
	return c.ingestWikidataSocialList(ctx, dir, "wikidata-short-video", wikidataShortVideoSocials)
}

func (c *Client) ingestWikidataShortVideoAll(ctx context.Context, dir *Directory) IngestStats {
	return c.ingestWikidataSocialList(ctx, dir, "wikidata-short-video-all", wikidataShortVideoSocials)
}

// ingestWikidataOfficialShortVideo dumps P856 official websites that are
// already a TikTok or Douyin homepage (often missing from P7085/P7120).
func (c *Client) ingestWikidataOfficialShortVideo(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil || strings.TrimSpace(c.WikidataURL) == "" {
		return IngestStats{Source: "wikidata-official-short-video", Took: time.Since(started), Note: "skipped"}
	}
	byQID := map[string]*Merchant{}
	leiByQID := map[string]string{}
	raw, err := c.fetchWikidataSPARQL(ctx, wikidataOfficialShortVideoSPARQL())
	if err != nil {
		time.Sleep(2 * time.Second)
		raw, err = c.fetchWikidataSPARQL(ctx, wikidataOfficialShortVideoSPARQL())
	}
	if err != nil {
		st := IngestStats{Source: "wikidata-official-short-video", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, st.Source, started, 0, st.Err)
		return st
	}
	added := mergeWikidataGlobalSocial(byQID, leiByQID, raw, "", "")
	rows := make([]Merchant, 0, len(byQID))
	for _, m := range byQID {
		rows = append(rows, *m)
	}
	inserted, err := dir.InsertBatch(ctx, rows)
	st := IngestStats{
		Source: "wikidata-official-short-video",
		Rows:   inserted,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("orgs=%d bindings=%d", len(byQID), added),
	}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, st.Source, started, inserted, st.Note)
	logIngest("Wikidata official short-video inserted %d (orgs=%d)", inserted, len(byQID))
	return st
}

func wikidataOfficialShortVideoSPARQL() string {
	return fmt.Sprintf(`PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX wdt: <http://www.wikidata.org/prop/direct/>
SELECT ?item ?itemLabel ?zhLabel ?lei ?cc ?val WHERE {
  ?item wdt:P856 ?val .
  FILTER(CONTAINS(LCASE(STR(?val)), "tiktok.com/@") || CONTAINS(LCASE(STR(?val)), "douyin.com/user/"))
  OPTIONAL { ?item rdfs:label ?itemLabel . FILTER(LANG(?itemLabel) = "en") }
  OPTIONAL { ?item rdfs:label ?zhLabel . FILTER(LANG(?zhLabel) = "zh") }
  OPTIONAL { ?item wdt:P1278 ?lei }
  OPTIONAL { ?item wdt:P17 ?country . ?country wdt:P297 ?cc }
}
LIMIT %d`, wikidataSocialChunkLimit)
}

func (c *Client) ingestWikidataGlobalSocials(ctx context.Context, dir *Directory) IngestStats {
	return c.ingestWikidataSocialList(ctx, dir, "wikidata-socials", wikidataGlobalSocials)
}

func (c *Client) ingestWikidataSocialList(ctx context.Context, dir *Directory, source string, props []struct {
	prop     string
	prefix   string
	platform string
}) IngestStats {
	started := time.Now()
	if source == "" {
		source = "wikidata-socials"
	}
	if c == nil || strings.TrimSpace(c.WikidataURL) == "" {
		return IngestStats{Source: source, Took: time.Since(started), Note: "skipped"}
	}
	byQID := map[string]*Merchant{}
	leiByQID := map[string]string{}
	failed := 0
	added := 0
	for _, q := range props {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: source, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, source, started, 0, st.Err)
			return st
		}
		n, err := c.fetchWikidataSocialAll(ctx, byQID, leiByQID, q.prop, q.prefix, q.platform, source != "wikidata-short-video-all")
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
		st := IngestStats{Source: source, Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, source, started, 0, st.Err)
		return st
	}
	matched, profiles, attachErr := dir.attachExisting(ctx, leiRows)
	if attachErr != nil && err == nil {
		err = attachErr
	}
	st := IngestStats{
		Source: source,
		Rows:   inserted,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("orgs=%d bindings=%d lei=%d leiProfiles=%d failed=%d", len(byQID), added, matched, profiles, failed),
	}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, source, started, inserted, st.Note)
	logIngest("Wikidata socials %s inserted %d (orgs=%d lei=%d)", source, inserted, len(byQID), matched)
	return st
}

func (c *Client) fetchWikidataSocialAll(ctx context.Context, byQID map[string]*Merchant, leiByQID map[string]string, prop, prefix, platform string, excludePerson bool) (int, error) {
	total := 0
	for offset := 0; offset < 400000; offset += wikidataSocialChunkLimit {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		raw, err := c.fetchWikidataSPARQL(ctx, wikidataSocialSPARQL(prop, "", offset, excludePerson))
		if err != nil {
			time.Sleep(2 * time.Second)
			raw, err = c.fetchWikidataSPARQL(ctx, wikidataSocialSPARQL(prop, "", offset, excludePerson))
		}
		if err != nil {
			if offset == 0 {
				return c.fetchWikidataSocialSharded(ctx, byQID, leiByQID, prop, prefix, platform, excludePerson)
			}
			return total, err
		}
		n := mergeWikidataGlobalSocial(byQID, leiByQID, raw, prefix, platform)
		total += n
		logIngest("Wikidata socials %s offset=%d +%d (orgs=%d)", prop, offset, n, len(byQID))
		if shouldShardWikidataSocial(n, offset) {
			return c.fetchWikidataSocialSharded(ctx, byQID, leiByQID, prop, prefix, platform, excludePerson)
		}
		if n < wikidataSocialChunkLimit {
			return total, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return total, nil
}

func (c *Client) fetchWikidataSocialSharded(ctx context.Context, byQID map[string]*Merchant, leiByQID map[string]string, prop, prefix, platform string, excludePerson bool) (int, error) {
	total := 0
	var last error
	for _, shard := range socialValuePrefixes() {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		raw, err := c.fetchWikidataSPARQL(ctx, wikidataSocialSPARQL(prop, shard, 0, excludePerson))
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
	return wikidataSocialSPARQL(prop, valuePrefix, offset, true)
}

func wikidataSocialSPARQL(prop, valuePrefix string, offset int, excludePerson bool) string {
	filter := ""
	if valuePrefix != "" {
		filter = fmt.Sprintf(`FILTER(STRSTARTS(LCASE(STR(?val)), "%s"))`, strings.ToLower(valuePrefix))
	}
	person := ""
	if excludePerson {
		person = `FILTER NOT EXISTS { ?item wdt:P31 wd:Q5 }`
	}
	if offset < 0 {
		offset = 0
	}
	return fmt.Sprintf(`PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX wdt: <http://www.wikidata.org/prop/direct/>
PREFIX wd: <http://www.wikidata.org/entity/>
SELECT ?item ?itemLabel ?zhLabel ?lei ?cc ?val WHERE {
  ?item wdt:%s ?val .
  %s
  %s
  OPTIONAL { ?item rdfs:label ?itemLabel . FILTER(LANG(?itemLabel) = "en") }
  OPTIONAL { ?item rdfs:label ?zhLabel . FILTER(LANG(?zhLabel) = "zh") }
  OPTIONAL { ?item wdt:P1278 ?lei }
  OPTIONAL { ?item wdt:P17 ?country . ?country wdt:P297 ?cc }
}
LIMIT %d OFFSET %d`, prop, person, filter, wikidataSocialChunkLimit, offset)
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
		name := firstNonEmpty(
			strings.TrimSpace(row["itemLabel"].Value),
			strings.TrimSpace(row["zhLabel"].Value),
			socialFallbackName(row["val"].Value, qid),
		)
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

func socialFallbackName(val, qid string) string {
	v := strings.TrimSpace(val)
	if i := strings.LastIndex(v, "/"); i >= 0 {
		v = strings.TrimSpace(v[i+1:])
	}
	v = strings.TrimPrefix(v, "@")
	if v != "" {
		return v
	}
	return strings.TrimSpace(qid)
}
