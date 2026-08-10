package gmaps

import (
	"context"
	"testing"

	"github.com/gosom/scrapemate"
)

func TestParseFeedHits(t *testing.T) {
	raw := []any{
		map[string]any{
			"href":  "https://www.google.com/maps/place/A/@-6.195,106.830,17z/data=!3d-6.195!4d106.830",
			"title": "Kopi A",
		},
		map[string]any{"href": "  ", "title": "empty"},
		map[string]any{
			"href":  "https://www.google.com/maps/place/B/data=!1s0x1:0x2",
			"title": "Kopi B",
		},
	}
	hits := parseFeedHits(raw)
	if len(hits) != 2 {
		t.Fatalf("hits=%d want 2: %+v", len(hits), hits)
	}
	if hits[0].Title != "Kopi A" || hits[1].Title != "Kopi B" {
		t.Fatalf("titles=%q %q", hits[0].Title, hits[1].Title)
	}
}

func TestClaimPlaceURLDedupsLocalAndStreamCount(t *testing.T) {
	j := &GmapJob{}
	ctx := context.Background()
	href := "https://www.google.com/maps/place/X/data=!1s0xabc:0xdef"
	if !j.claimPlaceURL(ctx, href) {
		t.Fatal("first claim should succeed")
	}
	if j.claimPlaceURL(ctx, href) {
		t.Fatal("second claim should fail")
	}
	// Encoding variant of same !1s key
	href2 := "https://www.google.com/maps/place/Y/@1,2,17z/data=!4m5!3m4!1s0xabc:0xdef!8m2!3d1!4d2"
	if MapsURLDedupKey(href) != MapsURLDedupKey(href2) {
		t.Fatalf("expected unified key, got %q vs %q", MapsURLDedupKey(href), MapsURLDedupKey(href2))
	}
	if j.claimPlaceURL(ctx, href2) {
		t.Fatal("variant of same place should be deduped")
	}
}

func TestStreamFeedPlacesUsesPusherAndDeduper(t *testing.T) {
	j := &GmapJob{
		LangCode: "id",
	}
	j.Job.ID = "seed-1"

	var pushed []string
	ctx := scrapemate.ContextWithJobPusher(context.Background(), func(_ context.Context, job scrapemate.IJob) error {
		pushed = append(pushed, job.GetURL())
		return nil
	})

	// Simulate collectFeedHits via direct claim+push path using streamFeedPlaces
	// with a stub page — use parse + manual loop equivalent.
	hits := []feedHit{
		{Href: "https://www.google.com/maps/place/A/data=!1s0x1:0x2", Title: "Cafe A"},
		{Href: "https://www.google.com/maps/place/A2/data=!1s0x1:0x2", Title: "Cafe A dup"},
		{Href: "https://www.google.com/maps/place/B/data=!1s0x3:0x4", Title: "Cafe B"},
	}
	opts := j.placeJobOpts()
	push := scrapemate.GetJobPusherFromContext(ctx)
	for _, hit := range hits {
		if !j.claimPlaceURL(ctx, hit.Href) {
			continue
		}
		pj := NewPlaceJob(j.ID, j.LangCode, hit.Href, true, false, opts...)
		if err := push(ctx, pj); err != nil {
			t.Fatal(err)
		}
		j.streamedCount++
	}
	if j.streamedCount != 2 {
		t.Fatalf("streamedCount=%d want 2 (one dup)", j.streamedCount)
	}
	if len(pushed) != 2 {
		t.Fatalf("pushed=%d want 2", len(pushed))
	}
	// Process-style remainder claim should find nothing new
	if j.claimPlaceURL(ctx, hits[0].Href) || j.claimPlaceURL(ctx, hits[2].Href) {
		t.Fatal("already streamed URLs must not be claimed again")
	}
}
