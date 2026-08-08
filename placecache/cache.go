// Package placecache provides a cross-job place_id → contacts cache so deep
// PlaceJobs can skip Playwright when the same merchant was scraped recently.
package placecache

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gosom/google-maps-scraper/placeref"
)

const defaultTTL = 14 * 24 * time.Hour

// CachedPlace is a compact contact-bearing snapshot reusable across scrape jobs.
type CachedPlace struct {
	Key        string   `json:"key"`
	PlaceID    string   `json:"place_id,omitempty"`
	Cid        string   `json:"cid,omitempty"`
	DataID     string   `json:"data_id,omitempty"`
	Link       string   `json:"link,omitempty"`
	Title      string   `json:"title,omitempty"`
	Category   string   `json:"category,omitempty"`
	Address    string   `json:"address,omitempty"`
	WebSite    string   `json:"web_site,omitempty"`
	Phone      string   `json:"phone,omitempty"`
	Latitude   float64  `json:"latitude,omitempty"`
	Longitude  float64  `json:"longitude,omitempty"`
	Emails     []string `json:"emails,omitempty"`
	WhatsApp   string   `json:"whatsapp,omitempty"`
	Facebook   string   `json:"facebook,omitempty"`
	Instagram  string   `json:"instagram,omitempty"`
	LinkedIn   string   `json:"linkedin,omitempty"`
	Twitter    string   `json:"twitter,omitempty"`
	TikTok     string   `json:"tiktok,omitempty"`
	YouTube    string   `json:"youtube,omitempty"`
	StoredAt   int64    `json:"stored_at"`
	FromCache  bool     `json:"-"`
}

var (
	mu     sync.RWMutex
	db     *sql.DB
	ttl    = defaultTTL
	hits   int64
	misses int64
	stores int64
)

// Init opens (or creates) the SQLite place cache under dataDir.
// Safe to call multiple times; subsequent calls with the same path are no-ops.
func Init(dataDir string) error {
	mu.Lock()
	defer mu.Unlock()
	if db != nil {
		return nil
	}
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dataDir, "place_cache.db")
	conn, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return err
	}
	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(2)
	if _, err := conn.Exec(`
CREATE TABLE IF NOT EXISTS places (
  key TEXT PRIMARY KEY,
  payload BLOB NOT NULL,
  stored_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_places_stored ON places(stored_at);
`); err != nil {
		_ = conn.Close()
		return err
	}
	db = conn
	log.Printf("placecache: opened %s ttl=%s", path, ttl)
	return nil
}

// SetTTL overrides the freshness window (tests / ops).
func SetTTL(d time.Duration) {
	if d <= 0 {
		return
	}
	mu.Lock()
	ttl = d
	mu.Unlock()
}

// Close releases the DB (tests).
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if db != nil {
		_ = db.Close()
		db = nil
	}
}

// Lookup returns a fresh cached place for key, or nil.
func Lookup(key string) *CachedPlace {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	mu.RLock()
	conn := db
	maxAge := ttl
	mu.RUnlock()
	if conn == nil {
		return nil
	}
	var payload []byte
	var storedAt int64
	err := conn.QueryRow(`SELECT payload, stored_at FROM places WHERE key = ?`, key).Scan(&payload, &storedAt)
	if err != nil {
		mu.Lock()
		misses++
		mu.Unlock()
		return nil
	}
	if time.Since(time.Unix(storedAt, 0)) > maxAge {
		mu.Lock()
		misses++
		mu.Unlock()
		return nil
	}
	var p CachedPlace
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	p.FromCache = true
	p.StoredAt = storedAt
	mu.Lock()
	hits++
	mu.Unlock()
	return &p
}

// LookupURL resolves a Maps place URL into a cache key then Lookup.
func LookupURL(mapsURL string) *CachedPlace {
	key := placeref.KeyFromMapsURL(mapsURL)
	if key == "" {
		key = strings.TrimSpace(mapsURL)
	}
	return Lookup(key)
}

// Store upserts a place snapshot under all useful keys (pid/cid/did/link).
func Store(p *CachedPlace) {
	if p == nil {
		return
	}
	mu.RLock()
	conn := db
	mu.RUnlock()
	if conn == nil {
		return
	}
	now := time.Now().Unix()
	p.StoredAt = now
	keys := collectKeys(p)
	if len(keys) == 0 {
		return
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return
	}
	tx, err := conn.Begin()
	if err != nil {
		return
	}
	stmt, err := tx.Prepare(`INSERT INTO places(key, payload, stored_at) VALUES(?,?,?)
ON CONFLICT(key) DO UPDATE SET payload=excluded.payload, stored_at=excluded.stored_at`)
	if err != nil {
		_ = tx.Rollback()
		return
	}
	defer stmt.Close()
	for _, k := range keys {
		if _, err := stmt.Exec(k, raw, now); err != nil {
			_ = tx.Rollback()
			return
		}
	}
	if err := tx.Commit(); err != nil {
		return
	}
	mu.Lock()
	stores++
	mu.Unlock()
}

func collectKeys(p *CachedPlace) []string {
	seen := map[string]struct{}{}
	add := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" {
			return
		}
		seen[k] = struct{}{}
	}
	add(p.Key)
	add(placeref.StableKey(placeref.Identifiers{
		PlaceID:   p.PlaceID,
		Cid:       p.Cid,
		DataID:    p.DataID,
		Link:      p.Link,
		Title:     p.Title,
		Latitude:  p.Latitude,
		Longitude: p.Longitude,
	}))
	add(placeref.KeyFromMapsURL(p.Link))
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// Stats returns hit/miss/store counters since process start.
func Stats() (h, m, s int64) {
	mu.RLock()
	defer mu.RUnlock()
	return hits, misses, stores
}
