package scrapemateapp

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/gosom/scrapemate"
	"github.com/gosom/scrapemate/adapters/fetchers/nethttp"
)

// muxFetcher routes jobs to the cheapest fetcher that can satisfy them:
//
//   - /maps/search & /maps/place -> browser (search list needs in-page
//     scrolling; place details arrive via runtime RPC that only the browser
//     captures into Meta["json"])
//   - everything else (merchant websites for email extraction) -> plain HTTP,
//     no fallback: a dead/slow site must not stall a browser page for 30s.
//     Email jobs are the long tail of every run — pulling them off the browser
//     cuts wall-clock time and Chromium memory sharply without touching result
//     quality (emails are extracted from static HTML via mailto links/regex).
type muxFetcher struct {
	js    scrapemate.HTTPFetcher
	plain scrapemate.HTTPFetcher
}

var _ scrapemate.HTTPFetcher = (*muxFetcher)(nil)

func newMuxFetcher(js scrapemate.HTTPFetcher, rotator scrapemate.ProxyRotator) scrapemate.HTTPFetcher {
	jar, _ := cookiejar.New(nil)

	// Pre-set a consent cookie for Google so plain HTTP fetches are not
	// redirected to consent.google.com.
	if u, err := url.Parse("https://www.google.com"); err == nil {
		jar.SetCookies(u, []*http.Cookie{
			{Name: "SOCS", Value: "CAI", Path: "/", Domain: ".google.com"},
		})
	}

	netClient := &http.Client{
		Timeout: 15 * time.Second,
		Jar:     jar,
	}

	if rotator != nil {
		netClient.Transport = rotator
	}

	return &muxFetcher{
		js:    js,
		plain: nethttp.New(netClient),
	}
}

func (m *muxFetcher) Close() error {
	err := m.js.Close()
	if cerr := m.plain.Close(); cerr != nil && err == nil {
		err = cerr
	}

	return err
}

func (m *muxFetcher) Fetch(ctx context.Context, job scrapemate.IJob) scrapemate.Response {
	u := job.GetFullURL()

	// Google Maps 搜索列表与详情页：走浏览器（滚动 + 运行时 RPC 数据）
	if strings.Contains(u, "/maps/search") || strings.Contains(u, "/maps/place") {
		return m.js.Fetch(ctx, job)
	}

	// 商户官网（邮箱提取）：纯 HTTP，绝不占用浏览器页面
	return m.plain.Fetch(ctx, job)
}
