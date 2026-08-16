package engine

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DefaultMerchantDB is the on-disk dump used by 智能引擎 after full ingest.
const DefaultMerchantDB = "webdata/merchants.db"

const defaultMerchantDB = DefaultMerchantDB

// Merchant is one row in the local directory (OSM shops, GLEIF legal entities).
type Merchant struct {
	ExtID    string
	Source   string
	Name     string
	Shop     string
	Country  string
	City     string
	Homepage string
	Phone    string
	Profiles []Profile
}

// Profile is one verified or claimed homepage belonging to a merchant.
type Profile struct {
	ExtID    string
	Platform string
	URL      string
	Handle   string
	Verified bool
	Source   string
}

// Directory is a local SQLite merchant dump used by 智能引擎.
type Directory struct {
	db *sql.DB
}

func OpenDirectory(path string) (*Directory, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultMerchantDB
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(8000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(directorySchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	d := &Directory{db: db}
	d.migrate()
	return d, nil
}

func (d *Directory) migrate() {
	if d == nil || d.db == nil {
		return
	}
	_, _ = d.db.Exec(`ALTER TABLE merchants ADD COLUMN enriched_at TEXT`)
	_, _ = d.db.Exec(`ALTER TABLE merchants ADD COLUMN probed_at TEXT`)
}

const directorySchema = `
CREATE TABLE IF NOT EXISTS merchants (
  id INTEGER PRIMARY KEY,
  ext_id TEXT NOT NULL UNIQUE,
  source TEXT NOT NULL,
  name TEXT NOT NULL,
  shop TEXT,
  country TEXT,
  city TEXT,
  homepage TEXT,
  phone TEXT
);
CREATE INDEX IF NOT EXISTS merchants_shop ON merchants(shop);
CREATE INDEX IF NOT EXISTS merchants_source ON merchants(source);
CREATE INDEX IF NOT EXISTS merchants_country ON merchants(country);
CREATE VIRTUAL TABLE IF NOT EXISTS merchants_fts USING fts5(
  name, shop, city, country, content='merchants', content_rowid='id'
);
CREATE TRIGGER IF NOT EXISTS merchants_ai AFTER INSERT ON merchants BEGIN
  INSERT INTO merchants_fts(rowid, name, shop, city, country)
  VALUES (new.id, new.name, new.shop, new.city, new.country);
END;
CREATE TABLE IF NOT EXISTS ingest_runs (
  id INTEGER PRIMARY KEY,
  source TEXT,
  started TEXT,
  finished TEXT,
  rows INTEGER,
  ms INTEGER,
  note TEXT
);
CREATE TABLE IF NOT EXISTS merchant_profiles (
  id INTEGER PRIMARY KEY,
  ext_id TEXT NOT NULL,
  platform TEXT NOT NULL,
  url TEXT NOT NULL,
  handle TEXT,
  verified INTEGER NOT NULL DEFAULT 0,
  source TEXT,
  updated TEXT,
  UNIQUE(ext_id, platform, url)
);
CREATE INDEX IF NOT EXISTS merchant_profiles_ext ON merchant_profiles(ext_id);
CREATE INDEX IF NOT EXISTS merchant_profiles_plat ON merchant_profiles(platform);
`

func (d *Directory) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

func (d *Directory) Count(ctx context.Context) (int, error) {
	if d == nil {
		return 0, nil
	}
	var n int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM merchants`).Scan(&n)
	return n, err
}

func (d *Directory) CountSource(ctx context.Context, source string) (int, error) {
	if d == nil {
		return 0, nil
	}
	var n int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM merchants WHERE source=?`, source).Scan(&n)
	return n, err
}

func (d *Directory) InsertBatch(ctx context.Context, rows []Merchant) (int, error) {
	if d == nil || len(rows) == 0 {
		return 0, nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO merchants(ext_id, source, name, shop, country, city, homepage, phone)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	defer stmt.Close()
	n := 0
	for _, row := range rows {
		if strings.TrimSpace(row.Name) == "" || strings.TrimSpace(row.ExtID) == "" {
			continue
		}
		res, err := stmt.ExecContext(ctx, row.ExtID, row.Source, row.Name, row.Shop, row.Country, row.City, row.Homepage, row.Phone)
		if err != nil {
			_ = tx.Rollback()
			return n, err
		}
		if aff, _ := res.RowsAffected(); aff > 0 {
			n++
		}
	}
	if err := tx.Commit(); err != nil {
		return n, err
	}
	var profiles []Profile
	for _, row := range rows {
		profiles = append(profiles, row.Profiles...)
	}
	if err := d.UpsertProfiles(ctx, profiles); err != nil {
		return n, err
	}
	return n, nil
}

func (d *Directory) UpsertProfiles(ctx context.Context, rows []Profile) error {
	if d == nil || len(rows) == 0 {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO merchant_profiles(ext_id, platform, url, handle, verified, source, updated)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ext_id, platform, url) DO UPDATE SET
			handle=excluded.handle,
			verified=CASE WHEN excluded.verified=1 THEN 1 ELSE merchant_profiles.verified END,
			source=excluded.source,
			updated=excluded.updated`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, row := range rows {
		if strings.TrimSpace(row.ExtID) == "" || strings.TrimSpace(row.URL) == "" || strings.TrimSpace(row.Platform) == "" {
			continue
		}
		verified := 0
		if row.Verified {
			verified = 1
		}
		if _, err := stmt.ExecContext(ctx, row.ExtID, row.Platform, row.URL, row.Handle, verified, row.Source, now); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (d *Directory) ProfilesFor(ctx context.Context, extIDs []string) (map[string][]Profile, error) {
	out := map[string][]Profile{}
	if d == nil || len(extIDs) == 0 {
		return out, nil
	}
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(extIDs))
	for _, id := range extIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	const chunk = 400
	for i := 0; i < len(ids); i += chunk {
		end := i + chunk
		if end > len(ids) {
			end = len(ids)
		}
		part := ids[i:end]
		pholders := make([]string, len(part))
		args := make([]any, len(part))
		for j, id := range part {
			pholders[j] = "?"
			args[j] = id
		}
		q := `SELECT ext_id, platform, url, handle, verified, source FROM merchant_profiles WHERE ext_id IN (` + strings.Join(pholders, ",") + `)`
		rows, err := d.db.QueryContext(ctx, q, args...)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var p Profile
			var verified int
			if err := rows.Scan(&p.ExtID, &p.Platform, &p.URL, &p.Handle, &verified, &p.Source); err != nil {
				_ = rows.Close()
				return out, err
			}
			p.Verified = verified == 1
			out[p.ExtID] = append(out[p.ExtID], p)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func (d *Directory) attachProfiles(ctx context.Context, rows []Merchant) []Merchant {
	if len(rows) == 0 {
		return rows
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ExtID)
	}
	byID, err := d.ProfilesFor(ctx, ids)
	if err != nil {
		return rows
	}
	for i := range rows {
		rows[i].Profiles = byID[rows[i].ExtID]
	}
	return rows
}

func (d *Directory) MarkEnriched(ctx context.Context, extIDs []string) error {
	if d == nil || len(extIDs) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `UPDATE merchants SET enriched_at=? WHERE ext_id=?`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, id := range extIDs {
		if _, err := stmt.ExecContext(ctx, now, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (d *Directory) ListToEnrich(ctx context.Context, limit int) ([]Merchant, error) {
	if d == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT ext_id, source, name, shop, country, city, homepage, phone FROM merchants
		WHERE source IN ('osm', 'wikidata', 'gleif')
		  AND homepage NOT LIKE '%openstreetmap.org%'
		  AND homepage NOT LIKE '%gleif.org%'
		  AND homepage NOT LIKE '%wikidata.org%'
		  AND (enriched_at IS NULL OR enriched_at='')
		ORDER BY id
		LIMIT ?`
	rows, err := d.scanMerchants(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	return d.attachProfiles(ctx, rows), nil
}

// ListToProbe returns OSM merchants that still have no social homepage so we
// can try same-handle / domain-handle probes. Website-only rows are included
// once; map-only shops need a Latin handle-like name.
func (d *Directory) ListToProbe(ctx context.Context, limit int) ([]Merchant, error) {
	if d == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT m.ext_id, m.source, m.name, m.shop, m.country, m.city, m.homepage, m.phone
		FROM merchants m
		WHERE m.source IN ('osm', 'wikidata')
		  AND (m.probed_at IS NULL OR m.probed_at='')
		  AND NOT EXISTS (
		    SELECT 1 FROM merchant_profiles p
		    WHERE p.ext_id=m.ext_id AND p.platform != 'website'
		  )`
	rows, err := d.scanMerchants(ctx, q)
	if err != nil {
		return nil, err
	}
	var out []Merchant
	for _, row := range rows {
		if len(merchantProbeHandles(row)) == 0 {
			continue
		}
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	return d.attachProfiles(ctx, out), nil
}

// ListUnverifiedProfiles returns stored social URLs that have not passed a live probe.
func (d *Directory) ListUnverifiedProfiles(ctx context.Context, limit int) ([]Profile, error) {
	if d == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}
	rows, err := d.db.QueryContext(ctx, `
SELECT ext_id, platform, url, handle, verified, source
FROM merchant_profiles
WHERE verified=0 AND platform != 'website'
ORDER BY id
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		var p Profile
		var verified int
		if err := rows.Scan(&p.ExtID, &p.Platform, &p.URL, &p.Handle, &verified, &p.Source); err != nil {
			return nil, err
		}
		p.Verified = verified == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *Directory) HasExtID(ctx context.Context, extID string) (bool, error) {
	if d == nil || strings.TrimSpace(extID) == "" {
		return false, nil
	}
	var n int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM merchants WHERE ext_id=?`, extID).Scan(&n)
	return n > 0, err
}

func (d *Directory) SetHomepage(ctx context.Context, extID, home string) error {
	if d == nil || strings.TrimSpace(extID) == "" || strings.TrimSpace(home) == "" {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `
UPDATE merchants SET homepage=?
WHERE ext_id=? AND (homepage IS NULL OR homepage='' OR homepage LIKE '%gleif.org%' OR homepage LIKE '%wikidata.org%')`,
		home, extID)
	return err
}

func (d *Directory) MarkProbed(ctx context.Context, extIDs []string) error {
	if d == nil || len(extIDs) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `UPDATE merchants SET probed_at=? WHERE ext_id=?`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, id := range extIDs {
		if _, err := stmt.ExecContext(ctx, now, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// DirectoryInventory is a snapshot of the local 智能引擎 dump.
type DirectoryInventory struct {
	Merchants        int            `json:"merchants"`
	GLEIF            int            `json:"gleif"`
	OSM              int            `json:"osm"`
	Profiles         int            `json:"profiles"`
	VerifiedProfiles int            `json:"verified_profiles"`
	OSMWithSocial    int            `json:"osm_with_social"`
	ByPlatform       map[string]int `json:"by_platform,omitempty"`
}

func (d *Directory) Inventory(ctx context.Context) (DirectoryInventory, error) {
	var inv DirectoryInventory
	if d == nil {
		return inv, nil
	}
	var err error
	if inv.Merchants, err = d.Count(ctx); err != nil {
		return inv, err
	}
	if inv.GLEIF, err = d.CountSource(ctx, "gleif"); err != nil {
		return inv, err
	}
	if inv.OSM, err = d.CountSource(ctx, "osm"); err != nil {
		return inv, err
	}
	if inv.Profiles, err = d.CountProfiles(ctx); err != nil {
		return inv, err
	}
	if inv.VerifiedProfiles, err = d.CountVerifiedProfiles(ctx); err != nil {
		return inv, err
	}
	err = d.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT m.ext_id) FROM merchants m
JOIN merchant_profiles p ON p.ext_id=m.ext_id
WHERE m.source='osm' AND p.platform != 'website'`).Scan(&inv.OSMWithSocial)
	if err != nil {
		return inv, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT platform, COUNT(*) FROM merchant_profiles GROUP BY platform`)
	if err != nil {
		return inv, err
	}
	defer rows.Close()
	inv.ByPlatform = map[string]int{}
	for rows.Next() {
		var plat string
		var n int
		if err := rows.Scan(&plat, &n); err != nil {
			return inv, err
		}
		inv.ByPlatform[plat] = n
	}
	return inv, rows.Err()
}

func (d *Directory) CountProfiles(ctx context.Context) (int, error) {
	if d == nil {
		return 0, nil
	}
	var n int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM merchant_profiles`).Scan(&n)
	return n, err
}

func (d *Directory) CountVerifiedProfiles(ctx context.Context) (int, error) {
	if d == nil {
		return 0, nil
	}
	var n int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM merchant_profiles WHERE verified=1`).Scan(&n)
	return n, err
}

func (d *Directory) beginBulk(ctx context.Context) error {
	if d == nil {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `
PRAGMA cache_size=-200000;
PRAGMA temp_store=MEMORY;
PRAGMA synchronous=OFF;
DROP TRIGGER IF EXISTS merchants_ai;
`)
	return err
}

func (d *Directory) endBulk(ctx context.Context) error {
	if d == nil {
		return nil
	}
	if _, err := d.db.ExecContext(ctx, `INSERT INTO merchants_fts(merchants_fts) VALUES('rebuild')`); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `
CREATE TRIGGER IF NOT EXISTS merchants_ai AFTER INSERT ON merchants BEGIN
  INSERT INTO merchants_fts(rowid, name, shop, city, country)
  VALUES (new.id, new.name, new.shop, new.city, new.country);
END;
PRAGMA synchronous=NORMAL;
`)
	return err
}

func (d *Directory) RecordRun(ctx context.Context, source string, started time.Time, rows int, note string) error {
	if d == nil {
		return nil
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO ingest_runs(source, started, finished, rows, ms, note) VALUES (?, ?, ?, ?, ?, ?)`,
		source, started.UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), rows, time.Since(started).Milliseconds(), note)
	return err
}

