package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/sync/errgroup"
)

var (
	hrefAbsRe      = regexp.MustCompile(`https?://[^\s"'<>]+`)
	bingRedirectRe = regexp.MustCompile(`[?&](?:amp;)?u=a1([A-Za-z0-9_\-]+={0,2})`)
	namedIndexMu   sync.Mutex
	lastNamedIndex map[string]time.Time
	indexRotation  atomic.Uint64
)

const (
	indexExtraPages    = 3
	enoughHitsPerQuery = 40
	blobExtractCap     = 200
	publicSearchLimit  = 8
)

// decodeBingRedirect resolves a bing.com/ck/a redirect link to its real target
// URL (the u=a1<base64url> parameter Bing wraps every organic result in).
func decodeBingRedirect(href string) string {
	if !strings.Contains(href, "u=a1") {
		return href
	}
	m := bingRedirectRe.FindStringSubmatch(href)
	if len(m) != 2 {
		return href
	}
	if target := decodeBingTarget(m[1]); target != "" {
		return target
	}
	return href
}

func decodeBingTarget(enc string) string {
	dec, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(enc, "="))
	if err != nil {
		return ""
	}
	target := string(dec)
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target
	}
	return ""
}

// decodeBingRedirectsBlob extracts every redirect target embedded in a Bing
// result page so profile regexes can see the real URLs.
func decodeBingRedirectsBlob(html string) []string {
	if !strings.Contains(html, "u=a1") {
		return nil
	}
	var out []string
	for _, m := range bingRedirectRe.FindAllStringSubmatch(html, blobExtractCap*2) {
		if len(m) != 2 {
			continue
		}
		if target := decodeBingTarget(m[1]); target != "" {
			out = append(out, target)
		}
	}
	return out
}

func isRateLimitedErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "429") || strings.Contains(s, "rate limited") ||
		strings.Contains(s, "403") || strings.Contains(s, "challenge")
}

func indexGap(name string) time.Duration {
	switch name {
	case "brave":
		return 800 * time.Millisecond
	case "duckduckgo":
		return 500 * time.Millisecond
	default:
		return 350 * time.Millisecond
	}
}

func waitPublicIndex(ctx context.Context) error {
	return waitNamedIndex(ctx, "bing")
}

func waitNamedIndex(ctx context.Context, name string) error {
	if testing.Testing() {
		return nil
	}
	if name == "" {
		name = "bing"
	}

	gap := indexGap(name)

	namedIndexMu.Lock()
	wait := time.Duration(0)
	if last, ok := lastNamedIndex[name]; ok {
		wait = gap - time.Since(last)
	}
	namedIndexMu.Unlock()
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}

	namedIndexMu.Lock()
	if lastNamedIndex == nil {
		lastNamedIndex = map[string]time.Time{}
	}
	lastNamedIndex[name] = time.Now()
	namedIndexMu.Unlock()
	return nil
}

func (c *Client) searchPublicProfiles(ctx context.Context, keyword, country, role string, wanted map[string]bool, limit int) ([]Hit, []string, []string, []string) {
	if c != nil && c.DisablePublic {
		return nil, nil, nil, nil
	}

	terms := ExpandSearchTerms(ctx, c, keyword, country, role)
	queries := publicSearchQueriesTerms(keyword, terms, wanted, country, role)

	var (
		mu       sync.Mutex
		hits     []Hit
		warnings []string
		sources  []string
	)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(publicSearchLimit)

	for _, q := range queries {
		q := q
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}

			batch, src, err := c.searchOneIndex(gctx, q.query, q.platform)
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
				// Stamp the finding query for keyword matching only.
				// Skip ID-only cards (regex leftovers) so sidebar noise
				// cannot ride in on the product query.
				if looksLikeProfileName(h.Name) {
					if h.Extra == nil {
						h.Extra = map[string]string{}
					}
					h.Extra["q"] = q.query
				}
				hits = append(hits, h)
			}

			return nil
		})
	}

	_ = g.Wait()

	return hits, uniqueStrings(warnings), uniqueStrings(sources), terms
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
	PlatformDouyin, PlatformTikTok, PlatformFacebook, PlatformLinkedIn,
	PlatformInstagram, PlatformYouTube, PlatformXiaohongshu,
	PlatformKuaishou, PlatformWeibo, PlatformBilibili,
	PlatformX, PlatformPinterest, PlatformThreads, PlatformTelegram, PlatformReddit, PlatformTwitch,
}

