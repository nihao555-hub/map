package engine

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProbeableHandle(t *testing.T) {
	if probeableHandle("osdinlighting") == "" {
		t.Fatal("latin handle")
	}
	if probeableHandle("MS4wLjABAAAA5Xq2gT6p1gpKEZ7w0aTs") != "" {
		t.Fatal("douyin sec uid must not be probed")
	}
	if probeableHandle("小刘姐") != "" {
		t.Fatal("cjk nickname")
	}
	if probeableHandle("ab") != "" {
		t.Fatal("too short")
	}
	if probeableHandle("shop") != "" || probeableHandle("12345678") != "" {
		t.Fatal("generic or numeric handle must not be probed")
	}
}

func TestPickExpandSeedsPrefersLatinHandles(t *testing.T) {
	hits := []Hit{
		{Name: "灯具批发", Handle: "MS4wLjABAAAAFactory", Platform: PlatformDouyin, HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAFactory"},
		{Name: "Osdin Lighting", Handle: "osdinlighting", Platform: PlatformFacebook, HomepageURL: "https://www.facebook.com/osdinlighting"},
		{Name: "另一家工厂", Handle: "MS4wLjABAAAAOther", Platform: PlatformDouyin, HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAOther"},
	}
	got := pickExpandSeeds(hits, 2)
	if len(got) != 2 || got[0].Handle != "osdinlighting" {
		t.Fatalf("%+v", got)
	}
}

func TestExpandMerchantSocialsDedupedSeedsDoNotPanic(t *testing.T) {
	// pickExpandSeeds collapses duplicate URLs, so the seed list can be
	// shorter than maxExpandSeeds. The expand loop must use the collapsed length.
	same := "https://www.facebook.com/osdinlighting"
	hits := make([]Hit, maxExpandSeeds+4)
	for i := range hits {
		hits[i] = Hit{Name: "Osdin Lighting", Handle: "osdinlighting", Platform: PlatformFacebook, HomepageURL: same}
	}
	c := &Client{SkipExpand: false, DisablePublic: true}
	got := c.expandMerchantSocials(context.Background(), hits, nil)
	if got != nil {
		t.Fatalf("disabled public should skip expand, got %+v", got)
	}
	c.DisablePublic = false
	c.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`<html><title>Osdin Lighting</title><body>shop</body></html>`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	_ = c.expandMerchantSocials(context.Background(), hits, map[string]bool{PlatformFacebook: true, PlatformInstagram: true})
}

func TestMerchantProbeHandles(t *testing.T) {
	got := merchantProbeHandles(Merchant{Name: "Backwerk", Homepage: "https://www.openstreetmap.org/node/1"})
	if len(got) == 0 || got[0] != "Backwerk" {
		t.Fatalf("name handle: %v", got)
	}
	got = merchantProbeHandles(Merchant{Name: "Licht Kraus", Homepage: "https://www.licht-kraus.de"})
	joined := strings.Join(got, ",")
	if !strings.Contains(strings.ToLower(joined), "lichtkraus") {
		t.Fatalf("expected domain handle, got %v", got)
	}
	if len(merchantProbeHandles(Merchant{Name: "Licht Kraus", Homepage: "https://www.openstreetmap.org/node/1"})) != 0 {
		t.Fatal("multi-word map-only name must not invent a handle")
	}
	if len(merchantProbeHandles(Merchant{Name: "老王灯具", Homepage: "https://www.openstreetmap.org/node/2"})) != 0 {
		t.Fatal("CJK map-only must not be probed")
	}
}

func TestSameHandleURLs(t *testing.T) {
	urls := sameHandleURLs("osdinlighting")
	joined := strings.Join(urls, " ")
	for _, want := range []string{"instagram.com/osdinlighting", "tiktok.com/@osdinlighting", "youtube.com/@osdinlighting"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %v", want, urls)
		}
	}
}

func TestOutboundOfficialSitesSkipsSocial(t *testing.T) {
	html := []byte(`<a href="https://www.instagram.com/factorytools">ig</a>
<a href="https://factory-tools.com/about">site</a>
<a href="https://www.facebook.com/factorytools">fb</a>`)
	sites := outboundOfficialSites("https://www.facebook.com/factorytools", html, 3)
	if len(sites) != 1 || !strings.Contains(sites[0], "factory-tools.com") {
		t.Fatalf("%v", sites)
	}
}

func TestExpandOneMerchantFromPageLinks(t *testing.T) {
	html := `<html><body>
<a href="https://www.instagram.com/osdinlighting/">Instagram</a>
<a href="https://www.tiktok.com/@osdinlighting">TikTok</a>
<a href="https://www.youtube.com/watch?v=abc1234xxxx">video</a>
</body></html>`
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(html)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}}
	fetches := 0
	take := func() bool { fetches++; return fetches <= 8 }
	seed := Hit{
		Name:        "Osdin Lighting",
		Handle:      "osdinlighting",
		Platform:    PlatformFacebook,
		HomepageURL: "https://www.facebook.com/osdinlighting",
		Score:       100,
	}
	wanted := map[string]bool{PlatformFacebook: true, PlatformInstagram: true, PlatformTikTok: true, PlatformYouTube: true}
	got := c.expandOneMerchant(context.Background(), seed, wanted, take)
	var sawIG, sawTK, sawVideo bool
	for _, h := range got {
		if !isSocialHomepage(h) {
			t.Fatalf("non-homepage %+v", h)
		}
		if h.Platform == PlatformInstagram {
			sawIG = true
		}
		if h.Platform == PlatformTikTok {
			sawTK = true
		}
		if strings.Contains(h.HomepageURL, "/watch") || strings.Contains(h.HomepageURL, "/video/") {
			sawVideo = true
		}
	}
	if !sawIG || !sawTK {
		t.Fatalf("expected ig+tiktok homepages got %+v", got)
	}
	if sawVideo {
		t.Fatal("videos leaked")
	}
}

