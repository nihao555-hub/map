package engine

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	emailRe = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,24}\b`)
	waMeRe  = regexp.MustCompile(`(?i)(?:wa\.me/|api\.whatsapp\.com/send\?[^"'<>]*phone=)(\+?\d{8,15})`)
)

var skipEmailHost = map[string]struct{}{
	"example.com": {}, "sentry.io": {}, "wixpress.com": {}, "cloudflare.com": {},
	"google.com": {}, "gstatic.com": {}, "github.com": {}, "githubusercontent.com": {},
	"facebookmail.com": {}, "company.com": {}, "domain.com": {}, "email.com": {},
	"yourcompany.com": {}, "test.com": {}, "example.org": {}, "example.net": {},
}

func (c *Client) searchMarketing(ctx context.Context, q Query) (Result, error) {
	wanted := wantedPeoplePlatforms(q.Platforms)
	channel := strings.ToLower(strings.TrimSpace(q.Channel))

	items, warns, srcs := c.searchPublicContacts(ctx, q.Keyword, wanted, q.Limit)
	merged := mergeHits(items, q.Keyword, q.Limit)
	if channel == ChannelEmail || channel == ChannelWhatsApp {
		filtered := merged[:0]
		for _, h := range merged {
			if h.Channel == channel {
				filtered = append(filtered, h)
			}
		}
		merged = filtered
	}

	if len(merged) == 0 {
		warns = append(warns, "未找到公开邮箱或 WhatsApp，请换关键词。")
	}

	return Result{
		Hits:     merged,
		Warnings: uniqueStrings(warns),
		Sources:  uniqueStrings(srcs),
		Note:     marketingPolicyNote,
	}, nil
}

func (c *Client) searchPublicContacts(ctx context.Context, keyword string, wanted map[string]bool, limit int) ([]Hit, []string, []string) {
	if c != nil && c.DisablePublic {
		return nil, nil, nil
	}

	queries := marketingSearchQueries(keyword, wanted)
	var (
		hits     []Hit
		warnings []string
		sources  []string
		pages    []string
	)
	order := []string{"duckduckgo", "bing", "brave"}

	for _, q := range queries {
		if ctx.Err() != nil {
			break
		}

		raw, src, err := c.fetchIndexHTML(ctx, q, order)
		if err != nil || len(raw) == 0 {
			continue
		}
		if src != "" {
			sources = append(sources, src)
		}
		hits = append(hits, extractContactsFromHTML(raw, src)...)
		pages = append(pages, candidatePagesFromHTML(raw)...)
	}

	harvested, hw := c.harvestPages(ctx, pages, limit)
	hits = append(hits, harvested...)
	warnings = append(warnings, hw...)
	if len(harvested) > 0 {
		sources = append(sources, "page-harvest")
	}

	return hits, uniqueStrings(warnings), uniqueStrings(sources)
}

func marketingSearchQueries(keyword string, wanted map[string]bool) []string {
	var out []string
	if hasCJK(keyword) {
		out = []string{
			keyword + " 厂家 联系方式",
			keyword + " 官网 邮箱",
			keyword + " 邮箱 OR mailto OR 联系我们",
		}
	} else {
		out = []string{
			keyword + ` email OR contact OR mailto`,
			keyword + ` whatsapp OR wa.me`,
		}
	}

	if hasCJK(keyword) {
		return out
	}

	n := 0
	for _, p := range []string{PlatformLinkedIn, PlatformFacebook, PlatformInstagram, PlatformX} {
		if !wanted[p] {
			continue
		}
		out = append(out, "site:"+p+".com "+keyword+" email")
		n++
		if n >= 1 {
			break
		}
	}

	return out
}

func extractContactsFromHTML(raw []byte, source string) []Hit {
	html := string(raw)
	decoded, err := url.QueryUnescape(html)
	if err != nil {
		decoded = html
	}
	blob := html + "\n" + decoded

	seen := map[string]Hit{}
	add := func(hit Hit) {
		if hit.ID == "" {
			return
		}
		hit.Source = source
		hit.Kind = KindMarketing
		if prev, ok := seen[hit.ID]; ok {
			if hit.Score > prev.Score {
				seen[hit.ID] = hit
			}
			return
		}
		seen[hit.ID] = hit
	}

	pageURL := ""
	pageTitle := ""
	if doc, err := goquery.NewDocumentFromReader(strings.NewReader(html)); err == nil {
		doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			title := strings.TrimSpace(s.Text())
			snippet := strings.TrimSpace(s.Parent().Text())
			if len(snippet) > 240 {
				snippet = snippet[:240]
			}
			for _, hit := range contactsFromText(href+" "+title+" "+snippet, title, href, source) {
				add(hit)
			}
			if pageURL == "" {
				if u := unwrapRedirect(href); strings.HasPrefix(u, "http") {
					pageURL = u
					pageTitle = title
				}
			}
		})
	}

	for _, hit := range contactsFromText(blob, pageTitle, pageURL, source) {
		add(hit)
	}

	out := make([]Hit, 0, len(seen))
	for _, h := range seen {
		out = append(out, h)
	}
	return out
}

