package engine

import (
	"testing"
	"time"
)

func TestSearchCacheRoundTrip(t *testing.T) {
	q := Query{Keyword: "shoes", Kind: KindCustoms, Role: RoleBuyer, Limit: 0}
	storeSearchCacheAlways(q, Result{
		Keyword: "shoes",
		Kind:    KindCustoms,
		Hits:    []Hit{{ID: "c1", Name: "FOOT LOCKER INC", Extra: map[string]string{"shipments": "48"}}},
	})
	got, ok := lookupSearchCacheAlways(q)
	if !ok || len(got.Hits) != 1 || got.Hits[0].Name != "FOOT LOCKER INC" {
		t.Fatalf("cache miss %+v ok=%v", got, ok)
	}
	got.Hits[0].Extra["shipments"] = "0"
	again, _ := lookupSearchCacheAlways(q)
	if again.Hits[0].Extra["shipments"] != "48" {
		t.Fatal("cache alias")
	}
}

func TestSearchCacheExpires(t *testing.T) {
	q := Query{Keyword: "empty-expire", Kind: KindPeople}
	storeSearchCacheAlways(q, Result{Kind: KindPeople})
	searchCacheMu.Lock()
	key := searchCacheKey(q)
	ent := searchCache[key]
	ent.at = time.Now().Add(-2 * time.Minute)
	searchCache[key] = ent
	searchCacheMu.Unlock()
	if _, ok := lookupSearchCacheAlways(q); ok {
		t.Fatal("expired empty result should miss")
	}
}

func TestLookupStaleCache(t *testing.T) {
	q := Query{Keyword: "stale-shoes", Kind: KindCustoms, Role: RoleBuyer}
	storeSearchCacheAlways(q, Result{
		Kind: KindCustoms,
		Hits: []Hit{{ID: "c1", Name: "FOOT LOCKER INC"}},
	})
	searchCacheMu.Lock()
	key := searchCacheKey(q)
	ent := searchCache[key]
	ent.at = time.Now().Add(-30 * time.Minute)
	searchCache[key] = ent
	searchCacheMu.Unlock()
	if _, ok := lookupSearchCacheAlways(q); ok {
		t.Fatal("fresh should miss")
	}
	got, ok := lookupCache(q, true)
	if !ok || len(got.Hits) != 1 || got.Hits[0].Name != "FOOT LOCKER INC" {
		t.Fatalf("stale miss %+v ok=%v", got, ok)
	}
}

func TestRefreshInFlightSurvivesProgressCache(t *testing.T) {
	q := Query{Keyword: "progress-led", Kind: KindPeople, Role: RoleBuyer}
	job := &refreshJob{done: make(chan struct{})}
	key := searchCacheKey(q)
	refreshJobsMu.Lock()
	refreshJobs[key] = job
	refreshJobsMu.Unlock()
	t.Cleanup(func() {
		refreshJobsMu.Lock()
		delete(refreshJobs, key)
		refreshJobsMu.Unlock()
		close(job.done)
	})
	if !refreshInFlight(q) {
		t.Fatal("refresh should be in flight")
	}
	storeSearchCacheAlways(q, Result{Kind: KindPeople, Hits: []Hit{{ID: "s1", Name: "LED Lighting Store"}}})
	got, ok := lookupSearchCacheAlways(q)
	if !ok || len(got.Hits) != 1 {
		t.Fatalf("progress cache miss %+v ok=%v", got, ok)
	}
	if !refreshInFlight(q) {
		t.Fatal("progress snapshot must not clear in-flight refresh")
	}
}

func TestCacheTTLFor(t *testing.T) {
	if cacheTTLFor(KindCustoms, 3) != customsCacheTTL {
		t.Fatal("customs ttl")
	}
	if cacheTTLFor(KindExhibition, 0) != emptyCacheTTL {
		t.Fatal("empty ttl")
	}
}