const (
	maxPublicQueries   = 48
	publicQueryTermCap = 4
)

var platformProfileSite = map[string]string{
	PlatformFacebook:    "facebook.com",
	PlatformLinkedIn:    "linkedin.com",
	PlatformInstagram:   "instagram.com",
	PlatformYouTube:     "youtube.com/@",
	PlatformTikTok:      "tiktok.com/@",
	PlatformX:           "x.com",
	PlatformPinterest:   "pinterest.com",
	PlatformThreads:     "threads.net/@",
	PlatformDouyin:      "douyin.com/user",
	PlatformXiaohongshu: "xiaohongshu.com/user",
	PlatformKuaishou:    "kuaishou.com/profile",
	PlatformWeibo:       "weibo.com/u",
	PlatformBilibili:    "space.bilibili.com",
	PlatformTelegram:    "t.me",
	PlatformReddit:      "reddit.com/user",
	PlatformTwitch:      "twitch.tv",
}

func publicSearchQueries(keyword string, wanted map[string]bool, country, role string) []publicQuery {
	return publicSearchQueriesTerms(keyword, LocalSearchTerms(keyword, country), wanted, country, role)
}

func publicSearchQueriesTerms(keyword string, terms []string, wanted map[string]bool, country, role string) []publicQuery {
	out := make([]publicQuery, 0, len(wanted)*4)
	keyword = strings.TrimSpace(keyword)
	role = NormalizeRole(role)
	lang := LangForCountry(country)
	if lang == "" {
		if hasCJK(keyword) {
			lang = "zh"
		} else {
			lang = "en"
		}
	}
	engGeo := CountryQueryToken(country, false)
	localGeo := CountryQueryToken(country, true)
	terms = clipTerms(uniqueFoldedStrings(append([]string{keyword}, terms...)), publicQueryTermCap)
	if len(terms) == 0 || keyword == "" {
		return out
	}
	intents := append([]string{}, localIntentWords(lang, role)...)
	if role == RoleBuyer && lang != "en" {
		intents = append(intents, "importer")
	}
	if role == RoleSeller && lang != "en" {
		intents = append(intents, "wholesaler")
	}
	intents = uniqueFoldedStrings(intents)

	seen := make(map[string]bool, len(wanted)*4)
	add := func(platform, query string) {
		query = strings.TrimSpace(query)
		if platform == "" || query == "" {
			return
		}
		key := platform + "\t" + query
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, publicQuery{platform: platform, query: query})
	}

	intentExtra := map[string]bool{
		PlatformFacebook:    true,
		PlatformInstagram:   true,
		PlatformLinkedIn:    true,
		PlatformTikTok:      true,
		PlatformDouyin:      true,
		PlatformKuaishou:    true,
		PlatformXiaohongshu: true,
		PlatformWeibo:       true,
		PlatformBilibili:    true,
	}
	overseasMarketPlatforms := map[string]bool{
		PlatformFacebook:  true,
		PlatformInstagram: true,
		PlatformLinkedIn:  true,
		PlatformTikTok:    true,
		PlatformYouTube:   true,
		PlatformX:         true,
	}
	chineseHomePlatforms := map[string]bool{
		PlatformDouyin:      true,
		PlatformXiaohongshu: true,
		PlatformKuaishou:    true,
		PlatformWeibo:       true,
		PlatformBilibili:    true,
	}

	for _, platform := range publicSearchOrder {
		if !wanted[platform] {
			continue
		}
		if LookupCountry(country).Code != "" && LookupCountry(country).Code != "CN" && chineseHomePlatforms[platform] {
			continue
		}
		site := platformProfileSite[platform]
		if site == "" {
			site = platformSearchDomain[platform]
		}
		if site == "" {
			continue
		}

		for i, term := range terms {
			add(platform, "site:"+site+" "+term)
			if i == 0 && hasCJK(term) {
				add(platform, term+" "+PeoplePlatformLabel(platform))
			}
			if i < 2 && intentExtra[platform] && len(intents) > 0 {
				add(platform, "site:"+site+" "+term+" "+intents[0])
			}
			if overseasMarketPlatforms[platform] && i < 3 {
				if engGeo != "" {
					add(platform, "site:"+site+" "+term+" "+engGeo)
				} else if localGeo != "" {
					add(platform, "site:"+site+" "+term+" "+localGeo)
				}
			}
		}
		if platform == PlatformLinkedIn {
			head := terms[0]
			if role == RoleSeller {
				add(platform, "site:linkedin.com/company "+head+" manufacturer")
			} else {
				add(platform, "site:linkedin.com/company "+head+" importer")
				add(platform, "site:linkedin.com/company "+head+" buyer")
			}
			if engGeo != "" {
				add(platform, "site:linkedin.com/company "+head+" importer "+engGeo)
			}
		}
	}

	if len(out) > maxPublicQueries {
		out = out[:maxPublicQueries]
	}
	return out
}

