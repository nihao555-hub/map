package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const ddgDouyinHTML = `
<html><body>
  <div class="result">
    <a class="result__a" href="https://www.douyin.com/user/MS4wLjABAAAAFactory">河北喜提电动工具工厂的抖音 - 抖音</a>
    <a class="result__snippet">工厂直营电动工具</a>
  </div>
  <div class="result">
    <a class="result__a" href="https://www.tiktok.com/@boschpowertools">Bosch Power Tools (@boschpowertools)</a>
    <a class="result__snippet">Official Bosch tools on TikTok</a>
  </div>
</body></html>
`

func TestExtractProfilesFromHTML(t *testing.T) {
	hits := extractProfilesFromHTML([]byte(ddgDouyinHTML), "duckduckgo")
	if len(hits) < 2 {
		t.Fatalf("hits=%+v", hits)
	}

	var sawTK, sawDY bool
	for _, h := range hits {
		if h.Platform == PlatformTikTok && h.Handle == "boschpowertools" {
			sawTK = true
			if h.HomepageURL != "https://www.tiktok.com/@boschpowertools" {
				t.Fatalf("tk home %s", h.HomepageURL)
			}
		}

		if h.Platform == PlatformDouyin && h.Handle == "MS4wLjABAAAAFactory" {
			sawDY = true
		}
	}

	if !sawTK || !sawDY {
		t.Fatalf("missing profiles tk=%v dy=%v hits=%+v", sawTK, sawDY, hits)
	}
}

func TestLooksLikeChallenge(t *testing.T) {
	if !looksLikeChallenge([]byte(`<div class="anomaly-modal__title">Unfortunately, bots use DuckDuckGo too.`)) {
		t.Fatal("expected challenge")
	}

	if !looksLikeChallenge([]byte(`<div id="b_captcha">verify</div>`)) {
		t.Fatal("expected bing captcha")
	}

	if looksLikeChallenge([]byte(`<a class="result__a" href="https://www.tiktok.com/@nike">Nike</a>`)) {
		t.Fatal("false positive")
	}
}

func TestIndexAttemptsSkipsBraveAfter429(t *testing.T) {
	c := &Client{}
	c.markBraveLimited()
	for _, a := range indexAttempts(c, nil) {
		if a.name == "brave" {
			t.Fatal("brave still attempted after 429")
		}
	}
}

func TestIndexAttemptsSkipsBingAfterLimited(t *testing.T) {
	c := &Client{}
	c.markBingLimited()
	for _, a := range indexAttempts(c, nil) {
		if a.name == "bing" {
			t.Fatal("bing still attempted after cooldown")
		}
	}
}

func TestIndexCooldownRecovers(t *testing.T) {
	c := &Client{}
	c.markBraveLimited()
	if !c.braveSkipped() {
		t.Fatal("expected skip")
	}
	c.braveUntil.Store(time.Now().Add(-time.Second).UnixNano())
	if c.braveSkipped() {
		t.Fatal("cooldown should expire")
	}
}

func TestSearchPeopleFallsOverQuietly(t *testing.T) {
	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "duckduckgo") {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Body:       io.NopCloser(strings.NewReader("rate")),
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(ddgDouyinHTML)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
		TikTokURL:  "",
		F2URL:      "",
		SkipExpand: true,
	}

	res, err := c.Search(context.Background(), Query{
		Keyword:   "电动工具",
		Kind:      KindPeople,
		Role:      RoleSeller,
		Platforms: []string{PlatformDouyin, PlatformTikTok},
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) < 2 {
		t.Fatalf("hits=%+v warnings=%v", res.Hits, res.Warnings)
	}
	for _, w := range res.Warnings {
		low := strings.ToLower(w)
		if strings.Contains(low, "429") || strings.Contains(low, "duckduckgo") || strings.Contains(low, "rate limited") {
			t.Fatalf("leaked warning %q", w)
		}
	}
}

func TestWaitReadyIndexesStopsWhenAllCoolingDown(t *testing.T) {
	c := &Client{}
	c.markDDGLimited()
	c.markBingLimited()
	c.markBraveLimited()
	c.markGoogleLimited()
	if got := c.waitReadyIndexes(context.Background(), nil); len(got) != 0 {
		t.Fatalf("want empty while cooling, got %+v", got)
	}
}

func TestIndexAttemptsSkipsDDGAfterChallenge(t *testing.T) {
	c := &Client{}
	c.markDDGLimited()
	for _, a := range indexAttempts(c, nil) {
		if a.name == "duckduckgo" {
			t.Fatal("duckduckgo still attempted after challenge")
		}
	}
}

