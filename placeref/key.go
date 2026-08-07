// Package placeref provides stable place identity keys and keyword-domain
// relevance filters shared by scrape jobs, CSV writers, and the web API.
package placeref

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	ftidInURLRe    = regexp.MustCompile(`(?i)(?:!1s|1s)(0x[0-9a-f]+:0x[0-9a-f]+)`)
	cidInURLRe     = regexp.MustCompile(`(?i)(?:[?&]cid=)(\d{3,})`)
	placeIDURLRe   = regexp.MustCompile(`(?i)(?:place_id=|query_place_id=|q=place_id:)([A-Za-z0-9_-]{20,})`)
	chijInURLRe    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])((?:ChIJ|GhIJ)[A-Za-z0-9_-]{20,})`)
	ftidExactRe    = regexp.MustCompile(`(?i)^0x[0-9a-f]+:0x[0-9a-f]+$`)
	placeIDExactRe = regexp.MustCompile(`^(?:ChIJ|GhIJ)[A-Za-z0-9_-]{20,}$`)
)

// Identifiers are the raw IDs / link / geo fields available for a place.
type Identifiers struct {
	PlaceID   string
	Cid       string
	DataID    string
	Link      string
	Title     string
	Latitude  float64
	Longitude float64
}

// StableKey returns a canonical dedup key. Prefer PlaceID (ChIJ…), then CID,
// then feature/data ID (0x…:0x…), then IDs extracted from Maps URLs, then a
// title+coordinate hash. Prefixes keep namespaces from colliding.
func StableKey(id Identifiers) string {
	if pid := normalizePlaceID(id.PlaceID); pid != "" {
		return "pid:" + pid
	}
	if cid := strings.TrimSpace(id.Cid); cid != "" {
		return "cid:" + cid
	}
	if did := normalizeDataID(id.DataID); did != "" {
		return "did:" + did
	}
	if fromLink := KeyFromMapsURL(id.Link); fromLink != "" {
		return fromLink
	}
	title := normalizeTitle(id.Title)
	if title == "" && id.Latitude == 0 && id.Longitude == 0 {
		return ""
	}
	raw := fmt.Sprintf("%s|%.5f|%.5f", title, id.Latitude, id.Longitude)
	sum := sha1.Sum([]byte(raw))
	return "geo:" + hex.EncodeToString(sum[:10])
}

// KeyFromMapsURL extracts a stable key from a Google Maps place/search URL.
// Deep-mode feed hrefs often differ only by encoding; feature IDs unify them.
func KeyFromMapsURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	decoded, err := url.QueryUnescape(raw)
	if err == nil && decoded != "" {
		raw = decoded
	}
	if m := placeIDURLRe.FindStringSubmatch(raw); len(m) == 2 {
		if pid := normalizePlaceID(m[1]); pid != "" {
			return "pid:" + pid
		}
	}
	if m := chijInURLRe.FindStringSubmatch(raw); len(m) == 2 {
		if pid := normalizePlaceID(m[1]); pid != "" {
			return "pid:" + pid
		}
	}
	if m := ftidInURLRe.FindStringSubmatch(raw); len(m) == 2 {
		did := strings.ToLower(m[1])
		return "did:" + did
	}
	if m := cidInURLRe.FindStringSubmatch(raw); len(m) == 2 {
		return "cid:" + m[1]
	}
	return ""
}

// RowKey builds a CSV-row key from named columns (same semantics as StableKey).
func RowKey(get func(name string) string) string {
	lat, _ := strconv.ParseFloat(strings.TrimSpace(get("latitude")), 64)
	lon, _ := strconv.ParseFloat(strings.TrimSpace(get("longitude")), 64)
	return StableKey(Identifiers{
		PlaceID:   get("place_id"),
		Cid:       get("cid"),
		DataID:    get("data_id"),
		Link:      get("link"),
		Title:     get("title"),
		Latitude:  lat,
		Longitude: lon,
	})
}

func normalizePlaceID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Ignore synthetic geo_ keys previously stuffed into place_id by the API.
	if strings.HasPrefix(s, "geo_") || strings.HasPrefix(s, "geo:") {
		return ""
	}
	if strings.HasPrefix(s, "pid:") {
		s = strings.TrimPrefix(s, "pid:")
	}
	// Trust explicit ChIJ/GhIJ place ids from Maps (length varies slightly).
	if strings.HasPrefix(s, "ChIJ") || strings.HasPrefix(s, "GhIJ") {
		return s
	}
	if placeIDExactRe.MatchString(s) {
		return s
	}
	return ""
}

func normalizeDataID(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if ftidExactRe.MatchString(s) {
		return s
	}
	return ""
}

func normalizeTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevSpace = false
		case r > 127: // keep CJK / accented letters
			b.WriteRune(r)
			prevSpace = false
		default:
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}
