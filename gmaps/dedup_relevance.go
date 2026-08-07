package gmaps

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gosom/google-maps-scraper/placeref"
)

var mapsURLCoordRe = regexp.MustCompile(`@(-?\d+\.?\d*),(-?\d+\.?\d*)`)
var mapsURL3d4dRe = regexp.MustCompile(`!3d(-?\d+\.?\d*)!4d(-?\d+\.?\d*)`)

// EntryDedupKey returns the scrape-time dedup key for an Entry.
// Prefer PlaceID / CID / data_id / Maps-URL feature id over raw href so
// fast-mode and deep-mode (and overlapping grid cells) collapse to one place.
func EntryDedupKey(e *Entry) string {
	if e == nil {
		return ""
	}
	key := placeref.StableKey(placeref.Identifiers{
		PlaceID:   e.PlaceID,
		Cid:       e.Cid,
		DataID:    e.DataID,
		Link:      e.Link,
		Title:     e.Title,
		Latitude:  e.Latitude,
		Longitude: e.Longtitude,
	})
	if key != "" {
		return key
	}
	if id := strings.TrimSpace(e.ID); id != "" {
		return "id:" + id
	}
	return ""
}

// MapsURLDedupKey normalizes a deep-mode feed href into a stable key.
func MapsURLDedupKey(href string) string {
	if key := placeref.KeyFromMapsURL(href); key != "" {
		return key
	}
	return strings.TrimSpace(href)
}

// EntryRelevant reports whether an Entry matches the scrape keywords.
func EntryRelevant(e *Entry, keywords []string) bool {
	if e == nil {
		return false
	}
	return placeref.Relevant(placeref.Place{
		Title:        e.Title,
		Category:     e.Category,
		Address:      e.Address,
		Descriptions: e.Description,
		About:        aboutText(e),
	}, keywords)
}

func aboutText(e *Entry) string {
	if e == nil || len(e.About) == 0 {
		return ""
	}
	parts := make([]string, 0, len(e.About))
	for _, a := range e.About {
		parts = append(parts, a.Name)
		for _, o := range a.Options {
			parts = append(parts, o.Name)
		}
	}
	return strings.Join(parts, " ")
}

// FilterEntriesByKeywords drops off-brief noise before email/CSV spawn.
func FilterEntriesByKeywords(entries []*Entry, keywords []string) []*Entry {
	if len(entries) == 0 || len(keywords) == 0 {
		return entries
	}
	out := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if EntryRelevant(e, keywords) {
			out = append(out, e)
		}
	}
	return out
}

// placeEntryShouldDrop reports whether a fully parsed place is off-brief or
// outside the job radius (meters). Empty filters disable that check.
func placeEntryShouldDrop(e *Entry, keywords []string, lat, lon, radiusM float64) bool {
	if e == nil {
		return true
	}
	if radiusM > 0 && (lat != 0 || lon != 0) {
		if e.Latitude != 0 || e.Longtitude != 0 {
			if !e.isWithinRadius(lat, lon, radiusM) {
				return true
			}
		}
	}
	if len(keywords) > 0 && !EntryRelevant(e, keywords) {
		return true
	}
	return false
}

// feedHitShouldSkip is a cheap pre-PlaceJob gate using feed title + URL coords.
// When signals are missing it returns false (keep) so we do not over-drop.
func feedHitShouldSkip(title, href string, keywords []string, lat, lon, radiusM float64) bool {
	plat, plon, ok := coordsFromMapsURL(href)
	if ok && radiusM > 0 && (lat != 0 || lon != 0) {
		tmp := &Entry{Latitude: plat, Longtitude: plon}
		if !tmp.isWithinRadius(lat, lon, radiusM) {
			return true
		}
	}
	title = strings.TrimSpace(title)
	if title == "" || len(keywords) == 0 {
		return false
	}
	// Feed aria-label is often "Name · Category · stars" — use as title only.
	if !placeref.Relevant(placeref.Place{Title: title}, keywords) {
		return true
	}
	return false
}

func coordsFromMapsURL(raw string) (lat, lon float64, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, false
	}
	if dec, err := url.QueryUnescape(raw); err == nil && dec != "" {
		raw = dec
	}
	if m := mapsURL3d4dRe.FindStringSubmatch(raw); len(m) == 3 {
		lat, err1 := strconv.ParseFloat(m[1], 64)
		lon, err2 := strconv.ParseFloat(m[2], 64)
		if err1 == nil && err2 == nil {
			return lat, lon, true
		}
	}
	if m := mapsURLCoordRe.FindStringSubmatch(raw); len(m) == 3 {
		lat, err1 := strconv.ParseFloat(m[1], 64)
		lon, err2 := strconv.ParseFloat(m[2], 64)
		if err1 == nil && err2 == nil {
			return lat, lon, true
		}
	}
	return 0, 0, false
}