func contactsFromText(blob, title, pageURL, source string) []Hit {
	var out []Hit
	for _, m := range emailRe.FindAllString(blob, 40) {
		email := strings.ToLower(strings.TrimSpace(m))
		if !validPublicEmail(email) {
			continue
		}
		home := firstHTTPURL(pageURL)
		plat := platformFromURL(home)
		out = append(out, Hit{
			ID:          "email:" + email,
			Kind:        KindMarketing,
			Platform:    plat,
			Name:        displayName(title, email),
			Handle:      email,
			Title:       title,
			Snippet:     clipSnippet(blob),
			HomepageURL: home,
			MessageURL:  "mailto:" + email,
			MessageHint: "打开系统邮箱撰写开发信。系统不会代发。",
			Contact:     email,
			Channel:     ChannelEmail,
			Source:      source,
			Score:       70,
		})
	}

	for _, m := range waMeRe.FindAllStringSubmatch(blob, 20) {
		if len(m) < 2 {
			continue
		}
		num := strings.TrimPrefix(m[1], "+")
		if len(num) < 8 {
			continue
		}
		home := firstHTTPURL(pageURL)
		wa := "https://wa.me/" + num
		out = append(out, Hit{
			ID:          "whatsapp:" + num,
			Kind:        KindMarketing,
			Platform:    "whatsapp",
			Name:        displayName(title, "+"+num),
			Handle:      "+" + num,
			Title:       title,
			Snippet:     clipSnippet(blob),
			HomepageURL: home,
			MessageURL:  wa,
			MessageHint: "打开 WhatsApp 官方聊天窗口。系统不会代发。",
			Contact:     "+" + num,
			Channel:     ChannelWhatsApp,
			Source:      source,
			Score:       75,
		})
	}

	return out
}

func skippedEmailHost(host string) bool {
	if _, skip := skipEmailHost[host]; skip {
		return true
	}
	for skip := range skipEmailHost {
		if strings.HasSuffix(host, "."+skip) {
			return true
		}
	}
	return false
}

func validPublicEmail(email string) bool {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	host := parts[1]
	if strings.HasSuffix(host, ".png") || strings.HasSuffix(host, ".jpg") || strings.HasSuffix(host, ".js") {
		return false
	}
	if skippedEmailHost(host) {
		return false
	}
	if strings.Contains(parts[0], "noreply") || strings.Contains(parts[0], "no-reply") {
		return false
	}
	return true
}

func firstHTTPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http") {
		return unwrapRedirect(raw)
	}
	return ""
}

func platformFromURL(raw string) string {
	u := strings.ToLower(raw)
	switch {
	case strings.Contains(u, "linkedin.com"):
		return PlatformLinkedIn
	case strings.Contains(u, "facebook.com"):
		return PlatformFacebook
	case strings.Contains(u, "instagram.com"):
		return PlatformInstagram
	case strings.Contains(u, "tiktok.com"):
		return PlatformTikTok
	case strings.Contains(u, "youtube.com"):
		return PlatformYouTube
	case strings.Contains(u, "twitter.com"), strings.Contains(u, "x.com/"):
		return PlatformX
	default:
		return "web"
	}
}

func clipSnippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 240 {
		return s[:240]
	}
	return s
}
