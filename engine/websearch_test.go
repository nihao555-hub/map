package engine

import (
	"context"
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
		TikTokURL: "",
		F2URL:     "",
	}

	res, err := c.Search(context.Background(), Query{
		Keyword:   "电动工具",
		Kind:      KindPeople,
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
	qs := publicSearchQueries("配电", wanted, "")
	if len(qs) != 4 {
		t.Fatalf("queries=%+v", qs)
	}
	if qs[0].platform != PlatformDouyin || qs[0].query != "配电 批发 抖音" {
		t.Fatalf("douyin first %+v", qs[0])
	}
	if qs[1].platform != PlatformDouyin || qs[1].query != "site:douyin.com/user 配电 批发" {
		t.Fatalf("douyin site %+v", qs[1])
	}
	if qs[2].platform != PlatformFacebook || qs[2].query != "配电 批发 Facebook" {
		t.Fatalf("facebook %+v", qs[2])
	}
	if qs[3].query != "site:facebook.com 配电" {
		t.Fatalf("facebook site %+v", qs[3])
	}
}

func TestPublicSearchQueriesEnglishUsesSite(t *testing.T) {
	wanted := map[string]bool{PlatformTikTok: true}
	qs := publicSearchQueries("power tools", wanted, "")
	if len(qs) != 1 || qs[0].query != "site:tiktok.com/@ power tools wholesaler" {
		t.Fatalf("%+v", qs)
	}
}

func TestPublicSearchQueriesAppendsCountry(t *testing.T) {
	wanted := map[string]bool{PlatformFacebook: true}
	qs := publicSearchQueries("LED灯", wanted, "MY")
	if len(qs) != 2 {
		t.Fatalf("%+v", qs)
	}
	if !strings.Contains(qs[0].query, "马来西亚") || !strings.Contains(qs[1].query, "马来西亚") {
		t.Fatalf("country missing %+v", qs)
	}
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
	sawPage2, sawPage3, sawPage4 := false, false, false
	for _, f := range firsts {
		if f == "11" {
			sawPage2 = true
		}
		if f == "21" {
			sawPage3 = true
		}
		if f == "31" {
			sawPage4 = true
		}
	}
	if !sawPage2 || !sawPage3 || !sawPage4 {
		t.Fatalf("missing bing pagination firsts=%v", firsts)
	}
}
