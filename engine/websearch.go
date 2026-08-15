package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
)

var (
	hrefAbsRe = regexp.MustCompile(`https?://[^\s"'<>]+`)

	publicIndexMu   sync.Mutex
	lastPublicIndex time.Time
	lastNamedIndex  map[string]time.Time
)

const (
	publicIndexGap = 2800 * time.Millisecond
	braveIndexGap  = 4000 * time.Millisecond
)

func waitPublicIndex(ctx context.Context) error {
	return waitNamedIndex(ctx, "")
}

func waitNamedIndex(ctx context.Context, name string) error {
	if testing.Testing() {
		return nil
	}

	gap := publicIndexGap
	if name == "brave" {
		gap = braveIndexGap
	}

	publicIndexMu.Lock()
	wait := gap - time.Since(lastPublicIndex)
	if name != "" {
		if last, ok := lastNamedIndex[name]; ok {
			if named := gap - time.Since(last); named > wait {
				wait = named
			}
		}
	}
	publicIndexMu.Unlock()
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}

	publicIndexMu.Lock()
	lastPublicIndex = time.Now()
	if name != "" {
		if lastNamedIndex == nil {
			lastNamedIndex = map[string]time.Time{}
		}
		lastNamedIndex[name] = time.Now()
	}
	publicIndexMu.Unlock()
	return nil
}

func (c *Client) searchPublicProfiles(ctx context.Context, keyword string, wanted map[string]bool, limit int) ([]Hit, []string, []string) {
	if c != nil && c.DisablePublic {
		return nil, nil, nil
	}

	queries := publicSearchQueries(keyword, wanted)

	var (
		mu       sync.Mutex
		hits     []Hit
		warnings []string
		sources  []string
	)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(1)

	for _, q := range queries {
		q := q
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}

			batch, src, err := c.searchOneIndex(gctx, q.query)
			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				return nil
			}

			if src != "" {
				sources = append(sources, src)
			}

			for _, h := range batch {
				if q.platform != "" && h.Platform != q.platform {
					continue
				}
				if len(wanted) > 0 && !wanted[h.Platform] {
					continue
				}

				hits = append(hits, h)
			}

			return nil
		})
	}

	_ = g.Wait()

	return hits, uniqueStrings(warnings), uniqueStrings(sources)
}

type publicQuery struct {
	platform string
	query    string
}

var platformSearchDomain = map[string]string{
	PlatformFacebook:    "facebook.com",
	PlatformLinkedIn:    "linkedin.com",
	PlatformInstagram:   "instagram.com",
	PlatformYouTube:     "youtube.com",
	PlatformTikTok:      "tiktok.com",
	PlatformX:           "x.com",
	PlatformPinterest:   "pinterest.com",
	PlatformThreads:     "threads.net",
	PlatformDouyin:      "douyin.com",
	PlatformXiaohongshu: "xiaohongshu.com",
	PlatformKuaishou:    "kuaishou.com",
	PlatformWeibo:       "weibo.com",
	PlatformBilibili:    "bilibili.com",
	PlatformTelegram:    "t.me",
	PlatformReddit:      "reddit.com",
	PlatformTwitch:      "twitch.tv",
}

// publicSearchOrder prefers Chinese networks first so a CJK keyword can
// return Douyin/Xiaohongshu hits before site: queries trip index challenges.
var publicSearchOrder = []string{
	PlatformDouyin, PlatformXiaohongshu, PlatformKuaishou, PlatformWeibo, PlatformBilibili,
	PlatformTikTok, PlatformFacebook, PlatformInstagram, PlatformYouTube, PlatformLinkedIn,
	PlatformX, PlatformPinterest, PlatformThreads, PlatformTelegram, PlatformReddit, PlatformTwitch,
}

func publicSearchQueries(keyword string, wanted map[string]bool) []publicQuery {
	out := make([]publicQuery, 0, len(wanted))
	cjk := hasCJK(keyword)
	for _, platform := range publicSearchOrder {
		if !wanted[platform] {
			continue
		}
		domain := platformSearchDomain[platform]
		if domain == "" {
			continue
		}

		query := "site:" + domain + " " + keyword
		if cjk {
			// Natural queries survive HTML indexes better than site:path filters.
			query = keyword + " " + PeoplePlatformLabel(platform)
		}
		out = append(out, publicQuery{platform: platform, query: query})
	}

	return out
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana) {
			return true
		}
	}
	return false
}

func (c *Client) searchOneIndex(ctx context.Context, query string) ([]Hit, string, error) {
	return c.searchOneIndexExtract(ctx, query, extractProfilesFromHTML)
}

func (c *Client) searchOneIndexExtract(ctx context.Context, query string, extract func([]byte, string) []Hit) ([]Hit, string, error) {
	return c.searchOneIndexExtractOrder(ctx, query, extract, nil)
}