func productSearchAliases(keyword string) []string {
	return glossaryAliases(keyword)
}

func merchantIntentKeywords(keyword, role string) []string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil
	}
	if NormalizeRole(role) == RoleSeller {
		if hasCJK(keyword) {
			return []string{keyword + " 批发", keyword + " 厂家"}
		}

		return []string{keyword + " wholesaler"}
	}
	if hasCJK(keyword) {
		return []string{keyword + " 采购", keyword + " 进口商", keyword + " importer"}
	}

	return []string{keyword + " importer", keyword + " buyer"}
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana) {
			return true
		}
	}
	return false
}

func (c *Client) searchOneIndex(ctx context.Context, query, platform string) ([]Hit, string, error) {
	return c.searchOneIndexExtract(ctx, query, extractProfilesFromHTML, platform)
}

func (c *Client) searchOneIndexExtract(ctx context.Context, query string, extract func([]byte, string) []Hit, platform string) ([]Hit, string, error) {
	return c.searchOneIndexExtractOrder(ctx, query, extract, nil, platform)
}

func (c *Client) searchOneIndexExtractOrder(ctx context.Context, query string, extract func([]byte, string) []Hit, names []string, platform string) ([]Hit, string, error) {
	attempts := indexAttempts(c, names)

	var (
		merged []Hit
		srcs   []string
		used   []string
	)

	for _, a := range attempts {
		if uniqueHitCount(merged) >= enoughHitsPerQuery {
			break
		}
		if err := waitNamedIndex(ctx, a.name); err != nil {
			return merged, strings.Join(uniqueStrings(srcs), "+"), err
		}

		raw, err := a.fn(ctx, query)
		if err != nil {
			if isRateLimitedErr(err) {
				c.markIndexLimited(a.name)
			}
			continue
		}
		if looksLikeChallenge(raw) {
			c.markIndexLimited(a.name)
			continue
		}

		hits := filterHitsPlatform(extract(raw, a.name), platform)
		if len(hits) == 0 {
			continue
		}

		before := uniqueHitCount(merged)
		merged = append(merged, hits...)
		if uniqueHitCount(merged) == before {
			continue
		}
		srcs = append(srcs, a.name)
		used = append(used, a.name)
	}

	for _, name := range used {
		if uniqueHitCount(merged) >= enoughHitsPerQuery {
			break
		}
		emptyStreak := 0
		for page := 1; page <= indexExtraPages; page++ {
			if ctx.Err() != nil || uniqueHitCount(merged) >= enoughHitsPerQuery {
				break
			}
			if err := waitNamedIndex(ctx, name); err != nil {
				break
			}
			raw, err := c.fetchIndexPage(ctx, name, query, page)
			if err != nil || looksLikeChallenge(raw) {
				if looksLikeChallenge(raw) {
					c.markIndexLimited(name)
				}
				break
			}
			hits := filterHitsPlatform(extract(raw, name), platform)
			if len(hits) == 0 {
				emptyStreak++
				if emptyStreak >= 2 {
					break
				}
				continue
			}
			emptyStreak = 0
			before := uniqueHitCount(merged)
			merged = append(merged, hits...)
			if uniqueHitCount(merged) == before {
				emptyStreak++
				if emptyStreak >= 2 {
					break
				}
			}
		}
	}

	return merged, strings.Join(uniqueStrings(srcs), "+"), nil
}

