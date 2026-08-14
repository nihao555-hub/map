package engine

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
)

var hrefAbsRe = regexp.MustCompile(`https?://[^\s"'<>]+`)

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
	g.SetLimit(6)

	for _, q := range queries {
		q := q
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}

			batch, src, err := c.searchOneIndex(gctx, q)
			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				warnings = append(warnings, err.Error())
				return nil
			}

			if src != "" {
				sources = append(sources, src)
			}

			for _, h := range batch {
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

func publicSearchQueries(keyword string, wanted map[string]bool) []string {
	specs := []struct {
		platform string
		query    string
	}{
		{PlatformFacebook, "site:facebook.com " + keyword},
		{PlatformLinkedIn, "site:linkedin.com " + keyword},
		{PlatformInstagram, "site:instagram.com " + keyword},
		{PlatformYouTube, "site:youtube.com " + keyword},
		{PlatformTikTok, "site:tiktok.com " + keyword},
		{PlatformX, "site:x.com " + keyword},
		{PlatformPinterest, "site:pinterest.com " + keyword},
		{PlatformThreads, "site:threads.net " + keyword},
		{PlatformDouyin, "site:douyin.com/user " + keyword},
		{PlatformXiaohongshu, "site:xiaohongshu.com/user/profile " + keyword},
		{PlatformKuaishou, "site:kuaishou.com/profile " + keyword},
		{PlatformWeibo, "site:weibo.com/u " + keyword},
		{PlatformBilibili, "site:space.bilibili.com " + keyword},
		{PlatformTelegram, "site:t.me " + keyword},
		{PlatformReddit, "site:reddit.com/user " + keyword},
		{PlatformTwitch, "site:twitch.tv " + keyword},
	}

	out := make([]string, 0, len(specs))
	for _, s := range specs {
		if wanted[s.platform] {
			out = append(out, s.query)
		}
	}

	return out
}

func (c *Client) searchOneIndex(ctx context.Context, query string) ([]Hit, string, error) {
	type attempt struct {
		name string
		fn   func(context.Context, string) ([]byte, error)
	}

	attempts := []attempt{
		{"duckduckgo", c.fetchDuckDuckGo},
		{"brave", c.fetchBrave},
		{"bing", c.fetchBing},
	}

	var lastErr error
	for _, a := range attempts {
		raw, err := a.fn(ctx, query)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", a.name, err)
			continue
		}

		if looksLikeChallenge(raw) {
			lastErr = fmt.Errorf("%s: challenge page", a.name)
			continue
		}

		hits := extractProfilesFromHTML(raw, a.name)
		if len(hits) == 0 {
			lastErr = fmt.Errorf("%s: no profiles", a.name)
			continue
		}

		return hits, a.name, nil
	}

	if lastErr != nil {
		return nil, "", lastErr
	}

	return nil, "", fmt.Errorf("public search empty")
}

func (c *Client) fetchDuckDuckGo(ctx context.Context, query string) ([]byte, error) {
	form := "q=" + url.QueryEscape(query) + "&kl=wt-wt"
	raw, err := c.postFormHTML(ctx, "https://html.duckduckgo.com/html/", form)
	if err == nil && !looksLikeChallenge(raw) && len(raw) > 500 {
		return raw, nil
	}

	return c.getHTML(ctx, "https://lite.duckduckgo.com/lite/?q="+url.QueryEscape(query))
}

func (c *Client) fetchBrave(ctx context.Context, query string) ([]byte, error) {
	return c.getHTML(ctx, "https://search.brave.com/search?q="+url.QueryEscape(query))
}

func (c *Client) fetchBing(ctx context.Context, query string) ([]byte, error) {
	return c.getHTML(ctx, "https://www.bing.com/search?q="+url.QueryEscape(query)+"&setlang=en")
}

func (c *Client) getHTML(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := newBrowserRequest(ctx, "GET", rawURL, "")
	if err != nil {
		return nil, err
	}

	return c.do(req)
}

func (c *Client) postFormHTML(ctx context.Context, rawURL, body string) ([]byte, error) {
	req, err := newBrowserRequest(ctx, "POST", rawURL, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
		doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			title := strings.TrimSpace(s.Text())
			snippet := strings.TrimSpace(s.Parent().Text())
			if len(snippet) > 240 {
				snippet = snippet[:240]
			}

			add(ParseSocialURL(href, title, snippet))
		})
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

	for _, m := range xiaohongshuRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.xiaohongshu.com/user/profile/"+m[1], m[1], ""))
		}
	}

	for _, m := range kuaishouRe.FindAllStringSubmatch(blob, 20) {
		if len(m) == 2 {
			add(ParseSocialURL("https://www.kuaishou.com/profile/"+m[1], m[1], ""))
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
	return strings.Contains(s, "anomaly-modal") ||
		strings.Contains(s, "select all squares containing a duck") ||
		(strings.Contains(s, "unfortunately, bots use duckduckgo") && strings.Contains(s, "challenge"))
}
