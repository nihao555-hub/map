package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const wikidataParentChunkLimit = 20000

func (c *Client) ingestWikidataParentSocials(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil || strings.TrimSpace(c.WikidataURL) == "" {
		return IngestStats{Source: "wikidata-parent", Took: time.Since(started), Note: "skipped"}
	}
	byLEI := map[string]*Merchant{}
	failed := 0
	added := 0
	for _, prefix := range leiStartPrefixes(1) {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: "wikidata-parent", Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "wikidata-parent", started, 0, st.Err)
			return st
		}
		n, err := c.fetchWikidataParentPrefix(ctx, byLEI, prefix)
		if err != nil {
			failed++
			logIngest("Wikidata parent %s fail: %v", prefix, err)
			continue
		}
		added += n
	}
	rows := make([]Merchant, 0, len(byLEI))
	for _, m := range byLEI {
		if len(m.Profiles) == 0 {
			continue
		}
		rows = append(rows, *m)
	}
	matched, profiles, err := dir.attachExisting(ctx, rows)
	st := IngestStats{
		Source: "wikidata-parent",
		Rows:   matched,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("P749/P355 inherit, +%d bindings, %d profiles, %d failed", added, profiles, failed),
	}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, "wikidata-parent", started, matched, st.Note)
	logIngest("Wikidata parent attached %d GLEIF", matched)
	return st
}

func (c *Client) fetchWikidataParentPrefix(ctx context.Context, byLEI map[string]*Merchant, prefix string) (int, error) {
	raw, err := c.fetchWikidataSPARQL(ctx, wikidataParentSocialSPARQL(prefix))
	if err != nil {
		time.Sleep(2 * time.Second)
		raw, err = c.fetchWikidataSPARQL(ctx, wikidataParentSocialSPARQL(prefix))
	}
	if err != nil {
		return 0, err
	}
	n := mergeWikidataParentSocials(byLEI, raw)
	logIngest("Wikidata parent %s +%d (entities=%d)", prefix, n, len(byLEI))
	time.Sleep(400 * time.Millisecond)
	return n, nil
}

func wikidataParentSocialSPARQL(prefix string) string {
	prefix = strings.ToUpper(strings.TrimSpace(prefix))
	return fmt.Sprintf(`PREFIX wdt: <http://www.wikidata.org/prop/direct/>
SELECT ?lei ?web ?fb ?ig ?tw ?li ?tt ?yt WHERE {
  ?child wdt:P1278 ?lei .
  { ?child wdt:P749 ?parent } UNION { ?parent wdt:P355 ?child }
  OPTIONAL { ?parent wdt:P856 ?web }
  OPTIONAL { ?parent wdt:P2013 ?fb }
  OPTIONAL { ?parent wdt:P2003 ?ig }
  OPTIONAL { ?parent wdt:P2002 ?tw }
  OPTIONAL { ?parent wdt:P4264 ?li }
  OPTIONAL { ?parent wdt:P7085 ?tt }
  OPTIONAL { ?parent wdt:P2397 ?yt }
  FILTER(STRSTARTS(STR(?lei), "%s"))
  FILTER(BOUND(?web) || BOUND(?fb) || BOUND(?ig) || BOUND(?tw) || BOUND(?li) || BOUND(?tt) || BOUND(?yt))
}
LIMIT %d`, prefix, wikidataParentChunkLimit)
}

func mergeWikidataParentSocials(byLEI map[string]*Merchant, raw []byte) int {
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
		if len(lei) < 18 || len(lei) > 20 {
			continue
		}
		m := byLEI[lei]
		if m == nil {
			m = &Merchant{ExtID: "gleif:" + lei, Source: "gleif"}
			byLEI[lei] = m
		}
		add := func(raw, prefix, platform string) {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				return
			}
			if platform == PlatformWebsite {
				if strings.Contains(raw, "wikidata.org") || !isRealHomepage(raw) {
					return
				}
				if m.Homepage == "" || registryOnlyHomepage(m.Homepage) {
					m.Homepage = raw
				}
				m.Profiles = append(m.Profiles, Profile{
					ExtID:    m.ExtID,
					Platform: PlatformWebsite,
					URL:      raw,
					Source:   "wikidata-parent",
				})
				added++
				return
			}
			if prefix != "" && !strings.Contains(raw, "://") {
				raw = prefix + raw
			}
			hit, ok := ParseSocialURL(raw, "", "")
			if !ok {
				return
			}
			m.Profiles = append(m.Profiles, Profile{
				ExtID:    m.ExtID,
				Platform: firstNonEmpty(hit.Platform, platform),
				URL:      hit.HomepageURL,
				Handle:   hit.Handle,
				Source:   "wikidata-parent",
			})
			if m.Homepage == "" {
				m.Homepage = hit.HomepageURL
			}
			added++
		}
		add(row["web"].Value, "", PlatformWebsite)
		add(row["fb"].Value, "https://www.facebook.com/", PlatformFacebook)
		add(row["ig"].Value, "https://www.instagram.com/", PlatformInstagram)
		add(row["tw"].Value, "https://x.com/", PlatformX)
		add(row["li"].Value, "https://www.linkedin.com/company/", PlatformLinkedIn)
		add(row["tt"].Value, "https://www.tiktok.com/@", PlatformTikTok)
		add(row["yt"].Value, "https://www.youtube.com/channel/", PlatformYouTube)
	}
	return added
}
