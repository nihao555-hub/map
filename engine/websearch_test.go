package engine

import "testing"

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
