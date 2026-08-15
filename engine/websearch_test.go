package engine

import (
	"strings"
	"testing"
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
	qs := publicSearchQueries("配电", wanted)
	if len(qs) != 2 {
		t.Fatalf("queries=%+v", qs)
	}
	if qs[0].platform != PlatformDouyin || qs[0].query != "配电 抖音" {
		t.Fatalf("douyin first %+v", qs[0])
	}
	if strings.Contains(qs[0].query, "site:") || strings.Contains(qs[0].query, "/user") {
		t.Fatalf("CJK query still uses site path %+v", qs[0])
	}
	if qs[1].platform != PlatformFacebook || qs[1].query != "配电 Facebook" {
		t.Fatalf("facebook %+v", qs[1])
	}
}

func TestPublicSearchQueriesEnglishUsesSite(t *testing.T) {
	wanted := map[string]bool{PlatformTikTok: true}
	qs := publicSearchQueries("power tools", wanted)
	if len(qs) != 1 || qs[0].query != "site:tiktok.com power tools" {
		t.Fatalf("%+v", qs)
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
		if h.Platform == PlatformDouyin && strings.Contains(h.HomepageURL, "/video/") {
			sawVideo = true
			if h.Name != "知了电力" {
				t.Fatalf("name=%q hit=%+v", h.Name, h)
			}
		}
		if h.Platform == PlatformFacebook {
			t.Fatalf("chrome facebook leaked %+v", h)
		}
	}
	if !sawVideo {
		t.Fatalf("missing video hits=%+v", hits)
	}
}
