package engine

import (
	"strings"
	"testing"
)

func TestParseSocialURLTikTok(t *testing.T) {
	hit, ok := ParseSocialURL("https://www.tiktok.com/@nike", "Nike (@nike) Official", "Welcome to Nike")
	if !ok {
		t.Fatal("expected tiktok hit")
	}

	if hit.Platform != PlatformTikTok || hit.Handle != "nike" {
		t.Fatalf("got %+v", hit)
	}

	if !strings.Contains(hit.HomepageURL, "@nike") {
		t.Fatalf("homepage %s", hit.HomepageURL)
	}

	if hit.MessageURL == "" || !strings.Contains(hit.MessageHint, "不会代发") {
		t.Fatalf("message fields %+v", hit)
	}
}

func TestParseSocialURLRejectsTag(t *testing.T) {
	if _, ok := ParseSocialURL("https://www.tiktok.com/tag/shoes", "shoes", ""); ok {
		t.Fatal("tag pages must not become profiles")
	}
}

func TestParseSocialURLFacebookLinkedInX(t *testing.T) {
	fb, ok := ParseSocialURL("https://www.facebook.com/BoschPowerTools", "Bosch Power Tools", "")
	if !ok || fb.Platform != PlatformFacebook || fb.Handle != "BoschPowerTools" {
		t.Fatalf("facebook %+v ok=%v", fb, ok)
	}

	in, ok := ParseSocialURL("https://www.linkedin.com/in/jane-doe", "Jane Doe | LinkedIn", "")
	if !ok || in.Platform != PlatformLinkedIn || in.Handle != "jane-doe" {
		t.Fatalf("linkedin in %+v ok=%v", in, ok)
	}

	co, ok := ParseSocialURL("https://www.linkedin.com/company/bosch", "Bosch", "")
	if !ok || co.Platform != PlatformLinkedIn {
		t.Fatalf("linkedin co %+v ok=%v", co, ok)
	}

	x, ok := ParseSocialURL("https://x.com/dewalt", "DEWALT (@dewalt)", "")
	if !ok || x.Platform != PlatformX || x.Handle != "dewalt" {
		t.Fatalf("x %+v ok=%v", x, ok)
	}

	th, ok := ParseSocialURL("https://www.threads.net/@nike", "Nike", "")
	if !ok || th.Platform != PlatformThreads {
		t.Fatalf("threads %+v ok=%v", th, ok)
	}

	if _, ok := ParseSocialURL("https://www.facebook.com/watch", "watch", ""); ok {
		t.Fatal("facebook watch must be rejected")
	}
}

func TestParseSocialURLMoreNetworks(t *testing.T) {
	xhs, ok := ParseSocialURL("https://www.xiaohongshu.com/user/profile/5c1a2b3c4d5e6f7890ab1234", "某工厂", "")
	if !ok || xhs.Platform != PlatformXiaohongshu {
		t.Fatalf("xiaohongshu %+v ok=%v", xhs, ok)
	}

	ks, ok := ParseSocialURL("https://www.kuaishou.com/profile/3xabcdEF", "快手店主", "")
	if !ok || ks.Platform != PlatformKuaishou {
		t.Fatalf("kuaishou %+v ok=%v", ks, ok)
	}

	wb, ok := ParseSocialURL("https://weibo.com/u/1234567890", "微博店", "")
	if !ok || wb.Platform != PlatformWeibo || wb.Handle != "1234567890" {
		t.Fatalf("weibo %+v ok=%v", wb, ok)
	}

	bl, ok := ParseSocialURL("https://space.bilibili.com/208259", "工具测评", "")
	if !ok || bl.Platform != PlatformBilibili {
		t.Fatalf("bilibili %+v ok=%v", bl, ok)
	}

	tg, ok := ParseSocialURL("https://t.me/bosch_powertools", "Bosch", "")
	if !ok || tg.Platform != PlatformTelegram || tg.Handle != "bosch_powertools" {
		t.Fatalf("telegram %+v ok=%v", tg, ok)
	}

	if _, ok := ParseSocialURL("https://t.me/joinchat/AAAA", "invite", ""); ok {
		t.Fatal("telegram invite must be rejected")
	}

	rd, ok := ParseSocialURL("https://www.reddit.com/user/toolguy", "toolguy", "")
	if !ok || rd.Platform != PlatformReddit {
		t.Fatalf("reddit %+v ok=%v", rd, ok)
	}

	tw, ok := ParseSocialURL("https://www.twitch.tv/ninja", "Ninja", "")
	if !ok || tw.Platform != PlatformTwitch {
		t.Fatalf("twitch %+v ok=%v", tw, ok)
	}

	if _, ok := ParseSocialURL("https://www.twitch.tv/directory", "dir", ""); ok {
		t.Fatal("twitch directory must be rejected")
	}

	for _, h := range []Hit{xhs, ks, wb, bl, tg, rd, tw} {
		if h.HomepageURL == "" || !strings.Contains(h.MessageHint, "不会代发") {
			t.Fatalf("incomplete %+v", h)
		}
	}
}