func (c *Client) searchOneIndexExtractOrder(ctx context.Context, query string, extract func([]byte, string) []Hit, names []string) ([]Hit, string, error) {
	attempts := indexAttempts(c, names)

	for _, a := range attempts {
		if err := waitNamedIndex(ctx, a.name); err != nil {
			return nil, "", err
		}

		raw, err := a.fn(ctx, query)
		if err != nil {
			continue
		}
		if looksLikeChallenge(raw) {
			c.markIndexLimited(a.name)
			continue
		}

		hits := extract(raw, a.name)
		if len(hits) == 0 {
			continue
		}

		return hits, a.name, nil
	}

	return nil, "", nil
}

type indexAttempt struct {
	name string
	fn   func(context.Context, string) ([]byte, error)
}

func indexAttempts(c *Client, names []string) []indexAttempt {
	all := []indexAttempt{
		{"duckduckgo", c.fetchDuckDuckGo},
		{"bing", c.fetchBing},
		{"brave", c.fetchBrave},
	}
	filtered := make([]indexAttempt, 0, len(all))
	for _, a := range all {
		if a.name == "brave" && c.braveSkipped() {
			continue
		}
		if a.name == "duckduckgo" && c.ddgSkipped() {
			continue
		}
		if a.name == "bing" && c.bingSkipped() {
			continue
		}
		filtered = append(filtered, a)
	}
	all = filtered
	if len(names) == 0 {
		return all
	}

	byName := map[string]indexAttempt{}
	for _, a := range all {
		byName[a.name] = a
	}

	out := make([]indexAttempt, 0, len(names))
	for _, n := range names {
		if a, ok := byName[n]; ok {
			out = append(out, a)
		}
	}

	return out
}

func (c *Client) fetchIndexHTML(ctx context.Context, query string, names []string) ([]byte, string, error) {
	for _, a := range indexAttempts(c, names) {
		if err := waitNamedIndex(ctx, a.name); err != nil {
			return nil, "", err
		}

		raw, err := a.fn(ctx, query)
		if err != nil {
			continue
		}
		if looksLikeChallenge(raw) {
			c.markIndexLimited(a.name)
			continue
		}
		if len(raw) < 400 {
			continue
		}

		return raw, a.name, nil
	}

	return nil, "", nil
}