func TestPublicSearchQueriesCJKUsesPlatformLabel(t *testing.T) {
	wanted := map[string]bool{PlatformDouyin: true, PlatformFacebook: true}
	qs := publicSearchQueries("配电", wanted, "", RoleBuyer)
	got := queryStrings(qs)
	for _, want := range []string{
		"配电 抖音",
		"site:douyin.com/user 配电",
		"site:douyin.com/user 配电 采购",
		"配电 Facebook",
		"site:facebook.com 配电",
		"site:facebook.com 配电 采购",
	} {
		if !containsString(got, want) {
			t.Fatalf("missing %q in %+v", want, got)
		}
	}
	for _, forbid := range got {
		if strings.Contains(forbid, "批发") {
			t.Fatalf("buyer query leaked seller intent %+v", qs)
		}
	}
}

func TestPublicSearchQueriesSellerKeepsWholesale(t *testing.T) {
	wanted := map[string]bool{PlatformDouyin: true, PlatformFacebook: true}
	qs := publicSearchQueries("配电", wanted, "", RoleSeller)
	got := queryStrings(qs)
	for _, want := range []string{
		"配电 抖音",
		"site:douyin.com/user 配电",
		"site:douyin.com/user 配电 批发",
		"配电 Facebook",
		"site:facebook.com 配电",
		"site:facebook.com 配电 批发",
	} {
		if !containsString(got, want) {
			t.Fatalf("missing %q in %+v", want, got)
		}
	}
}

func TestPublicSearchQueriesEnglishUsesSite(t *testing.T) {
	wanted := map[string]bool{PlatformTikTok: true}
	qs := publicSearchQueries("power tools", wanted, "", RoleBuyer)
	got := queryStrings(qs)
	for _, want := range []string{
		"site:tiktok.com power tools",
		"site:tiktok.com power tools importer",
	} {
		if !containsString(got, want) {
			t.Fatalf("missing %q in %+v", want, got)
		}
	}
}

func TestPublicSearchQueriesAppendsCountry(t *testing.T) {
	wanted := map[string]bool{PlatformFacebook: true, PlatformDouyin: true}
	qs := publicSearchQueries("LED灯", wanted, "MY", RoleBuyer)
	got := queryStrings(qs)
	if !containsString(got, "site:facebook.com LED灯") {
		t.Fatalf("missing facebook volume %+v", got)
	}
	if !containsString(got, "site:facebook.com LED light Malaysia") {
		t.Fatalf("missing english alias %+v", got)
	}
	if !containsString(got, "site:facebook.com LED lighting Malaysia") {
		t.Fatalf("missing facebook geo %+v", got)
	}
	for _, q := range qs {
		if q.platform == PlatformDouyin && strings.Contains(q.query, "马来西亚") {
			t.Fatalf("douyin should not glue foreign market %+v", q)
		}
	}
}

func TestPublicSearchQueriesCJKThailandUsesPowerTools(t *testing.T) {
	wanted := map[string]bool{PlatformFacebook: true, PlatformLinkedIn: true, PlatformDouyin: true}
	qs := publicSearchQueries("电动工具", wanted, "TH", RoleBuyer)
	got := queryStrings(qs)
	for _, want := range []string{
		"site:facebook.com 电动工具",
		"site:facebook.com power tools Thailand",
		"site:facebook.com เครื่องมือไฟฟ้า",
		"site:linkedin.com/company power tools importer Thailand",
	} {
		if !containsString(got, want) {
			t.Fatalf("missing %q in %+v", want, got)
		}
	}
	for _, q := range qs {
		if q.platform == PlatformDouyin {
			t.Fatalf("overseas market should skip douyin %+v", q)
		}
	}
}

func TestDuckDuckGoKLKeepsChineseForCJK(t *testing.T) {
	ctx := WithSearchCountry(context.Background(), "MY")
	if kl := duckDuckGoKL(ctx, "LED灯 采购"); kl != "cn-zh" {
		t.Fatalf("cjk kl=%s", kl)
	}
	if kl := duckDuckGoKL(ctx, "LED light importer"); kl != "my-en" {
		t.Fatalf("en kl=%s", kl)
	}
}

func TestPublicSearchQueriesBuyerAddsSourcing(t *testing.T) {
	wanted := map[string]bool{PlatformFacebook: true, PlatformDouyin: true}
	qs := publicSearchQueries("LED灯", wanted, "", RoleBuyer)
	got := queryStrings(qs)
	for _, want := range []string{
		"site:facebook.com LED灯 求购",
		"site:facebook.com LED light sourcing",
		"site:douyin.com/user LED灯 采购",
	} {
		if !containsString(got, want) {
			t.Fatalf("missing %q in %+v", want, got)
		}
	}
}

