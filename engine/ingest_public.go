package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

const defaultRORZip = "/tmp/merchant-ingest/ror-data.zip"

func (c *Client) ingestPublicSocials(ctx context.Context, dir *Directory, opt IngestOptions) []IngestStats {
	var stats []IngestStats
	stats = append(stats, c.ingestROR(ctx, dir, opt.RORZip))
	if c != nil && strings.TrimSpace(c.WikidataURL) != "" {
		stats = append(stats, c.ingestWikidataOfficialSites(ctx, dir))
	}
	stats = append(stats, dir.attachUniqueNameSocials(ctx))
	return stats
}

func (c *Client) ingestROR(ctx context.Context, dir *Directory, zipPath string) IngestStats {
	started := time.Now()
	if zipPath == "" {
		zipPath = defaultRORZip
	}
	if _, err := os.Stat(zipPath); err != nil {
		logIngest("ROR dump missing, downloading %s", DefaultRORDump)
		if err := downloadRORDump(ctx, zipPath); err != nil {
			st := IngestStats{Source: "ror", Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "ror", started, 0, st.Err)
			return st
		}
	}
	orgs, err := parseRORDump(zipPath)
	if err != nil {
		st := IngestStats{Source: "ror", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "ror", started, 0, st.Err)
		return st
	}
	rows := make([]Merchant, 0, len(orgs))
	leiOrgs := 0
	for _, org := range orgs {
		if org.LEI == "" || !isRealHomepage(org.Website) {
			continue
		}
		leiOrgs++
		extID := "gleif:" + org.LEI
		rows = append(rows, Merchant{
			ExtID:    extID,
			Source:   "gleif",
			Homepage: org.Website,
			Profiles: []Profile{{
				ExtID:    extID,
				Platform: PlatformWebsite,
				URL:      org.Website,
				Source:   "ror",
			}},
		})
	}
	// Current ROR dumps do not populate LEI; still copy unique name+country websites.
	if extra, err := rorNameMatchRows(ctx, dir, orgs); err != nil {
		st := IngestStats{Source: "ror", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "ror", started, 0, st.Err)
		return st
	} else {
		rows = append(rows, extra...)
	}
	matched, profiles, err := dir.attachExisting(ctx, rows)
	st := IngestStats{
		Source: "ror",
		Rows:   matched,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("dump orgs=%d lei=%d profiles=%d", len(orgs), leiOrgs, profiles),
	}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, "ror", started, matched, st.Note)
	logIngest("ROR attached %d GLEIF (dump=%d lei=%d)", matched, len(orgs), leiOrgs)
	return st
}

func rorNameMatchRows(ctx context.Context, dir *Directory, orgs []ROROrg) ([]Merchant, error) {
	if dir == nil || len(orgs) == 0 {
		return nil, nil
	}
	targets, err := dir.loadBareGLEIFKeys(ctx)
	if err != nil {
		return nil, err
	}
	type slot struct {
		org      ROROrg
		conflict bool
	}
	byKey := map[string]*slot{}
	for _, org := range orgs {
		if !isRealHomepage(org.Website) {
			continue
		}
		key := nameCountryKey(org.Name, org.Country)
		if key == "" {
			continue
		}
		if cur, ok := byKey[key]; ok {
			cur.conflict = true
			continue
		}
		org := org
		byKey[key] = &slot{org: org}
	}
	var rows []Merchant
	for key, extID := range targets {
		slot, ok := byKey[key]
		if !ok || slot.conflict {
			continue
		}
		rows = append(rows, Merchant{
			ExtID:    extID,
			Source:   "gleif",
			Homepage: slot.org.Website,
			Profiles: []Profile{{
				ExtID:    extID,
				Platform: PlatformWebsite,
				URL:      slot.org.Website,
				Source:   "ror-name",
			}},
		})
	}
	return rows, nil
}