func (c *Client) fetchDuckDuckGo(ctx context.Context, query string) ([]byte, error) {
	kl := "wt-wt"
	if hasCJK(query) {
		kl = "cn-zh"
	}
	form := "q=" + url.QueryEscape(query) + "&kl=" + kl
	raw, err := c.postFormHTML(ctx, "https://html.duckduckgo.com/html/", form, "https://html.duckduckgo.com/")
	if err == nil && !looksLikeChallenge(raw) && len(raw) > 500 {
		return raw, nil
	}

	lite := "https://lite.duckduckgo.com/lite/?q=" + url.QueryEscape(query) + "&kl=" + kl
	raw2, err2 := c.getHTMLReferer(ctx, lite, "https://lite.duckduckgo.com/")
	if err2 == nil && !looksLikeChallenge(raw2) && len(raw2) > 400 {
		return raw2, nil
	}
	if looksLikeChallenge(raw) || looksLikeChallenge(raw2) {
		c.markDDGLimited()
	}
	if err2 != nil {
		return nil, err2
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("challenge page")
}

func (c *Client) fetchBrave(ctx context.Context, query string) ([]byte, error) {
	return c.getHTMLReferer(ctx, "https://search.brave.com/search?q="+url.QueryEscape(query), "https://search.brave.com/")
}

func (c *Client) fetchBing(ctx context.Context, query string) ([]byte, error) {
	rawURL := "https://www.bing.com/search?q=" + url.QueryEscape(query)
	if hasCJK(query) {
		rawURL += "&setlang=zh-Hans&cc=CN"
	} else {
		rawURL += "&setlang=en"
	}
	return c.getHTMLReferer(ctx, rawURL, "https://www.bing.com/")
}

func (c *Client) getHTML(ctx context.Context, rawURL string) ([]byte, error) {
	return c.getHTMLReferer(ctx, rawURL, "")
}

func (c *Client) getHTMLReferer(ctx context.Context, rawURL, referer string) ([]byte, error) {
	return c.doHTML(ctx, func() (*http.Request, error) {
		req, err := newBrowserRequest(ctx, "GET", rawURL, "")
		if err != nil {
			return nil, err
		}
		if referer != "" {
			req.Header.Set("Referer", referer)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		return req, nil
	})
}

func (c *Client) postFormHTML(ctx context.Context, rawURL, body, referer string) ([]byte, error) {
	return c.doHTML(ctx, func() (*http.Request, error) {
		req, err := newBrowserRequest(ctx, "POST", rawURL, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if referer != "" {
			req.Header.Set("Referer", referer)
			req.Header.Set("Origin", strings.TrimRight(referer, "/"))
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		return req, nil
	})
}

func (c *Client) doHTML(ctx context.Context, makeReq func() (*http.Request, error)) ([]byte, error) {
	req, err := makeReq()
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func extractProfilesFromHTML(raw []byte, source string) []Hit {
	html := string(raw)
	decoded, err := url.QueryUnescape(html)
	if err != nil {
		decoded = html
	}

	seen := map[string]Hit{}
	add := func(hit Hit, ok bool) {
		if !ok {
			return
		}

		hit.Source = source
		if prev, exists := seen[hit.ID]; exists {
			if hit.Name != "" && (prev.Name == "" || prev.Name == prev.Handle) {
				seen[hit.ID] = hit
			}

			return
		}

		seen[hit.ID] = hit
	}

	if doc, err := goquery.NewDocumentFromReader(strings.NewReader(html)); err == nil {
		addAnchors := func(sel *goquery.Selection) {
			sel.Each(func(_ int, s *goquery.Selection) {
				href, _ := s.Attr("href")
				title := strings.TrimSpace(s.Text())
				snippet := strings.TrimSpace(s.Parent().Text())
				if len(snippet) > 240 {
					snippet = snippet[:240]
				}
				add(ParseSocialURL(href, title, snippet))
			})
		}

		cards := doc.Find("a.result__a, li.b_algo h2 a, #b_results h2 a")
		addAnchors(cards)
		if len(seen) == 0 {
			addAnchors(doc.Find("a[href]"))
		}
	}

	blob := html + "\n" + decoded
	for _, m := range tiktokHandleRe.FindAllStringSubmatch(blob, 40) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.tiktok.com/@"+m[1], m[1], ""))
		}
	}

	for _, m := range instagramRe.FindAllStringSubmatch(blob, 40) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.instagram.com/"+m[1], m[1], ""))
		}
	}

	for _, m := range youtubeAtRe.FindAllStringSubmatch(blob, 40) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.youtube.com/@"+m[1], m[1], ""))
		}
	}

	for _, m := range facebookUserRe.FindAllStringSubmatch(blob, 40) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.facebook.com/"+m[1], m[1], ""))
		}
	}

	for _, m := range linkedinInRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.linkedin.com/in/"+m[1], m[1], ""))
		}
	}

	for _, m := range linkedinCoRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.linkedin.com/company/"+m[1], m[1], ""))
		}
	}

	for _, m := range xHandleRe.FindAllStringSubmatch(blob, 40) {
		if len(m) == 2 {
			add(ParseSocialURL("https://x.com/"+m[1], m[1], ""))
		}
	}

	for _, m := range threadsRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.threads.net/@"+m[1], m[1], ""))
		}
	}

	for _, m := range douyinUserRe.FindAllStringSubmatch(blob, 40) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.douyin.com/user/"+m[1], m[1], ""))
		}
	}

	for _, m := range douyinVideoRe.FindAllStringSubmatch(blob, 40) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.douyin.com/video/"+m[1], m[1], ""))
		}
	}

	for _, m := range douyinNoteRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.douyin.com/note/"+m[1], m[1], ""))
		}
	}

	for _, m := range douyinCollectionRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.douyin.com/collection/"+m[1], m[1], ""))
		}
	}

	for _, m := range xiaohongshuRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.xiaohongshu.com/user/profile/"+m[1], m[1], ""))
		}
	}

	for _, m := range xiaohongshuNoteRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.xiaohongshu.com/explore/"+m[1], m[1], ""))
		}
	}

	for _, m := range kuaishouRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.kuaishou.com/profile/"+m[1], m[1], ""))
		}
	}

	for _, m := range kuaishouVideoRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.kuaishou.com/short-video/"+m[1], m[1], ""))
		}
	}

	for _, m := range weiboUIDRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://weibo.com/u/"+m[1], m[1], ""))
		}
	}

	for _, m := range bilibiliRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://space.bilibili.com/"+m[1], m[1], ""))
		}
	}

	for _, m := range telegramRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://t.me/"+m[1], m[1], ""))
		}
	}

	for _, m := range redditUserRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.reddit.com/user/"+m[1], m[1], ""))
		}
	}

	for _, m := range twitchRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.twitch.tv/"+m[1], m[1], ""))
		}
	}

	for _, href := range hrefAbsRe.FindAllString(decoded, 80) {
		add(ParseSocialURL(href, "", ""))
	}

	out := make([]Hit, 0, len(seen))
	for _, h := range seen {
		out = append(out, h)
	}

	return out
}

func looksLikeChallenge(raw []byte) bool {
	s := strings.ToLower(string(raw))
	if strings.Contains(s, "anomaly-modal") ||
		strings.Contains(s, "select all squares containing a duck") ||
		strings.Contains(s, "unfortunately, bots use duckduckgo") {
		return true
	}
	if strings.Contains(s, `id="b_captcha"`) || strings.Contains(s, `id='b_captcha'`) ||
		strings.Contains(s, "our systems have detected unusual traffic") {
		return true
	}
	if strings.Contains(s, "sorry, you have been rate limited") ||
		strings.Contains(s, "too many requests made") {
		return true
	}
	return false
}