func TestPublicSearchQueriesLinkedInBuyer(t *testing.T) {
	wanted := map[string]bool{PlatformLinkedIn: true}
	qs := publicSearchQueries("LED light", wanted, "", RoleBuyer)
	got := queryStrings(qs)
	for _, want := range []string{
		"site:linkedin.com LED light",
		"site:linkedin.com LED light importer",
		"site:linkedin.com/company LED light importer",
		"site:linkedin.com/company LED light buyer",
	} {
		if !containsString(got, want) {
			t.Fatalf("missing %q in %+v", want, got)
		}
	}
}

func queryStrings(qs []publicQuery) []string {
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		out = append(out, q.query)
	}

	return out
}

func containsString(items []string, want string) bool {
	for _, s := range items {
		if s == want {
			return true
		}
	}

	return false
}

func TestHasCJK(t *testing.T) {
	if !hasCJK("配电") || hasCJK("power tools") {
		t.Fatal("cjk detect")
	}
}

func TestExtractProfilesFromBingVideoCards(t *testing.T) {
	html := []byte(`<html><body><ol id="b_results">
<li class="b_algo"><h2><a href="https://www.douyin.com/video/7642369989815868323">配电设备图解（基础篇） - 知了电力 - 抖音</a></h2></li>
<li class="b_algo"><h2><a href="https://www.facebook.com">Facebook</a></h2></li>
</ol></body></html>`)
	hits := extractProfilesFromHTML(html, "bing")
	var sawVideo bool
	for _, h := range hits {
		if strings.Contains(h.HomepageURL, "/video/") || strings.Contains(h.HomepageURL, "/watch") {
			sawVideo = true
		}
		if h.Platform == PlatformFacebook {
			t.Fatalf("chrome facebook leaked %+v", h)
		}
	}
	if sawVideo {
		t.Fatalf("videos must not be extracted hits=%+v", hits)
	}
}

func TestExtractProfilesFromHTMLScansNonCardAnchors(t *testing.T) {
	html := []byte(`<html><body>
<div class="result"><a class="result__a" href="https://www.tiktok.com/@carduser">card</a></div>
<a href="https://www.douyin.com/user/MS4wLjABAAAAextra">extra profile</a>
</body></html>`)
	hits := extractProfilesFromHTML(html, "duckduckgo")
	var sawCard, sawExtra bool
	for _, h := range hits {
		if h.Platform == PlatformTikTok && h.Handle == "carduser" {
			sawCard = true
		}
		if h.Platform == PlatformDouyin && h.Handle == "MS4wLjABAAAAextra" {
			sawExtra = true
		}
	}
	if !sawCard || !sawExtra {
		t.Fatalf("card=%v extra=%v hits=%+v", sawCard, sawExtra, hits)
	}
}

func htmlOK(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", 600))),
		Header:     make(http.Header),
		Request:    req,
	}
}

func TestSearchOneIndexSkipsEngineWithWrongPlatform(t *testing.T) {
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return htmlOK(req), nil
	})}}

	extract := func(_ []byte, source string) []Hit {
		switch source {
		case "duckduckgo":
			return []Hit{{ID: "fb:1", Platform: PlatformFacebook, HomepageURL: "https://www.facebook.com/foo"}}
		case "bing":
			return []Hit{{ID: "dy:1", Platform: PlatformDouyin, HomepageURL: "https://www.douyin.com/user/abc"}}
		default:
			return nil
		}
	}

	hits, src, err := c.searchOneIndexExtractOrder(context.Background(), "配电 抖音", extract, []string{"duckduckgo", "bing"}, PlatformDouyin)
	if err != nil {
		t.Fatal(err)
	}
	if src != "bing" {
		t.Fatalf("src=%s hits=%+v", src, hits)
	}
	if uniqueHitCount(hits) != 1 || hits[0].Platform != PlatformDouyin {
		t.Fatalf("hits=%+v", hits)
	}
}

func TestSearchOneIndexMergesEngines(t *testing.T) {
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return htmlOK(req), nil
	})}}

	extract := func(_ []byte, source string) []Hit {
		switch source {
		case "duckduckgo":
			return []Hit{{ID: "dy:ddg", Platform: PlatformDouyin, HomepageURL: "https://www.douyin.com/user/ddg"}}
		case "bing":
			return []Hit{{ID: "dy:bing", Platform: PlatformDouyin, HomepageURL: "https://www.douyin.com/user/bing"}}
		case "brave":
			return []Hit{{ID: "dy:brave", Platform: PlatformDouyin, HomepageURL: "https://www.douyin.com/user/brave"}}
		default:
			return nil
		}
	}

	hits, src, err := c.searchOneIndexExtractOrder(context.Background(), "配电 抖音", extract, []string{"duckduckgo", "bing", "brave"}, PlatformDouyin)
	if err != nil {
		t.Fatal(err)
	}
	if uniqueHitCount(hits) != 3 {
		t.Fatalf("hits=%+v src=%s", hits, src)
	}
	if !strings.Contains(src, "duckduckgo") || !strings.Contains(src, "bing") || !strings.Contains(src, "brave") {
		t.Fatalf("src=%s", src)
	}
}

