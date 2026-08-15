package engine

import (
	"context"
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
	staleKeepFor       = 6 * time.Hour
	realtimeBudget     = 950 * time.Millisecond
	refreshTimeout     = 45 * time.Second
)

type cachedSearch struct {
	at  time.Time
	ttl time.Duration
	res Result
}

type refreshJob struct {
	done chan struct{}
}

var (
	searchCacheMu sync.Mutex
	searchCache   = map[string]cachedSearch{}

	refreshJobsMu sync.Mutex
	refreshJobs   = map[string]*refreshJob{}
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
	return lookupCache(q, false)
}

func lookupStaleCache(q Query) (Result, bool) {
	if testing.Testing() {
		return Result{}, false
	}
	return lookupCache(q, true)
}

func lookupCache(q Query, staleOK bool) (Result, bool) {
	key := searchCacheKey(q)
	searchCacheMu.Lock()
	ent, ok := searchCache[key]
	searchCacheMu.Unlock()
	if !ok {
		return Result{}, false
	}
	age := time.Since(ent.at)
	if age <= ent.ttl {
		return cloneResult(ent.res), true
	}
	if staleOK && age <= staleKeepFor {
		return cloneResult(ent.res), true
	}
	return Result{}, false
}

func storeSearchCache(q Query, res Result) {
	if testing.Testing() {
		return
	}
	// Empty people results are often a transient index challenge.
	// Do not lock the UI on a 90s miss.
	if len(res.Hits) == 0 && firstNonEmpty(q.Kind, res.Kind) == KindPeople {
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
		if now.Sub(ent.at) > staleKeepFor {
			delete(searchCache, k)
		}
	}
}

func cloneResult(res Result) Result {
	out := res
	out.Cached = false
	out.Refreshing = false
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

func refreshInFlight(q Query) bool {
	key := searchCacheKey(q)
	refreshJobsMu.Lock()
	_, ok := refreshJobs[key]
	refreshJobsMu.Unlock()
	return ok
}

func kickSearchRefresh(c *Client, q Query) *refreshJob {
	key := searchCacheKey(q)
	refreshJobsMu.Lock()
	if job, ok := refreshJobs[key]; ok {
		refreshJobsMu.Unlock()
		return job
	}
	job := &refreshJob{done: make(chan struct{})}
	refreshJobs[key] = job
	refreshJobsMu.Unlock()

	go func() {
		defer func() {
			refreshJobsMu.Lock()
			delete(refreshJobs, key)
			refreshJobsMu.Unlock()
			close(job.done)
		}()
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		res, err := c.runKind(ctx, q)
		if err != nil {
			return
		}
		res = finalizeResult(q, res, time.Now())
		storeSearchCacheAlways(q, res)
	}()
	return job
}
