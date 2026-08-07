package gmaps

import (
	"strings"

	"github.com/gosom/google-maps-scraper/placeref"
)

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
