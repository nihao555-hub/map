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

func TestParseWaybackDouyinHandles(t *testing.T) {
	t.Parallel()
	raw := []byte(`[
	  ["original"],
	  ["https://www.douyin.com/user/MS4wLjABAAAAFactory"],
	  ["https://www.douyin.com/video/7642369989815868323"],
	  ["https://www.douyin.com/user/%22junk"],
	  ["https://www.douyin.com/user/ab"]
	]`)
	got := parseWaybackDouyinHandles(raw)
	if !containsString(got, "MS4wLjABAAAAFactory") {
		t.Fatalf("%v", got)
	}
	if containsString(got, "ab") || containsString(got, `"junk`) {
		t.Fatalf("junk kept: %v", got)
	}
}

func TestValidDouyinUserID(t *testing.T) {
	t.Parallel()
	if !validDouyinUserID("MS4wLjABAAAAFactory") || validDouyinUserID("ab") || validDouyinUserID("a/b") {
		t.Fatal("douyin id rules")
	}
}

func TestWaybackDouyinShardsSplitMS4w(t *testing.T) {
	t.Parallel()
	got := waybackDouyinShards()
	if containsString(got, "m") || containsString(got, "MS4wLjABAAAAq") {
		t.Fatal("lone m / lowercase shards waste CDX (urlkey is folded)")
	}
	if !containsString(got, "MS4wLjABAAAA6") || !containsString(got, "MS4wLjABAAAA_") {
		t.Fatalf("%v", got)
	}
}
