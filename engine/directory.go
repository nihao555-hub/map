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
	return &Directory{db: db}, nil
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
	return n, nil
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
	return out, nil
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
		if home == "" {
			continue
		}
		_, label := inferCountryFromText(row.Country, row.Country)
		snippet := strings.TrimSpace(strings.Join([]string{row.Shop, "店铺", row.City, label, row.Source}, " · "))
		plat := PlatformWebsite
		hit := Hit{
			ID:           row.Source + ":" + row.ExtID,
			Kind:         KindPeople,
			Platform:     plat,
			Name:         row.Name,
			Title:        row.Name,
			Snippet:      snippet,
			HomepageURL:  home,
			MessageURL:   home,
			MessageHint:  "打开目录里的官网或地图页。系统不会代发。",
			Score:        86,
			Country:      strings.ToUpper(strings.TrimSpace(row.Country)),
			CountryLabel: label,
			Extra:        map[string]string{"src": row.Source, "shop": row.Shop, "match": "category"},
		}
		if social, ok := ParseSocialURL(home, row.Name, snippet); ok {
			social.Name = row.Name
			social.Snippet = snippet
			social.Country = hit.Country
			social.CountryLabel = label
			if social.Extra == nil {
				social.Extra = map[string]string{}
			}
			social.Extra["src"] = row.Source
			social.Extra["shop"] = row.Shop
			social.Extra["match"] = "category"
			out = append(out, social)
			continue
		}
		out = append(out, hit)
	}
	return out
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
