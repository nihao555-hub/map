package web

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

type placesCacheEntry struct {
	modTime time.Time
	size    int64
	places  []Place
	lite    []PlaceLite
}

var placesCache sync.Map // jobID -> placesCacheEntry

const (
	maxPlacesCacheEntries = 8
	maxPlacesCacheBytes   = int64(16 << 20)
)

// PlaceLite is a compact place payload for map/table first paint.
type PlaceLite struct {
	Title        string  `json:"title,omitempty"`
	Category     string  `json:"category,omitempty"`
	Address      string  `json:"address,omitempty"`
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	Phone        string  `json:"phone,omitempty"`
	Website      string  `json:"website,omitempty"`
	Emails       string  `json:"emails,omitempty"`
	WhatsApp     string  `json:"whatsapp,omitempty"`
	Facebook     string  `json:"facebook,omitempty"`
	Instagram    string  `json:"instagram,omitempty"`
	LinkedIn     string  `json:"linkedin,omitempty"`
	ReviewRating float64 `json:"review_rating,omitempty"`
	ReviewCount  int     `json:"review_count,omitempty"`
	Thumbnail    string  `json:"thumbnail,omitempty"`
	PlaceID      string  `json:"place_id,omitempty"`
	Cid          string  `json:"cid,omitempty"`
}

func toPlaceLite(p Place) PlaceLite {
	return PlaceLite{
		Title:        p.Title,
		Category:     p.Category,
		Address:      p.Address,
		Latitude:     p.Latitude,
		Longitude:    p.Longitude,
		Phone:        p.Phone,
		Website:      p.Website,
		Emails:       p.Emails,
		WhatsApp:     p.WhatsApp,
		Facebook:     p.Facebook,
		Instagram:    p.Instagram,
		LinkedIn:     p.LinkedIn,
		ReviewRating: p.ReviewRating,
		ReviewCount:  p.ReviewCount,
		Thumbnail:    p.Thumbnail,
		PlaceID:      p.PlaceID,
		Cid:          p.Cid,
	}
}

func (s *Service) placesFileInfo(id string) (path string, mod time.Time, size int64, err error) {
	path, err = s.csvPath(id)
	if err != nil {
		return "", time.Time{}, 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return path, time.Time{}, 0, err
	}
	return path, st.ModTime(), st.Size(), nil
}

// GetPlacesCached returns parsed places, reusing an in-memory cache when the CSV is unchanged.
func (s *Service) GetPlacesCached(ctx context.Context, id string) ([]Place, error) {
	path, mod, size, err := s.placesFileInfo(id)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("csv file not found for job %s: %w", id, ErrPlacesNotFound)
		}
		return nil, err
	}
	if v, ok := placesCache.Load(id); ok {
		ent := v.(placesCacheEntry)
		if ent.modTime.Equal(mod) && ent.size == size && ent.places != nil {
			return ent.places, nil
		}
	}
	places, err := s.GetPlaces(ctx, id)
	if err != nil {
		return nil, err
	}
	lite := make([]PlaceLite, len(places))
	for i := range places {
		lite[i] = toPlaceLite(places[i])
	}
	placesCache.Store(id, placesCacheEntry{modTime: mod, size: size, places: places, lite: lite})
	trimPlacesCache()
	_ = path
	return places, nil
}

// GetPlacesLiteCached returns compact places for fast UI loads.
func (s *Service) GetPlacesLiteCached(ctx context.Context, id string) ([]PlaceLite, error) {
	_, mod, size, err := s.placesFileInfo(id)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("csv file not found for job %s: %w", id, ErrPlacesNotFound)
		}
		return nil, err
	}
	if v, ok := placesCache.Load(id); ok {
		ent := v.(placesCacheEntry)
		if ent.modTime.Equal(mod) && ent.size == size && ent.lite != nil {
			return ent.lite, nil
		}
	}
	places, err := s.GetPlacesCached(ctx, id)
	if err != nil {
		return nil, err
	}
	if v, ok := placesCache.Load(id); ok {
		return v.(placesCacheEntry).lite, nil
	}
	lite := make([]PlaceLite, len(places))
	for i := range places {
		lite[i] = toPlaceLite(places[i])
	}
	return lite, nil
}

// CountPlacesCached returns the number of mappable places without building a huge JSON payload.
func (s *Service) CountPlacesCached(ctx context.Context, id string) (int, error) {
	places, err := s.GetPlacesLiteCached(ctx, id)
	if err != nil {
		return 0, err
	}
	return len(places), nil
}

// trimPlacesCache bounds parsed CSV retention. []Place expands far beyond the
// file size; an unbounded history cache eventually consumed the RAM saved by
// fair browser admission when many users opened completed jobs.
func trimPlacesCache() {
	type item struct {
		id  string
		ent placesCacheEntry
	}
	items := make([]item, 0, maxPlacesCacheEntries+1)
	var total int64
	placesCache.Range(func(key, value any) bool {
		id, okID := key.(string)
		ent, okEnt := value.(placesCacheEntry)
		if okID && okEnt {
			items = append(items, item{id: id, ent: ent})
			total += ent.size
		}
		return true
	})
	if len(items) <= maxPlacesCacheEntries && total <= maxPlacesCacheBytes {
		return
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ent.modTime.Before(items[j].ent.modTime)
	})
	remaining := len(items)
	for _, candidate := range items {
		if remaining <= maxPlacesCacheEntries && total <= maxPlacesCacheBytes {
			break
		}
		if _, loaded := placesCache.LoadAndDelete(candidate.id); loaded {
			total -= candidate.ent.size
			remaining--
		}
	}
}
