package engine

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	peopleCacheTTL     = 8 * time.Minute
	customsCacheTTL    = 20 * time.Minute
	exhibitionCacheTTL = 25 * time.Minute
	emptyCacheTTL      = 90 * time.Second
)

type cachedSearch struct {
	at  time.Time
	ttl time.Duration
	res Result
}

var (
	searchCacheMu sync.Mutex
	searchCache   = map[string]cachedSearch{}
)

func searchCacheKey(q Query) string {
	plats := append([]string(nil), q.Platforms...)
	return strings.ToLower(fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%t|%d|%d",
		q.Kind, q.Keyword, q.Role, q.Country, q.Mode, q.Channel,
		strings.Join(plats, ","), q.Precise, q.Limit, q.Year))
}

func cacheTTLFor(kind string, hits int) time.Duration {
	if hits == 0 {
		return emptyCacheTTL
	}
	switch kind {
	case KindCustoms:
		return customsCacheTTL
	case KindExhibition:
		return exhibitionCacheTTL
	default:
		return peopleCacheTTL
	}
}

func lookupSearchCache(q Query) (Result, bool) {
	if testing.Testing() {
		return Result{}, false
	}
	return lookupSearchCacheAlways(q)
}

func lookupSearchCacheAlways(q Query) (Result, bool) {
	key := searchCacheKey(q)
	searchCacheMu.Lock()
	ent, ok := searchCache[key]
	searchCacheMu.Unlock()
	if !ok {
		return Result{}, false
	}
	if time.Since(ent.at) > ent.ttl {
		return Result{}, false
	}
	return cloneResult(ent.res), true
}

func storeSearchCache(q Query, res Result) {
	if testing.Testing() {
		return
	}
	storeSearchCacheAlways(q, res)
}

func storeSearchCacheAlways(q Query, res Result) {
	key := searchCacheKey(q)
	ent := cachedSearch{
		at:  time.Now(),
		ttl: cacheTTLFor(firstNonEmpty(q.Kind, res.Kind), len(res.Hits)),
		res: cloneResult(res),
	}
	searchCacheMu.Lock()
	if searchCache == nil {
		searchCache = map[string]cachedSearch{}
	}
	searchCache[key] = ent
	if len(searchCache) > 256 {
		pruneSearchCacheLocked()
	}
	searchCacheMu.Unlock()
}

func pruneSearchCacheLocked() {
	now := time.Now()
	for k, ent := range searchCache {
		if now.Sub(ent.at) > ent.ttl {
			delete(searchCache, k)
		}
	}
}

func cloneResult(res Result) Result {
	out := res
	if res.Hits != nil {
		out.Hits = make([]Hit, len(res.Hits))
		copy(out.Hits, res.Hits)
		for i := range out.Hits {
			if out.Hits[i].Extra == nil {
				continue
			}
			extra := make(map[string]string, len(out.Hits[i].Extra))
			for k, v := range out.Hits[i].Extra {
				extra[k] = v
			}
			out.Hits[i].Extra = extra
		}
	}
	if res.Sources != nil {
		out.Sources = append([]string(nil), res.Sources...)
	}
	if res.Warnings != nil {
		out.Warnings = append([]string(nil), res.Warnings...)
	}
	if res.Expanded != nil {
		out.Expanded = append([]string(nil), res.Expanded...)
	}
	return out
}