func (c *Client) ingestWikidataOfficialSites(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil || strings.TrimSpace(c.WikidataURL) == "" {
		return IngestStats{Source: "wikidata-p856", Took: time.Since(started), Note: "skipped"}
	}
	byLEI := map[string]*Merchant{}
	failed := 0
	added := 0
	for _, prefix := range leiStartPrefixes(1) {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: "wikidata-p856", Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "wikidata-p856", started, 0, st.Err)
			return st
		}
		n, hitLimit, err := c.fetchLEIWebsitePrefix(ctx, byLEI, prefix)
		if err != nil {
			failed++
			logIngest("Wikidata P856 %s fail: %v", prefix, err)
			continue
		}
		added += n
		if !hitLimit {
			continue
		}
		for _, sub := range expandLEIPrefix(prefix) {
			if err := ctx.Err(); err != nil {
				break
			}
			n2, _, err := c.fetchLEIWebsitePrefix(ctx, byLEI, sub)
			if err != nil {
				failed++
				logIngest("Wikidata P856 %s fail: %v", sub, err)
				continue
			}
			added += n2
		}
	}
	rows := make([]Merchant, 0, len(byLEI))
	for _, m := range byLEI {
		if isRealHomepage(m.Homepage) {
			m.Profiles = append(m.Profiles, Profile{
				ExtID:    m.ExtID,
				Platform: PlatformWebsite,
				URL:      m.Homepage,
				Source:   "wikidata-p856",
			})
		}
		rows = append(rows, *m)
	}
	matched, profiles, err := dir.attachExisting(ctx, rows)
	st := IngestStats{
		Source: "wikidata-p856",
		Rows:   matched,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("chunked official sites, +%d bindings, %d profiles, %d failed", added, profiles, failed),
	}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, "wikidata-p856", started, matched, st.Note)
	logIngest("Wikidata P856 attached %d GLEIF", matched)
	return st
}

func (c *Client) fetchLEIWebsitePrefix(ctx context.Context, byLEI map[string]*Merchant, prefix string) (int, bool, error) {
	raw, err := c.fetchWikidataSPARQL(ctx, wikidataLEIWebsiteSPARQL(prefix))
	if err != nil {
		time.Sleep(2 * time.Second)
		raw, err = c.fetchWikidataSPARQL(ctx, wikidataLEIWebsiteSPARQL(prefix))
	}
	if err != nil {
		return 0, false, err
	}
	n := mergeWikidataLEIProp(byLEI, raw, "", PlatformWebsite)
	logIngest("Wikidata P856 %s +%d (entities=%d)", prefix, n, len(byLEI))
	time.Sleep(700 * time.Millisecond)
	return n, n >= leiWebsiteChunkLimit, nil
}

const leiWebsiteChunkLimit = 20000

func wikidataLEIWebsiteSPARQL(prefix string) string {
	prefix = strings.ToUpper(strings.TrimSpace(prefix))
	return fmt.Sprintf(`SELECT ?lei ?val WHERE {
  ?item wdt:P1278 ?lei ;
        wdt:P856 ?val .
  FILTER(STRSTARTS(STR(?lei), "%s"))
}
LIMIT %d`, prefix, leiWebsiteChunkLimit)
}

func leiStartPrefixes(width int) []string {
	alpha := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if width <= 1 {
		out := make([]string, 0, len(alpha))
		for i := 0; i < len(alpha); i++ {
			out = append(out, alpha[i:i+1])
		}
		return out
	}
	return expandLEIPrefix("")
}

func expandLEIPrefix(prefix string) []string {
	alpha := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	out := make([]string, 0, len(alpha))
	for i := 0; i < len(alpha); i++ {
		out = append(out, prefix+alpha[i:i+1])
	}
	return out
}

