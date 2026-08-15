package engine

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	maxHarvestPages  = 8
	maxContactFollow = 3
	pageFetchTimeout = 12 * time.Second
)

// Preview is a homepage / contact card rendered in the discover right pane.
type Preview struct {
	URL         string `json:"url"`
	FinalURL    string `json:"final_url,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	Site        string `json:"site,omitempty"`
	Embeddable  bool   `json:"embeddable"`
	EmbedReason string `json:"embed_reason,omitempty"`
	Contacts    []Hit  `json:"contacts,omitempty"`
	Note        string `json:"note"`
	Platform    string `json:"platform,omitempty"`
}

var skipIndexHost = map[string]struct{}{
	"search.brave.com": {}, "brave.com": {}, "duckduckgo.com": {},
	"html.duckduckgo.com": {}, "lite.duckduckgo.com": {},
	"bing.com": {}, "www.bing.com": {}, "google.com": {}, "www.google.com": {},
	"googleusercontent.com": {}, "gstatic.com": {}, "microsoft.com": {},
}

func (c *Client) harvestPages(ctx context.Context, pages []string, limit int) ([]Hit, []string) {
	if c != nil && c.DisablePublic {
		return nil, nil
	}

	var (
		hits     []Hit
		warnings []string
		followed int
	)

	seen := map[string]struct{}{}
	n := 0
	for _, raw := range uniqueStrings(pages) {
		if ctx.Err() != nil || (limit > 0 && len(hits) >= limit) || n >= maxHarvestPages {
			break
		}
		pageURL := firstHTTPURL(raw)
		if pageURL == "" {
			continue
		}
		if _, ok := seen[pageURL]; ok {
			continue
		}
		seen[pageURL] = struct{}{}
		n++

		doc, err := c.fetchDocument(ctx, pageURL)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}

		batch := extractContactsFromHTML(doc.Body, "page:"+hostOf(doc.FinalURL))
		for i := range batch {
			if batch[i].HomepageURL == "" {
				batch[i].HomepageURL = doc.FinalURL
			}
			batch[i].Source = "page-harvest"
			batch[i].Title = firstNonEmpty(batch[i].Title, ogTitle(doc.Body), hostOf(doc.FinalURL))
		}
		hits = append(hits, batch...)

		if len(batch) == 0 && followed < maxContactFollow && !isSocialHost(hostOf(doc.FinalURL)) {
			if extra := contactPageURL(doc.FinalURL); extra != "" && extra != doc.FinalURL {
				followed++
				more, w := c.harvestPages(ctx, []string{extra}, 2)
				hits = append(hits, more...)
				warnings = append(warnings, w...)
			}
		}
	}

	return hits, uniqueStrings(warnings)
}

// Preview fetches one public page and returns a card for the right-hand workbench.
func (c *Client) Preview(ctx context.Context, rawURL string) (Preview, error) {
	doc, err := c.fetchDocument(ctx, rawURL)
	if err != nil {
		return Preview{}, err
	}

	title, desc, image := ogMeta(doc.Body)
	contacts := extractContactsFromHTML(doc.Body, "preview")
	note := "右侧展示公开主页摘要。私信 / 邮件请在官方页或系统邮箱里手动发送，系统不会代发。"
	if doc.FrameDeny {
		note = "该官方页禁止内嵌。右侧显示主页摘要和草稿，点「在官方页打开」后手动发送。系统不会代发。"
	}

	return Preview{
		URL:         rawURL,
		FinalURL:    doc.FinalURL,
		Title:       firstNonEmpty(title, hostOf(doc.FinalURL)),
		Description: desc,
		Image:       image,
		Site:        hostOf(doc.FinalURL),
		Embeddable:  !doc.FrameDeny,
		EmbedReason: doc.FrameReason,
		Contacts:    contacts,
		Note:        note,
		Platform:    platformFromURL(doc.FinalURL),
	}, nil
}

type fetchedDoc struct {
	FinalURL    string
	Body        []byte
	FrameDeny   bool
	FrameReason string
}

func (c *Client) fetchDocument(ctx context.Context, rawURL string) (*fetchedDoc, error) {
	if err := assertPublicHTTPURL(rawURL); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, pageFetchTimeout)
	defer cancel()

	req, err := newBrowserRequest(ctx, http.MethodGet, rawURL, "")
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch page: %w", err)
	}
	defer resp.Body.Close()

	final := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	if err := assertPublicHTTPURL(final); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read page: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s: status %d", hostOf(final), resp.StatusCode)
	}

	deny, reason := frameAncestorsDenied(resp.Header)
	return &fetchedDoc{
		FinalURL:    final,
		Body:        body,
		FrameDeny:   deny,
		FrameReason: reason,
	}, nil
}

func candidatePagesFromHTML(raw []byte) []string {
	html := string(raw)
	decoded, err := url.QueryUnescape(html)
	if err != nil {
		decoded = html
	}

	seen := map[string]struct{}{}
	var out []string
	add := func(href string) {
		u := firstHTTPURL(unwrapRedirect(href))
		if u == "" || assertPublicHTTPURL(u) != nil {
			return
		}
		host := strings.ToLower(hostOf(u))
		if _, skip := skipIndexHost[host]; skip {
			return
		}
		if strings.Contains(host, "duckduckgo.") || strings.Contains(host, "brave.") {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}

	if doc, err := goquery.NewDocumentFromReader(strings.NewReader(html)); err == nil {
		doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			add(href)
		})
	}
	for _, href := range hrefAbsRe.FindAllString(decoded, 40) {
		add(href)
	}

	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func assertPublicHTTPURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http(s) urls allowed")
	}

	host := strings.ToLower(u.Hostname())
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "metadata.google.internal" {
		return fmt.Errorf("blocked host")
	}

	ip := net.ParseIP(host)
	if ip != nil && !publicIP(ip) {
		return fmt.Errorf("blocked address")
	}

	return nil
}

func publicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	return ip.IsGlobalUnicast()
}

func frameAncestorsDenied(h http.Header) (bool, string) {
	xfo := strings.ToLower(h.Get("X-Frame-Options"))
	if strings.Contains(xfo, "deny") {
		return true, "x-frame-options: deny"
	}
	if strings.Contains(xfo, "sameorigin") {
		return true, "x-frame-options: sameorigin"
	}

	csp := strings.ToLower(h.Get("Content-Security-Policy"))
	if i := strings.Index(csp, "frame-ancestors"); i >= 0 {
		rest := csp[i:]
		if end := strings.Index(rest, ";"); end > 0 {
			rest = rest[:end]
		}
		if strings.Contains(rest, "'none'") || strings.Contains(rest, "'self'") {
			return true, "csp frame-ancestors"
		}
	}

	return false, ""
}

func ogMeta(raw []byte) (title, desc, image string) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw)))
	if err != nil {
		return "", "", ""
	}

	prop := func(name string) string {
		v, _ := doc.Find(`meta[property="` + name + `"]`).Attr("content")
		if v == "" {
			v, _ = doc.Find(`meta[name="` + name + `"]`).Attr("content")
		}
		return strings.TrimSpace(v)
	}

	title = firstNonEmpty(prop("og:title"), strings.TrimSpace(doc.Find("title").First().Text()))
	desc = firstNonEmpty(prop("og:description"), prop("description"))
	image = prop("og:image")
	if image != "" && assertPublicHTTPURL(image) != nil {
		image = ""
	}
	if len([]rune(desc)) > 280 {
		desc = string([]rune(desc)[:280])
	}
	return title, desc, image
}

func ogTitle(raw []byte) string {
	t, _, _ := ogMeta(raw)
	return t
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func isSocialHost(host string) bool {
	h := strings.ToLower(host)
	for _, p := range []string{
		"facebook.com", "linkedin.com", "instagram.com", "tiktok.com", "youtube.com",
		"douyin.com", "xiaohongshu.com", "x.com", "twitter.com", "pinterest.com",
		"threads.net", "kuaishou.com", "weibo.com", "bilibili.com", "t.me",
		"telegram.org", "reddit.com", "twitch.tv",
	} {
		if h == p || strings.HasSuffix(h, "."+p) {
			return true
		}
	}
	return false
}

func contactPageURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	path := strings.ToLower(strings.TrimRight(u.Path, "/"))
	if strings.Contains(path, "contact") || strings.Contains(path, "about") {
		return ""
	}
	u.Path = "/contact"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
