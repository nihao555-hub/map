package gmaps

import (
	"github.com/gosom/google-maps-scraper/placecache"
	"github.com/gosom/google-maps-scraper/placeref"
)

func entryToCached(e *Entry) *placecache.CachedPlace {
	if e == nil {
		return nil
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
	if key == "" {
		key = MapsURLDedupKey(e.Link)
	}
	if key == "" {
		return nil
	}
	return &placecache.CachedPlace{
		Key:       key,
		PlaceID:   e.PlaceID,
		Cid:       e.Cid,
		DataID:    e.DataID,
		Link:      e.Link,
		Title:     e.Title,
		Category:  e.Category,
		Address:   e.Address,
		WebSite:   e.WebSite,
		Phone:     e.Phone,
		Latitude:  e.Latitude,
		Longitude: e.Longtitude,
		Emails:    append([]string(nil), e.Emails...),
		WhatsApp:  e.WhatsApp,
		Facebook:  e.Facebook,
		Instagram: e.Instagram,
		LinkedIn:  e.LinkedIn,
		Twitter:   e.Twitter,
		TikTok:    e.TikTok,
		YouTube:   e.YouTube,
	}
}

func cachedToEntry(c *placecache.CachedPlace) *Entry {
	if c == nil {
		return nil
	}
	return &Entry{
		Link:       c.Link,
		Cid:        c.Cid,
		Title:      c.Title,
		Category:   c.Category,
		Address:    c.Address,
		WebSite:    c.WebSite,
		Phone:      c.Phone,
		Latitude:   c.Latitude,
		Longtitude: c.Longitude,
		DataID:     c.DataID,
		PlaceID:    c.PlaceID,
		Emails:     append([]string(nil), c.Emails...),
		WhatsApp:   c.WhatsApp,
		Facebook:   c.Facebook,
		Instagram:  c.Instagram,
		LinkedIn:   c.LinkedIn,
		Twitter:    c.Twitter,
		TikTok:     c.TikTok,
		YouTube:    c.YouTube,
	}
}

func storeEntryInPlaceCache(e *Entry) {
	if c := entryToCached(e); c != nil {
		placecache.Store(c)
	}
}
