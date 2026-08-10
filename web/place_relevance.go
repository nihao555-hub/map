package web

import (
	"math"
	"strconv"
	"strings"

	"github.com/gosom/google-maps-scraper/placeref"
)

func placeToRef(p Place) placeref.Place {
	return placeref.Place{
		Title:        p.Title,
		Category:     p.Category,
		Address:      firstNonEmpty(p.Address, p.CompleteAddress),
		Descriptions: p.Descriptions,
		About:        p.About,
	}
}

func keywordTokens(kw string) []string {
	return placeref.KeywordTokens(kw)
}

// PlaceRelevantToKeywords keeps Maps hits that match the job's search domain.
func PlaceRelevantToKeywords(p Place, keywords []string) bool {
	return placeref.Relevant(placeToRef(p), keywords)
}

// FilterRelevantPlaces drops off-brief Maps noise for the job keywords.
func FilterRelevantPlaces(places []Place, keywords []string) []Place {
	if len(places) == 0 || len(keywords) == 0 {
		return places
	}
	out := make([]Place, 0, len(places))
	for _, p := range places {
		if PlaceRelevantToKeywords(p, keywords) {
			out = append(out, p)
		}
	}
	return out
}

// FilterRelevantPlacesLite is the compact-payload variant.
func FilterRelevantPlacesLite(places []PlaceLite, keywords []string) []PlaceLite {
	if len(places) == 0 || len(keywords) == 0 {
		return places
	}
	out := make([]PlaceLite, 0, len(places))
	for _, p := range places {
		full := Place{
			Title: p.Title, Category: p.Category, Address: p.Address,
			Phone: p.Phone, Website: p.Website, Emails: p.Emails,
			PlaceID: p.PlaceID, Cid: p.Cid,
		}
		if PlaceRelevantToKeywords(full, keywords) {
			out = append(out, p)
		}
	}
	return out
}

// DedupPlaces collapses duplicate rows by StablePlaceKey, keeping the richest
// contact score (then rating / review count).
func DedupPlaces(places []Place) []Place {
	if len(places) <= 1 {
		return places
	}
	type slot struct {
		p     Place
		order int
	}
	best := make(map[string]slot, len(places))
	order := make([]string, 0, len(places))
	for _, p := range places {
		ensurePlaceKey(&p)
		key := StablePlaceKey(p)
		if key == "" {
			key = placeref.StableKey(placeref.Identifiers{
				Title: p.Title, Latitude: p.Latitude, Longitude: p.Longitude,
			})
		}
		cur, ok := best[key]
		if !ok {
			best[key] = slot{p: p, order: len(order)}
			order = append(order, key)
			continue
		}
		if placeBetter(p, cur.p) {
			best[key] = slot{p: p, order: cur.order}
		}
	}
	out := make([]Place, 0, len(order))
	for _, key := range order {
		out = append(out, best[key].p)
	}
	return out
}

func placeBetter(a, b Place) bool {
	sa, sb := contactScore(a), contactScore(b)
	if sa != sb {
		return sa > sb
	}
	if a.ReviewRating != b.ReviewRating {
		return a.ReviewRating > b.ReviewRating
	}
	if a.ReviewCount != b.ReviewCount {
		return a.ReviewCount > b.ReviewCount
	}
	// Prefer the row that still has a real PlaceID over a synthetic geo key.
	aReal := strings.HasPrefix(strings.TrimSpace(a.PlaceID), "ChIJ") || strings.HasPrefix(strings.TrimSpace(a.PlaceID), "GhIJ")
	bReal := strings.HasPrefix(strings.TrimSpace(b.PlaceID), "ChIJ") || strings.HasPrefix(strings.TrimSpace(b.PlaceID), "GhIJ")
	if aReal != bReal {
		return aReal
	}
	return false
}

// DedupPlacesLite collapses compact payloads the same way.
func DedupPlacesLite(places []PlaceLite) []PlaceLite {
	if len(places) <= 1 {
		return places
	}
	type slot struct {
		p     PlaceLite
		order int
	}
	best := make(map[string]slot, len(places))
	order := make([]string, 0, len(places))
	for _, p := range places {
		full := Place{
			Title: p.Title, PlaceID: p.PlaceID, Cid: p.Cid,
			Latitude: p.Latitude, Longitude: p.Longitude,
			Phone: p.Phone, Website: p.Website, Emails: p.Emails,
			WhatsApp: p.WhatsApp, ReviewRating: p.ReviewRating, ReviewCount: p.ReviewCount,
		}
		key := StablePlaceKey(full)
		cur, ok := best[key]
		if !ok {
			best[key] = slot{p: p, order: len(order)}
			order = append(order, key)
			continue
		}
		if placeBetter(full, Place{
			Title: cur.p.Title, PlaceID: cur.p.PlaceID, Cid: cur.p.Cid,
			Latitude: cur.p.Latitude, Longitude: cur.p.Longitude,
			Phone: cur.p.Phone, Website: cur.p.Website, Emails: cur.p.Emails,
			WhatsApp: cur.p.WhatsApp, ReviewRating: cur.p.ReviewRating, ReviewCount: cur.p.ReviewCount,
		}) {
			best[key] = slot{p: p, order: cur.order}
		}
	}
	out := make([]PlaceLite, 0, len(order))
	for _, key := range order {
		out = append(out, best[key].p)
	}
	return out
}

// FilterPlacesForJob applies semantic relevance, geographic circle, and dedup.
func FilterPlacesForJob(places []Place, data JobData) []Place {
	places = FilterRelevantPlaces(places, data.Keywords)
	places = DedupPlaces(places)
	lat, lon, radiusKm, ok := jobRadiusAnchor(data)
	if !ok {
		if data.GridMode {
			return []Place{}
		}
		return places
	}
	if len(places) == 0 {
		return places
	}
	out := make([]Place, 0, len(places))
	for _, p := range places {
		if validPlaceCoord(p.Latitude, p.Longitude) &&
			haversineKm(lat, lon, p.Latitude, p.Longitude) <= radiusKm+0.25 {
			out = append(out, p)
		}
	}
	return out
}

func FilterPlacesLiteForJob(places []PlaceLite, data JobData) []PlaceLite {
	places = FilterRelevantPlacesLite(places, data.Keywords)
	places = DedupPlacesLite(places)
	lat, lon, radiusKm, ok := jobRadiusAnchor(data)
	if !ok {
		if data.GridMode {
			return []PlaceLite{}
		}
		return places
	}
	if len(places) == 0 {
		return places
	}
	out := make([]PlaceLite, 0, len(places))
	for _, p := range places {
		if validPlaceCoord(p.Latitude, p.Longitude) &&
			haversineKm(lat, lon, p.Latitude, p.Longitude) <= radiusKm+0.25 {
			out = append(out, p)
		}
	}
	return out
}

func jobRadiusAnchor(data JobData) (lat, lon, radiusKm float64, ok bool) {
	if data.Radius <= 0 {
		return 0, 0, 0, false
	}
	lat, errLat := strconv.ParseFloat(strings.TrimSpace(data.Lat), 64)
	lon, errLon := strconv.ParseFloat(strings.TrimSpace(data.Lon), 64)
	if errLat != nil || errLon != nil || !validPlaceCoord(lat, lon) || (lat == 0 && lon == 0) {
		return 0, 0, 0, false
	}
	return lat, lon, float64(data.Radius) / 1000, true
}

func validPlaceCoord(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0) &&
		lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0088
	toRad := math.Pi / 180
	dLat := (lat2 - lat1) * toRad
	dLon := (lon2 - lon1) * toRad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*toRad)*math.Cos(lat2*toRad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKm * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
