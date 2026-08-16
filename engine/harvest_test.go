package engine

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarvestDorkTermsPutsEnglishFirst(t *testing.T) {
	got := harvestDorkTerms("家具", "TH")
	if len(got) == 0 || hasCJK(got[0]) {
		t.Fatalf("want english first, got %+v", got)
	}
}

func TestKeepDorkHitDropsGenericPages(t *testing.T) {
	if keepDorkHit(Hit{Platform: PlatformFacebook, Handle: "videos", Name: "videos", HomepageURL: "https://www.facebook.com/videos"}) {
		t.Fatal("generic videos page")
	}
	if !keepDorkHit(Hit{Platform: PlatformFacebook, Handle: "LedWorldLighting", Name: "LED World Inc", HomepageURL: "https://www.facebook.com/LedWorldLighting"}) {
		t.Fatal("real shop dropped")
	}
}

func TestDorkExtID(t *testing.T) {
	id := dorkExtID(Hit{Platform: PlatformFacebook, Handle: "PowerbiltTools", HomepageURL: "https://www.facebook.com/PowerbiltTools"})
	if id != "dork:facebook:powerbilttools" {
		t.Fatalf("%s", id)
	}
}

func TestHarvestTradeDorksInsertsSocialHomepages(t *testing.T) {
	html := `<html><body>
	  <div id="search"><a href="/url?q=https://www.facebook.com/HarvestLEDShop&amp;sa=U">Harvest LED Shop</a></div>
	  <a href="https://www.instagram.com/harvestledshop">harvestledshop</a>
	</body></html>`
	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(html)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
		DisablePublic: false,
	}
	db := filepath.Join(t.TempDir(), "m.db")
	st, err := c.HarvestTradeDorks(context.Background(), HarvestOptions{
		DBPath:     db,
		Keywords:   []string{"LED灯"},
		Countries:  []string{""},
		QueryLimit: 4,
		Workers:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Hits < 1 || st.Inserted < 1 {
		t.Fatalf("stats=%+v", st)
	}
	dir, err := OpenDirectory(db)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	n, err := dir.CountSource(context.Background(), "dork")
	if err != nil || n < 1 {
		t.Fatalf("dork merchants=%d err=%v", n, err)
	}
	rows, err := dir.Search(context.Background(), "LED灯", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(merchantsToHits(rows)) == 0 {
		t.Fatalf("harvested socials missing from 找人: %+v", rows)
	}
}
