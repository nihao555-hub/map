package engine

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSocialBelongsToMerchant(t *testing.T) {
	hit := Hit{Name: "AC Wholesale Electric", Title: "AC Wholesale Electric | Facebook", HomepageURL: "https://www.facebook.com/acwholesaleelectric"}
	if !socialBelongsToMerchant("AC Wholesale Electric", hit) {
		t.Fatal("exact name")
	}
	if socialBelongsToMerchant("AC Wholesale Electric", Hit{Name: "Wholesale Electric Supply", HomepageURL: "https://www.facebook.com/wes"}) {
		t.Fatal("partial generic name must not match")
	}
}

func TestParseOSMExtID(t *testing.T) {
	kind, id, ok := parseOSMExtID("osm:node:9453724561")
	if !ok || kind != "node" || id != 9453724561 {
		t.Fatalf("%s %d %v", kind, id, ok)
	}
	if _, _, ok := parseOSMExtID("gleif:001"); ok {
		t.Fatal("gleif")
	}
}

func TestAttachMissingSocialsFromNameSearch(t *testing.T) {
	html := `
<html><body>
  <div class="result">
    <a class="result__a" href="https://www.facebook.com/acwholesaleelectric">AC Wholesale Electric</a>
  </div>
  <div class="result">
    <a class="result__a" href="https://www.facebook.com/randomshop">Random Shop</a>
  </div>
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
		OverpassURL: "http://127.0.0.1:1",
	}
	hits := []Hit{{
		Name:        "AC Wholesale Electric",
		Platform:    PlatformWebsite,
		HomepageURL: "https://www.openstreetmap.org/node/1",
		Country:     "US",
		Extra:       map[string]string{"src": "osm", "ext_id": "osm:node:1", "city": "Houston"},
	}}
	got := c.attachMissingSocials(context.Background(), hits, map[string]bool{PlatformFacebook: true})
	if !hitHasSocial(got[0]) {
		t.Fatalf("expected facebook attach, got %+v", got[0])
	}
	saw := false
	for _, p := range append([]Hit{got[0]}, got[0].Profiles...) {
		if p.Platform == PlatformFacebook && strings.Contains(p.HomepageURL, "acwholesaleelectric") {
			saw = true
		}
		if strings.Contains(p.HomepageURL, "randomshop") {
			t.Fatalf("unrelated shop attached: %+v", p)
		}
	}
	if !saw {
		t.Fatalf("facebook missing: %+v profiles=%+v", got[0], got[0].Profiles)
	}
}

func TestAttachMissingSocialsFromOSMTags(t *testing.T) {
	overpass := `{"elements":[{"type":"node","id":99,"tags":{"name":"Licht Kraus","contact:facebook":"LichtKraus","contact:instagram":"lichtkraus"}}]}`
	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(overpass)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
		OverpassURL: "http://overpass.test/api",
	}
	hits := []Hit{{
		Name:        "Licht Kraus",
		Platform:    PlatformWebsite,
		HomepageURL: "https://www.openstreetmap.org/node/99",
		Extra:       map[string]string{"src": "osm", "ext_id": "osm:node:99"},
	}}
	got := c.attachMissingSocials(context.Background(), hits, map[string]bool{
		PlatformFacebook:  true,
		PlatformInstagram: true,
	})
	if !hitHasSocial(got[0]) {
		t.Fatalf("osm tags not attached: %+v", got[0])
	}
}

func TestAttachMissingSocialsSkipsWhenDisabled(t *testing.T) {
	c := &Client{DisablePublic: true}
	hits := []Hit{{Name: "AC Wholesale Electric", Platform: PlatformWebsite, HomepageURL: "https://www.openstreetmap.org/node/1"}}
	got := c.attachMissingSocials(context.Background(), hits, nil)
	if hitHasSocial(got[0]) {
		t.Fatal("disabled public must not attach")
	}
}