func (d *Directory) Search(ctx context.Context, keyword, country string, limit int) ([]Merchant, error) {
	if d == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 2000 {
		limit = 800
	}
	terms := directoryQueryTerms(keyword)
	if len(terms) == 0 {
		return nil, nil
	}
	var out []Merchant
	seen := map[string]struct{}{}
	add := func(rows []Merchant) {
		for _, row := range rows {
			if _, ok := seen[row.ExtID]; ok {
				continue
			}
			seen[row.ExtID] = struct{}{}
			out = append(out, row)
		}
	}
	if tags := shopTagsForKeyword(keyword); len(tags) > 0 {
		shopRows, err := d.searchByShop(ctx, tags, country, limit)
		if err != nil {
			return nil, err
		}
		add(shopRows)
	}
	if len(out) < limit {
		ftsRows, err := d.searchFTS(ctx, terms, country, limit-len(out))
		if err != nil {
			return nil, err
		}
		add(ftsRows)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return d.attachProfiles(ctx, out), nil
}

func (d *Directory) searchByShop(ctx context.Context, tags []string, country string, limit int) ([]Merchant, error) {
	args := make([]any, 0, len(tags)+2)
	pholders := make([]string, 0, len(tags))
	for _, tag := range tags {
		pholders = append(pholders, "?")
		args = append(args, tag)
	}
	q := `SELECT ext_id, source, name, shop, country, city, homepage, phone FROM merchants WHERE shop IN (` + strings.Join(pholders, ",") + `)`
	if code := strings.ToUpper(strings.TrimSpace(LookupCountry(country).Code)); code != "" {
		q += ` AND country=?`
		args = append(args, code)
	}
	q += ` LIMIT ?`
	args = append(args, limit)
	return d.scanMerchants(ctx, q, args...)
}

func (d *Directory) searchFTS(ctx context.Context, terms []string, country string, limit int) ([]Merchant, error) {
	match := ftsMatchQuery(terms)
	q := `SELECT m.ext_id, m.source, m.name, m.shop, m.country, m.city, m.homepage, m.phone
		FROM merchants_fts f JOIN merchants m ON m.id=f.rowid
		WHERE merchants_fts MATCH ?`
	args := []any{match}
	if code := strings.ToUpper(strings.TrimSpace(LookupCountry(country).Code)); code != "" {
		q += ` AND m.country=?`
		args = append(args, code)
	}
	q += ` LIMIT ?`
	args = append(args, limit)
	return d.scanMerchants(ctx, q, args...)
}

func (d *Directory) scanMerchants(ctx context.Context, q string, args ...any) ([]Merchant, error) {
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Merchant
	for rows.Next() {
		var m Merchant
		if err := rows.Scan(&m.ExtID, &m.Source, &m.Name, &m.Shop, &m.Country, &m.City, &m.Homepage, &m.Phone); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func directoryQueryTerms(keyword string) []string {
	out := []string{strings.TrimSpace(keyword)}
	out = append(out, productSearchAliases(keyword)...)
	out = append(out, shopTagsForKeyword(keyword)...)
	return uniqueFoldedStrings(out)
}

func ftsMatchQuery(terms []string) string {
	parts := make([]string, 0, len(terms))
	for _, term := range uniqueFoldedStrings(terms) {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		term = strings.ReplaceAll(term, `"`, "")
		if strings.ContainsAny(term, " \t") {
			parts = append(parts, `"`+term+`"`)
		} else {
			parts = append(parts, term)
		}
	}
	return strings.Join(parts, " OR ")
}

func merchantsToHits(rows []Merchant) []Hit {
	out := make([]Hit, 0, len(rows))
	for _, row := range rows {
		home := strings.TrimSpace(row.Homepage)
		if home == "" && len(row.Profiles) == 0 {
			continue
		}
		_, label := inferCountryFromText(row.Country, row.Country)
		snippet := strings.TrimSpace(strings.Join([]string{row.Shop, "店铺", row.City, label, row.Source}, " · "))
		hit := Hit{
			ID:           row.Source + ":" + row.ExtID,
			Kind:         KindPeople,
			Platform:     PlatformWebsite,
			Name:         row.Name,
			Title:        row.Name,
			Snippet:      snippet,
			HomepageURL:  home,
			MessageURL:   home,
			MessageHint:  "打开已验证的官网或社媒主页。系统不会代发。",
			Score:        86,
			Country:      strings.ToUpper(strings.TrimSpace(row.Country)),
			CountryLabel: label,
			Extra:        map[string]string{"src": row.Source, "shop": row.Shop, "match": "category", "ext_id": row.ExtID},
		}
		if social, ok := ParseSocialURL(home, row.Name, snippet); ok {
			social.Name = row.Name
			social.Snippet = snippet
			social.Country = hit.Country
			social.CountryLabel = label
			social.ID = hit.ID
			if social.Extra == nil {
				social.Extra = map[string]string{}
			}
			social.Extra["src"] = row.Source
			social.Extra["shop"] = row.Shop
			social.Extra["match"] = "category"
			social.Extra["ext_id"] = row.ExtID
			hit = social
		}
		for _, p := range row.Profiles {
			ph := profileToHit(row, p, snippet, label)
			if ph.HomepageURL == "" {
				continue
			}
			if strings.EqualFold(ph.HomepageURL, hit.HomepageURL) {
				hit.Verified = hit.Verified || p.Verified
				hit.Handle = firstNonEmpty(hit.Handle, p.Handle)
				continue
			}
			hit.Profiles = append(hit.Profiles, ph)
			if p.Verified {
				hit.Score += 6
			}
		}
		if hit.HomepageURL == "" && len(hit.Profiles) > 0 {
			hit.HomepageURL = hit.Profiles[0].HomepageURL
			hit.MessageURL = hit.Profiles[0].MessageURL
			hit.Platform = hit.Profiles[0].Platform
			hit.Handle = hit.Profiles[0].Handle
			hit.Verified = hit.Profiles[0].Verified
			hit.Profiles = hit.Profiles[1:]
		}
		if registryOnlyHomepage(hit.HomepageURL) && len(hit.Profiles) == 0 && row.Source == "gleif" {
			continue
		}
		out = append(out, hit)
	}
	return out
}

func profileToHit(row Merchant, p Profile, snippet, label string) Hit {
	h := Hit{
		ID:           row.Source + ":" + row.ExtID + ":" + p.Platform,
		Kind:         KindPeople,
		Platform:     p.Platform,
		Name:         row.Name,
		Title:        row.Name,
		Handle:       p.Handle,
		Snippet:      snippet,
		HomepageURL:  p.URL,
		MessageURL:   p.URL,
		MessageHint:  "打开已验证的社媒主页。系统不会代发。",
		Score:        90,
		Verified:     p.Verified,
		Country:      strings.ToUpper(strings.TrimSpace(row.Country)),
		CountryLabel: label,
		Extra:        map[string]string{"src": row.Source, "shop": row.Shop, "match": "category", "ext_id": row.ExtID, "via": row.Homepage},
	}
	if social, ok := ParseSocialURL(p.URL, row.Name, snippet); ok {
		social.ID = h.ID
		social.Name = row.Name
		social.Title = row.Name
		social.Snippet = snippet
		social.Verified = p.Verified
		social.Country = h.Country
		social.CountryLabel = label
		if social.Extra == nil {
			social.Extra = map[string]string{}
		}
		social.Extra["src"] = row.Source
		social.Extra["shop"] = row.Shop
		social.Extra["match"] = "category"
		social.Extra["ext_id"] = row.ExtID
		social.Extra["via"] = row.Homepage
		return social
	}
	return h
}

func registryOnlyHomepage(raw string) bool {
	u := strings.ToLower(raw)
	return strings.Contains(u, "search.gleif.org") || strings.Contains(u, "goldencopy.gleif.org")
}

var (
	directoryOnce sync.Once
	directoryInst *Directory
)

func (c *Client) directory() *Directory {
	if c == nil {
		return nil
	}
	if c.dir != nil {
		return c.dir
	}
	path := strings.TrimSpace(c.MerchantDB)
	if path == "" {
		return nil
	}
	directoryOnce.Do(func() {
		d, err := OpenDirectory(path)
		if err == nil {
			directoryInst = d
		}
	})
	return directoryInst
}

func (c *Client) searchDirectory(ctx context.Context, keyword, country string) ([]Hit, error) {
	d := c.directory()
	if d == nil {
		return nil, nil
	}
	rows, err := d.Search(ctx, keyword, country, 800)
	if err != nil {
		return nil, err
	}
	return merchantsToHits(rows), nil
}
