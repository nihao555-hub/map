package engine

import (
	"strings"
	"testing"
)

func TestOSMTikTokWorldQuery(t *testing.T) {
	t.Parallel()
	q := osmTikTokWorldQuery()
	for _, want := range []string{`nwr["contact:tiktok"]`, `nwr["contact:douyin"]`, "out tags"} {
		if !strings.Contains(q, want) {
			t.Fatalf("missing %s in %s", want, q)
		}
	}
	box := osmTikTokBoxQuery(ingestBox{city: "sea", south: 1, west: 100, north: 2, east: 101})
	if !strings.Contains(box, `node["contact:tiktok"]`) {
		t.Fatal(box)
	}
}

func TestParseOverpassShortVideoMerchants(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"elements":[
		{"type":"node","id":11,"tags":{"contact:tiktok":"tokolistrikjaya"}},
		{"type":"node","id":12,"tags":{"name":"Pabrik Lampu","contact:tiktok":"https://www.tiktok.com/@pabriklampu"}},
		{"type":"node","id":13,"tags":{"name":"No social","contact:facebook":"x"}}
	]}`)
	got := parseOverpassShortVideoMerchants(raw, ingestBox{city: "jakarta", country: "ID"})
	if len(got) != 2 {
		t.Fatalf("got=%+v", got)
	}
	var nameless, named bool
	for _, m := range got {
		if m.Name == "tokolistrikjaya" && m.Source == "osm-tiktok" {
			nameless = true
		}
		if m.Name == "Pabrik Lampu" {
			named = true
		}
	}
	if !nameless || !named {
		t.Fatalf("rows=%+v", got)
	}
}

func TestCommonCrawlMerchants(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"url":"https://www.tiktok.com/@boschpowertools"}
{"url":"https://www.douyin.com/user/MS4wLjABAAAAFactory"}
not-json
{"url":"https://www.tiktok.com/@!junk"}
`)
	seen := map[string]struct{}{}
	got := commonCrawlMerchants(raw, seen)
	if len(got) != 2 || len(seen) != 2 {
		t.Fatalf("got=%+v seen=%v", got, seen)
	}
}
