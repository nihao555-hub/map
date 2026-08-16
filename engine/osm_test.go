package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShopTagsForKeyword(t *testing.T) {
	if tags := shopTagsForKeyword("LED灯"); len(tags) != 1 || tags[0] != "lighting" {
		t.Fatalf("LED灯 tags=%v", tags)
	}
	if tags := shopTagsForKeyword("furniture"); len(tags) != 1 || tags[0] != "furniture" {
		t.Fatalf("furniture tags=%v", tags)
	}
	if tags := shopTagsForKeyword("鞋子"); len(tags) != 1 || tags[0] != "shoes" {
		t.Fatalf("鞋子 tags=%v", tags)
	}
	if tags := shopTagsForKeyword("unknown widget"); len(tags) != 0 {
		t.Fatalf("unknown tags=%v", tags)
	}
}

func TestParseOverpassShopsKeepsNamedLightingStore(t *testing.T) {
	raw := []byte(`{"elements":[
		{"type":"node","id":1,"tags":{"name":"Licht Kraus","shop":"lighting","website":"https://licht-kraus.example","addr:country":"DE","addr:city":"Berlin"}},
		{"type":"node","id":2,"tags":{"name":"Ledox","shop":"lighting","contact:facebook":"LedoxLights","addr:country":"IT"}},
		{"type":"node","id":3,"tags":{"shop":"lighting"}}
	]}`)
	wanted := map[string]bool{PlatformFacebook: true}
	hits := parseOverpassShops(raw, "LED灯", "", wanted)
	var sawWeb, sawFB bool
	for _, h := range hits {
		if h.Name == "Licht Kraus" && h.Platform == PlatformWebsite && strings.Contains(h.HomepageURL, "licht-kraus") {
			sawWeb = true
		}
		if h.Name == "Ledox" && h.Platform == PlatformFacebook && strings.Contains(h.HomepageURL, "facebook.com/LedoxLights") {
			sawFB = true
		}
		if h.Name == "" {
			t.Fatalf("unnamed leaked %+v", h)
		}
	}
	if !sawWeb || !sawFB {
		t.Fatalf("web=%v fb=%v hits=%+v", sawWeb, sawFB, hits)
	}
}

func TestMergeHitsKeepsOSMCategoryShop(t *testing.T) {
	out := mergeHits([]Hit{{
		ID:          "osm:node:1",
		Platform:    PlatformWebsite,
		Name:        "Licht Kraus",
		HomepageURL: "https://www.openstreetmap.org/node/1",
		Snippet:     "lighting shop · 店铺 · 德国",
		Extra:       map[string]string{"src": "osm", "shop": "lighting", "match": "category"},
	}}, "LED灯", 0, RoleBuyer, "")
	if len(out) != 1 || out[0].Name != "Licht Kraus" {
		t.Fatalf("OSM lighting shop dropped %+v", out)
	}
}

func TestOverpassShopQueryUsesCityBox(t *testing.T) {
	q := overpassShopQuery([]string{"lighting"}, osmShopBoxes[0])
	if !strings.Contains(q, `["shop"="lighting"]`) || !strings.Contains(q, "52.35") {
		t.Fatalf("query=%s", q)
	}
}

func TestSearchOSMShopsFromOverpass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		_ = r.ParseForm()
		if !strings.Contains(r.Form.Get("data"), `["shop"="lighting"]`) {
			t.Errorf("query=%s", r.Form.Get("data"))
		}
		_, _ = w.Write([]byte(`{"elements":[{"type":"node","id":9,"tags":{"name":"Opple Lighting Showroom","shop":"lighting","website":"https://opple.example"}}]}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), OverpassURL: srv.URL, DisablePublic: true}
	hits, err := c.searchOSMShops(context.Background(), "LED灯", "DE", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Name != "Opple Lighting Showroom" || hits[0].Platform != PlatformWebsite {
		t.Fatalf("hits=%+v", hits)
	}
}

func TestParseWikidataCompanies(t *testing.T) {
	raw := []byte(`{"results":{"bindings":[
		{"itemLabel":{"value":"Signify"},"website":{"value":"https://www.signify.com"},"facebook":{"value":"Signify"},"countryLabel":{"value":"Netherlands"}},
		{"itemLabel":{"value":"LED"},"website":{"value":"https://example.com/led"}},
		{"itemLabel":{"value":"LED lighting apparatus"},"website":{"value":"https://patents.example/led"}}
	]}}`)
	hits, err := parseWikidataCompanies(raw, "LED灯", "", map[string]bool{PlatformFacebook: true})
	if err != nil {
		t.Fatal(err)
	}
	var sawWeb, sawFB bool
	for _, h := range hits {
		if h.Name == "LED" || strings.Contains(strings.ToLower(h.Name), "apparatus") {
			t.Fatalf("generic LED leaked %+v", h)
		}
		if h.Name == "Signify" && h.Platform == PlatformWebsite {
			sawWeb = true
		}
		if h.Name == "Signify" && h.Platform == PlatformFacebook {
			sawFB = true
		}
	}
	if !sawWeb || !sawFB {
		t.Fatalf("web=%v fb=%v hits=%+v", sawWeb, sawFB, hits)
	}
}