func TestProfileLooksGone(t *testing.T) {
	if !profileLooksGone("Sorry, this page isn't available.", []byte("instagram")) {
		t.Fatal("missing ig")
	}
	if profileLooksGone("Osdin Lighting Kepong", []byte("LED shop in KL")) {
		t.Fatal("false gone")
	}
}

func TestProbeProfileExistsRejectsMissing(t *testing.T) {
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`<title>Sorry, this page isn't available.</title>`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}}
	if c.probeProfileExists(context.Background(), "https://www.instagram.com/nope_nope_nope/") {
		t.Fatal("missing profile treated as real")
	}
}

func TestGroupExpandedHitsKeepsMerchantTogether(t *testing.T) {
	fb := Hit{Name: "Osdin Lighting", Handle: "osdinlighting", Platform: PlatformFacebook, HomepageURL: "https://www.facebook.com/osdinlighting", Score: 100}
	other := Hit{Name: "Other Shop", Handle: "othershop", Platform: PlatformFacebook, HomepageURL: "https://www.facebook.com/othershop", Score: 99}
	ig := Hit{Name: "Osdin Lighting", Handle: "osdinlighting", Platform: PlatformInstagram, HomepageURL: "https://www.instagram.com/osdinlighting/", Extra: map[string]string{"via": fb.HomepageURL}, Score: 92}
	got := groupExpandedHits([]Hit{fb, other, ig})
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if got[0].Platform != PlatformFacebook || got[1].Platform != PlatformInstagram || got[2].Platform != PlatformFacebook {
		t.Fatalf("order %+v %+v %+v", got[0].Platform, got[1].Platform, got[2].Platform)
	}
}

func TestSearchPeopleExpandsSisterSocials(t *testing.T) {
	indexHTML := `<html><body>
  <div class="result">
    <a class="result__a" href="https://www.facebook.com/osdinlighting">Osdin Lighting LED importer</a>
    <a class="result__snippet">LED lighting trading company</a>
  </div>
</body></html>`
	fbHTML := `<html><title>Osdin Lighting</title><body>
<a href="https://www.instagram.com/osdinlighting/">Instagram</a>
<a href="https://www.tiktok.com/@osdinlighting">TikTok</a>
</body></html>`
	gone := `<html><title>Sorry, this page isn't available.</title><body>isn't available</body></html>`
	okPage := func(title string) string {
		return `<html><title>` + title + `</title><body>` + title + ` lighting shop</body></html>`
	}

	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		host := req.URL.Host
		path := req.URL.Path
		html := gone
		switch {
		case strings.Contains(host, "duckduckgo"), strings.Contains(host, "bing"), strings.Contains(host, "brave"):
			html = indexHTML
		case strings.Contains(host, "facebook.com") && strings.Contains(path, "osdinlighting"):
			html = fbHTML
		case strings.Contains(host, "instagram.com") && strings.Contains(path, "osdinlighting"):
			html = okPage("Osdin Lighting")
		case strings.Contains(host, "tiktok.com") && strings.Contains(path, "osdinlighting"):
			html = okPage("Osdin Lighting")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(html)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}}

	res, err := c.Search(context.Background(), Query{
		Keyword:   "LED lighting",
		Kind:      KindPeople,
		Platforms: []string{PlatformFacebook, PlatformInstagram, PlatformTikTok},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sawFB, sawIG, sawTK bool
	mark := func(h Hit) {
		if !isSocialHomepage(h) {
			t.Fatalf("non-homepage %+v", h)
		}
		switch h.Platform {
		case PlatformFacebook:
			sawFB = true
		case PlatformInstagram:
			sawIG = true
		case PlatformTikTok:
			sawTK = true
		}
	}
	for _, h := range res.Hits {
		mark(h)
		for _, p := range h.Profiles {
			mark(p)
		}
	}
	if !sawFB || !sawIG || !sawTK {
		t.Fatalf("expected facebook+instagram+tiktok got %+v sources=%v", res.Hits, res.Sources)
	}
	joined := strings.Join(res.Sources, " ")
	if !strings.Contains(joined, "expand-socials") {
		t.Fatalf("sources=%v", res.Sources)
	}
}