func (d *Directory) attachUniqueNameSocials(ctx context.Context) IngestStats {
	started := time.Now()
	if d == nil {
		return IngestStats{Source: "name-match", Took: time.Since(started), Note: "skipped"}
	}
	targets, err := d.loadBareGLEIFKeys(ctx)
	if err != nil {
		st := IngestStats{Source: "name-match", Took: time.Since(started), Err: err.Error()}
		_ = d.RecordRun(ctx, "name-match", started, 0, st.Err)
		return st
	}
	donors, err := d.loadSocialDonors(ctx)
	if err != nil {
		st := IngestStats{Source: "name-match", Took: time.Since(started), Err: err.Error()}
		_ = d.RecordRun(ctx, "name-match", started, 0, st.Err)
		return st
	}
	type slot struct {
		m        Merchant
		conflict bool
	}
	byKey := map[string]*slot{}
	for _, row := range donors {
		key := nameCountryKey(row.Name, row.Country)
		if key == "" {
			continue
		}
		if cur, ok := byKey[key]; ok {
			cur.conflict = true
			continue
		}
		row := row
		byKey[key] = &slot{m: row}
	}
	var rows []Merchant
	for key, extID := range targets {
		slot, ok := byKey[key]
		if !ok || slot.conflict {
			continue
		}
		copied := Merchant{
			ExtID:    extID,
			Source:   "gleif",
			Homepage: slot.m.Homepage,
		}
		for _, p := range slot.m.Profiles {
			p.ExtID = extID
			p.Source = "name-match"
			copied.Profiles = append(copied.Profiles, p)
		}
		if isRealHomepage(copied.Homepage) {
			copied.Profiles = append(copied.Profiles, Profile{
				ExtID:    extID,
				Platform: PlatformWebsite,
				URL:      copied.Homepage,
				Source:   "name-match",
			})
		}
		if copied.Homepage == "" && len(copied.Profiles) == 0 {
			continue
		}
		rows = append(rows, copied)
	}
	matched, profiles, err := d.attachExisting(ctx, rows)
	st := IngestStats{
		Source: "name-match",
		Rows:   matched,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("unique name+country, %d donors, %d profiles", len(donors), profiles),
	}
	if err != nil {
		st.Err = err.Error()
	}
	_ = d.RecordRun(ctx, "name-match", started, matched, st.Note)
	logIngest("name-match attached %d GLEIF from %d donors", matched, len(donors))
	return st
}

func (d *Directory) loadBareGLEIFKeys(ctx context.Context) (map[string]string, error) {
	have := map[string]struct{}{}
	prows, err := d.db.QueryContext(ctx, `SELECT DISTINCT ext_id FROM merchant_profiles`)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var id string
		if err := prows.Scan(&id); err != nil {
			_ = prows.Close()
			return nil, err
		}
		have[id] = struct{}{}
	}
	if err := prows.Err(); err != nil {
		_ = prows.Close()
		return nil, err
	}
	_ = prows.Close()

	rows, err := d.db.QueryContext(ctx, `SELECT ext_id, name, country, homepage FROM merchants WHERE source='gleif'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	conflict := map[string]bool{}
	n := 0
	for rows.Next() {
		var extID, name, country, home string
		if err := rows.Scan(&extID, &name, &country, &home); err != nil {
			return nil, err
		}
		n++
		if _, ok := have[extID]; ok {
			continue
		}
		if isRealHomepage(home) && !registryOnlyHomepage(home) {
			continue
		}
		key := nameCountryKey(name, country)
		if key == "" || conflict[key] {
			continue
		}
		if _, ok := out[key]; ok {
			delete(out, key)
			conflict[key] = true
			continue
		}
		out[key] = extID
	}
	logIngest("name-match scanned %d GLEIF, %d unique bare keys", n, len(out))
	return out, rows.Err()
}

func (d *Directory) loadSocialDonors(ctx context.Context) ([]Merchant, error) {
	rows, err := d.scanMerchants(ctx, `SELECT ext_id, source, name, shop, country, city, homepage, phone
		FROM merchants m
		WHERE m.source IN ('osm', 'wikidata', 'dork')
		   OR (m.source='gleif' AND EXISTS (
		        SELECT 1 FROM merchant_profiles p WHERE p.ext_id=m.ext_id
		      ))`)
	if err != nil {
		return nil, err
	}
	rows = d.attachProfiles(ctx, rows)
	out := make([]Merchant, 0, len(rows))
	for _, row := range rows {
		if (isRealHomepage(row.Homepage) && !registryOnlyHomepage(row.Homepage)) || len(row.Profiles) > 0 {
			out = append(out, row)
		}
	}
	return out, nil
}