func TestSearchOneIndexPaginatesBing(t *testing.T) {
	var firsts []string
	c := &Client{HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		firsts = append(firsts, req.URL.Query().Get("first"))
		return htmlOK(req), nil
	})}}

	n := 0
	extract := func(_ []byte, source string) []Hit {
		if source != "bing" {
			return nil
		}
		n++
		return []Hit{{
			ID:          fmt.Sprintf("dy:%d", n),
			Platform:    PlatformDouyin,
			HomepageURL: fmt.Sprintf("https://www.douyin.com/user/%d", n),
		}}
	}

	hits, _, err := c.searchOneIndexExtractOrder(context.Background(), "配电 抖音", extract, []string{"bing"}, PlatformDouyin)
	if err != nil {
		t.Fatal(err)
	}
	if uniqueHitCount(hits) != 1+indexExtraPages {
		t.Fatalf("hits=%d firsts=%v", uniqueHitCount(hits), firsts)
	}
	sawPage2 := false
	for _, f := range firsts {
		if f == "11" {
			sawPage2 = true
		}
	}
	if !sawPage2 {
		t.Fatalf("missing bing pagination firsts=%v", firsts)
	}
}

func TestDecodeBingRedirect(t *testing.T) {
	target := "https://www.facebook.com/PowerbiltTools"
	enc := base64.RawURLEncoding.EncodeToString([]byte(target))
	href := "https://www.bing.com/ck/a?!&&p=abc&u=a1" + enc + "&ntb=1"
	if got := decodeBingRedirect(href); got != target {
		t.Fatalf("got %s", got)
	}
	if got := decodeBingRedirect("https://www.facebook.com/plain"); got != "https://www.facebook.com/plain" {
		t.Fatalf("plain %s", got)
	}
	rel := "/ck/a?!&&p=abc&u=a1" + enc + "&ntb=1"
	if got := decodeBingRedirect(rel); got != target {
		t.Fatalf("relative %s", got)
	}
}

func TestPublicSearchQueriesCoversLatePlatforms(t *testing.T) {
	wanted := map[string]bool{}
	for _, p := range DefaultPeoplePlatforms {
		wanted[p] = true
	}
	qs := publicSearchQueries("LED灯", wanted, "", RoleBuyer)
	saw := map[string]int{}
	for _, q := range qs {
		saw[q.platform]++
	}
	for _, p := range []string{PlatformReddit, PlatformTwitch, PlatformTelegram, PlatformPinterest, PlatformWeibo} {
		if saw[p] == 0 {
			t.Fatalf("platform %s starved counts=%v n=%d", p, saw, len(qs))
		}
	}
	if !containsString(queryStrings(qs), "site:facebook.com LED灯 店铺") {
		t.Fatalf("missing shop intent %+v", queryStrings(qs))
	}
}

func TestPublicSearchQueriesCapsAtMax(t *testing.T) {
	wanted := map[string]bool{}
	for _, p := range DefaultPeoplePlatforms {
		wanted[p] = true
	}
	qs := publicSearchQueries("LED灯", wanted, "", RoleBuyer)
	if len(qs) > maxPublicQueries {
		t.Fatalf("queries=%d cap=%d", len(qs), maxPublicQueries)
	}
	if len(qs) < 20 {
		t.Fatalf("too few queries %d", len(qs))
	}
}

func TestExtractProfilesFromBingRedirectHTML(t *testing.T) {
	target := "https://www.facebook.com/PowerbiltTools"
	enc := base64.RawURLEncoding.EncodeToString([]byte(target))
	html := `<html><body><li class="b_algo"><h2>
		<a href="https://www.bing.com/ck/a?!&amp;&amp;p=x&amp;u=a1` + enc + `&amp;ntb=1">Powerbilt Tools</a>
		</h2><cite>www.facebook.com/PowerbiltTools</cite></li></body></html>`
	hits := extractProfilesFromHTML([]byte(html), "bing")
	saw := false
	for _, h := range hits {
		if h.Platform == PlatformFacebook && strings.Contains(h.HomepageURL, "facebook.com/PowerbiltTools") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("missing facebook profile %+v", hits)
	}
}