func TestParseSocialURLDouyin(t *testing.T) {
	hit, ok := ParseSocialURL("https://www.douyin.com/user/MS4wLjABAAAA1234", "某工厂", "主营电动工具")
	if !ok {
		t.Fatal("expected douyin hit")
	}

	if hit.Platform != PlatformDouyin || hit.Handle != "MS4wLjABAAAA1234" {
		t.Fatalf("got %+v", hit)
	}
}

func TestParseSocialURLDouyinVideoNotShortLink(t *testing.T) {
	hit, ok := ParseSocialURL("https://www.douyin.com/video/7642369989815868323", "配电设备图解（基础篇） - 知了电力 - 抖音", "")
	if !ok || hit.Platform != PlatformDouyin {
		t.Fatalf("ok=%v hit=%+v", ok, hit)
	}
	if !strings.Contains(hit.HomepageURL, "/video/7642369989815868323") {
		t.Fatalf("home %s", hit.HomepageURL)
	}
	if strings.Contains(hit.HomepageURL, "v.douyin.com") {
		t.Fatal("www video must not become short link")
	}
	if hit.Name != "知了电力" {
		t.Fatalf("name=%q", hit.Name)
	}
}

func TestParseSocialURLXiaohongshuNote(t *testing.T) {
	hit, ok := ParseSocialURL("https://www.xiaohongshu.com/explore/64f0ab12cd34ef567890abcd", "配电箱现场", "")
	if !ok || hit.Platform != PlatformXiaohongshu || !strings.Contains(hit.HomepageURL, "/explore/") {
		t.Fatalf("ok=%v hit=%+v", ok, hit)
	}
	if _, ok := ParseSocialURL("https://www.xiaohongshu.com/explore?language=zh-CN", "小红书", ""); ok {
		t.Fatal("bare explore must be rejected")
	}
}

func TestUnwrapDuckDuckGoRedirect(t *testing.T) {
	raw := "https://duckduckgo.com/l/?uddg=https%3A%2F%2Fwww.tiktok.com%2F%40allbirds"
	hit, ok := ParseSocialURL(raw, "Allbirds (@allbirds)", "")
	if !ok || hit.Handle != "allbirds" {
		t.Fatalf("ok=%v hit=%+v", ok, hit)
	}
}

func TestLooksLikeHandle(t *testing.T) {
	if !looksLikeHandle("allbirds") || !looksLikeHandle("@Nike.Official") {
		t.Fatal("valid handles rejected")
	}

	if looksLikeHandle("电动工具采购商") || looksLikeHandle("") {
		t.Fatal("non-handle accepted")
	}
}

func TestDisplayNameStripsHandle(t *testing.T) {
	if got := displayName("Nike (@nike) Official TikTok", "nike"); got != "Nike" {
		t.Fatalf("got %q", got)
	}

	if got := displayName("河北喜提电动工具工厂的抖音 - 抖音", "id"); got != "河北喜提电动工具工厂" {
		t.Fatalf("got %q", got)
	}

	if got := displayName("便宜，耐用，自有工厂#电动工具", "cfhardware"); got != "cfhardware" {
		t.Fatalf("got %q", got)
	}

	if got := displayName("Instagram instagram.com › tonyspowertools   Tony's Power Tools", "tonyspowertools"); got != "Tony's Power Tools" {
		t.Fatalf("got %q", got)
	}

	if got := displayName("配电设备图解（基础篇） - 知了电力 - 抖音", "id"); got != "知了电力" {
		t.Fatalf("got %q", got)
	}
}

func TestMergeHitsDedupAndScore(t *testing.T) {
	hits := []Hit{
		tiktokHit("nike", "Nike", "shoes", "web"),
		tiktokHit("nike", "Nike Inc", "keyword nike", "sidecar"),
		tiktokHit("adidas", "Adidas", "other", "web"),
	}
	hits[1].Score = 90

	out := mergeHits(hits, "nike", 10)
	if len(out) != 2 {
		t.Fatalf("len=%d", len(out))
	}

	if out[0].Handle != "nike" {
		t.Fatalf("expected nike first, got %+v", out[0])
	}
}
