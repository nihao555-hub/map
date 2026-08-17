package engine

import "testing"

func TestParseWaybackTikTokHandles(t *testing.T) {
	t.Parallel()
	raw := []byte(`[
	  ["original"],
	  ["https://www.tiktok.com/@nike"],
	  ["https://www.tiktok.com/@nike/video/7012636934266801413"],
	  ["https://www.tiktok.com/@%22.$author_username.%22"],
	  ["https://www.tiktok.com/@!pageservesmn"],
	  ["https://www.tiktok.com/@tokolistrikjaya"]
	]`)
	got := parseWaybackTikTokHandles(raw)
	if !containsString(got, "nike") || !containsString(got, "tokolistrikjaya") {
		t.Fatalf("%v", got)
	}
	if containsString(got, "pageservesmn") || containsString(got, `".$author_username."`) {
		t.Fatalf("junk kept: %v", got)
	}
}

func TestValidTikTokHandle(t *testing.T) {
	t.Parallel()
	if !validTikTokHandle("boschpowertools") || validTikTokHandle("!") || validTikTokHandle("a") {
		t.Fatal("handle rules")
	}
}