func filterHitsPlatform(hits []Hit, platform string) []Hit {
	if platform == "" {
		return hits
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if h.Platform == platform {
			out = append(out, h)
		}
	}
	return out
}

func uniqueHitCount(hits []Hit) int {
	seen := map[string]struct{}{}
	for _, h := range hits {
		id := h.ID
		if id == "" {
			id = h.Platform + ":" + h.HomepageURL
		}
		seen[id] = struct{}{}
	}
	return len(seen)
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
		// Rotate the starting engine so concurrent queries spread across
		// all live indexes instead of hammering the first one.
		if len(all) > 1 {
			off := int(indexRotation.Add(1)) % len(all)
			rot := make([]indexAttempt, 0, len(all))
			rot = append(rot, all[off:]...)
			rot = append(rot, all[:off]...)
			all = rot
		}
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

func (c *Client) fetchIndexPage(ctx context.Context, name, query string, page int) ([]byte, error) {
	switch name {
	case "duckduckgo":
		return c.fetchDuckDuckGoPage(ctx, query, page)
	case "bing":
		return c.fetchBingPage(ctx, query, page)
	case "brave":
		return c.fetchBravePage(ctx, query, page)
	default:
		return nil, fmt.Errorf("unknown index")
	}
}

func (c *Client) fetchDuckDuckGo(ctx context.Context, query string) ([]byte, error) {
	return c.fetchDuckDuckGoPage(ctx, query, 0)
}

func (c *Client) fetchDuckDuckGoPage(ctx context.Context, query string, page int) ([]byte, error) {
	kl := duckDuckGoKL(ctx, query)
	form := "q=" + url.QueryEscape(query) + "&kl=" + kl
	if page > 0 {
		form += "&s=" + strconv.Itoa(page*10)
	}
	raw, err := c.postFormHTML(ctx, "https://html.duckduckgo.com/html/", form, "https://html.duckduckgo.com/")
	if err == nil && !looksLikeChallenge(raw) && len(raw) > 500 {
		return raw, nil
	}

	lite := "https://lite.duckduckgo.com/lite/?q=" + url.QueryEscape(query) + "&kl=" + kl
	if page > 0 {
		lite += "&s=" + strconv.Itoa(page*10)
	}
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
	return c.fetchBravePage(ctx, query, 0)
}

func (c *Client) fetchBravePage(ctx context.Context, query string, page int) ([]byte, error) {
	rawURL := "https://search.brave.com/search?q=" + url.QueryEscape(query)
	if page > 0 {
		rawURL += "&offset=" + strconv.Itoa(page*10)
	}
	return c.getHTMLReferer(ctx, rawURL, "https://search.brave.com/")
}

func (c *Client) fetchBing(ctx context.Context, query string) ([]byte, error) {
	return c.fetchBingPage(ctx, query, 0)
}

func (c *Client) fetchBingPage(ctx context.Context, query string, page int) ([]byte, error) {
	rawURL := "https://www.bing.com/search?q=" + url.QueryEscape(query)
	lang, cc := bingLocale(ctx, query)
	if lang != "" {
		rawURL += "&setlang=" + lang
	}
	if cc != "" {
		rawURL += "&cc=" + cc
	}
	if page > 0 {
		rawURL += "&first=" + strconv.Itoa(1+page*10)
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
				href = decodeBingRedirect(href)
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
		addAnchors(doc.Find("a[href]"))
		doc.Find("cite").Each(func(_ int, s *goquery.Selection) {
			cite := strings.TrimSpace(s.Text())
			if cite == "" {
				return
			}
			if !strings.HasPrefix(cite, "http://") && !strings.HasPrefix(cite, "https://") {
				cite = "https://" + strings.TrimPrefix(cite, "//")
			}
			add(ParseSocialURL(cite, strings.TrimSpace(s.Parent().Text()), ""))
		})
	}

	blob := html + "\n" + decoded
	if targets := decodeBingRedirectsBlob(html); len(targets) > 0 {
		blob += "\n" + strings.Join(targets, "\n")
		for _, target := range targets {
			add(ParseSocialURL(target, "", ""))
		}
	}
	addBlob := func(re *regexp.Regexp, home func(string) string) {
		for _, m := range re.FindAllStringSubmatch(blob, blobExtractCap) {
			if len(m) >= 2 {
				add(ParseSocialURL(home(m[1]), m[1], ""))
			}
		}
	}
	addBlob(tiktokHandleRe, func(id string) string { return "https://www.tiktok.com/@" + id })
	addBlob(instagramRe, func(id string) string { return "https://www.instagram.com/" + id })
	addBlob(youtubeAtRe, func(id string) string { return "https://www.youtube.com/@" + id })
	addBlob(youtubeChanRe, func(id string) string { return "https://www.youtube.com/channel/" + id })
	addBlob(facebookUserRe, func(id string) string { return "https://www.facebook.com/" + id })
	addBlob(linkedinInRe, func(id string) string { return "https://www.linkedin.com/in/" + id })
	addBlob(linkedinCoRe, func(id string) string { return "https://www.linkedin.com/company/" + id })
	addBlob(xHandleRe, func(id string) string { return "https://x.com/" + id })
	addBlob(threadsRe, func(id string) string { return "https://www.threads.net/@" + id })
	addBlob(douyinUserRe, func(id string) string { return "https://www.douyin.com/user/" + id })
	addBlob(xiaohongshuRe, func(id string) string { return "https://www.xiaohongshu.com/user/profile/" + id })
	addBlob(kuaishouRe, func(id string) string { return "https://www.kuaishou.com/profile/" + id })
	addBlob(weiboUIDRe, func(id string) string { return "https://weibo.com/u/" + id })
	addBlob(bilibiliRe, func(id string) string { return "https://space.bilibili.com/" + id })
	addBlob(telegramRe, func(id string) string { return "https://t.me/" + id })
	addBlob(redditUserRe, func(id string) string { return "https://www.reddit.com/user/" + id })
	addBlob(twitchRe, func(id string) string { return "https://www.twitch.tv/" + id })
	for _, m := range facebookPeopleRe.FindAllStringSubmatch(blob, blobExtractCap) {
		if len(m) == 3 {
			add(ParseSocialURL("https://www.facebook.com/people/"+m[1]+"/"+m[2], m[1], ""))
		}
	}

	for _, href := range hrefAbsRe.FindAllString(decoded, 400) {
		add(ParseSocialURL(href, "", ""))
	}

	out := make([]Hit, 0, len(seen))
	for _, h := range seen {
		out = append(out, h)
	}

	return out
}

func duckDuckGoKL(ctx context.Context, query string) string {
	if hasCJK(query) {
		return "cn-zh"
	}
	if r := searchCountry(ctx); r.DDGKL != "" {
		return r.DDGKL
	}

	return "wt-wt"
}

func bingLocale(ctx context.Context, query string) (lang, cc string) {
	if hasCJK(query) {
		return "zh-Hans", "CN"
	}
	if r := searchCountry(ctx); r.BingCC != "" {
		return "en", r.BingCC
	}

	return "en", ""
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
