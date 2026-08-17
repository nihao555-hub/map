package engine

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"
)

const (
	maxExpandSeeds     = 16
	maxExpandFetches   = 32
	maxHandleProbePlat = 6
	expandBudget       = 20 * time.Second
)

// expandMerchantSocials finds more homepages for the same merchant:
// social links on the page / official site (Photon-style intel), then the same
// Latin handle on other supported networks (Sherlock/Maigret idea, but only
// our 16 platforms — not 400-site username blasting).
func (c *Client) expandMerchantSocials(ctx context.Context, seeds []Hit, wanted map[string]bool) []Hit {
	if c == nil || c.DisablePublic || c.SkipExpand || ctx.Err() != nil || len(seeds) == 0 {
		return nil
	}

	n := maxExpandSeeds
	if len(seeds) < n {
		n = len(seeds)
	}
	seeds = pickExpandSeeds(seeds, n)
	n = len(seeds)
	if n == 0 {
		return nil
	}

	expandCtx, cancel := context.WithTimeout(ctx, expandBudget)
	defer cancel()

	var (
		mu      sync.Mutex
		extra   []Hit
		fetches int
	)
	add := func(items []Hit) {
		if len(items) == 0 {
			return
		}
		mu.Lock()
		extra = append(extra, items...)
		mu.Unlock()
	}
	takeFetch := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if expandCtx.Err() != nil || fetches >= maxExpandFetches {
			return false
		}
		fetches++
		return true
	}

	g, gctx := errgroup.WithContext(expandCtx)
	g.SetLimit(3)
	for i := 0; i < n; i++ {
		seed := seeds[i]
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			add(c.expandOneMerchant(gctx, seed, wanted, takeFetch))
			return nil
		})
	}
	_ = g.Wait()
	return extra
}

func (c *Client) expandOneMerchant(ctx context.Context, seed Hit, wanted map[string]bool, takeFetch func() bool) []Hit {
	var out []Hit
	seenPlat := map[string]bool{strings.ToLower(seed.Platform): true}

	addHit := func(h Hit) {
		if h.HomepageURL == "" || !isSocialHomepage(h) {
			return
		}
		if len(wanted) > 0 && !wanted[h.Platform] {
			return
		}
		if seenPlat[h.Platform] && strings.EqualFold(h.HomepageURL, seed.HomepageURL) {
			return
		}
		if h.Name == "" || h.Name == h.Handle || genericSocialLabel(h.Name) {
			h.Name = firstNonEmpty(seed.Name, h.Name)
		}
		if h.Title == "" || genericSocialLabel(h.Title) {
			h.Title = firstNonEmpty(seed.Title, seed.Name, h.Title)
		} else {
			h.Title = firstNonEmpty(h.Title, seed.Title, seed.Name)
		}
		h.Snippet = strings.TrimSpace(strings.Join([]string{seed.Name, seed.Title, seed.Snippet, h.Snippet}, " "))
		h.Score = seed.Score - 8
		if h.Score < 40 {
			h.Score = 40
		}
		if h.Extra == nil {
			h.Extra = map[string]string{}
		}
		h.Extra["via"] = seed.HomepageURL
		out = append(out, h)
		seenPlat[h.Platform] = true
	}

	handle := probeableHandle(seed.Handle)
	if handle == "" {
		handle = probeableHandle(seed.Name)
	}
	// Latin handles: probe sister networks first. Facebook/Instagram public HTML
	// is often a login wall, so page extract is a bonus on leftover budget.
	if handle != "" {
		probed := 0
		for _, raw := range sameHandleURLs(handle) {
			if probed >= maxHandleProbePlat || ctx.Err() != nil {
				break
			}
			h, ok := ParseSocialURL(raw, seed.Name, seed.Snippet)
			if !ok || seenPlat[h.Platform] {
				continue
			}
			if len(wanted) > 0 && !wanted[h.Platform] {
				continue
			}
			if !takeFetch() {
				break
			}
			probed++
			if c.probeProfileExists(ctx, h.HomepageURL) {
				h.Verified = true
				addHit(h)
			}
		}
	}

	if seed.HomepageURL != "" && takeFetch() {
		doc, err := c.fetchDocument(ctx, seed.HomepageURL)
		if err == nil && doc != nil {
			for _, h := range extractProfilesFromHTML(doc.Body, "expand") {
				addHit(h)
			}
			for _, site := range outboundOfficialSites(seed.HomepageURL, doc.Body, 2) {
				if !takeFetch() {
					break
				}
				siteDoc, err := c.fetchDocument(ctx, site)
				if err != nil || siteDoc == nil {
					continue
				}
				for _, h := range extractProfilesFromHTML(siteDoc.Body, "expand-site") {
					addHit(h)
				}
			}
		}
	}

	return out
}

// pickExpandSeeds prefers Latin handles that can be probed on other networks,
// so Douyin sec_uid shops do not consume the whole expand budget.
func pickExpandSeeds(hits []Hit, n int) []Hit {
	if n <= 0 || len(hits) == 0 {
		return nil
	}
	out := make([]Hit, 0, n)
	seen := map[string]struct{}{}
	add := func(h Hit) {
		if len(out) >= n {
			return
		}
		key := strings.ToLower(strings.TrimSpace(h.HomepageURL))
		if key == "" {
			key = h.Platform + ":" + strings.ToLower(h.Handle)
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}
	for _, h := range hits {
		if probeableHandle(h.Handle) != "" || probeableHandle(h.Name) != "" {
			add(h)
		}
	}
	for _, h := range hits {
		add(h)
	}
	return out
}

var genericProbeHandles = map[string]bool{
	"shop": true, "store": true, "official": true, "page": true, "home": true,
	"admin": true, "info": true, "contact": true, "about": true, "help": true,
	"news": true, "blog": true, "video": true, "videos": true, "live": true,
	"facebook": true, "instagram": true, "tiktok": true, "youtube": true,
	"support": true, "service": true, "team": true, "user": true, "profile": true,
	"company": true, "business": true, "market": true, "plus": true,
}

func isRealHomepage(home string) bool {
	home = strings.TrimSpace(home)
	if home == "" {
		return false
	}
	low := strings.ToLower(home)
	if strings.Contains(low, "openstreetmap.org") || strings.Contains(low, "gleif.org") {
		return false
	}
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}

func handleFromHomepage(home string) string {
	if !isRealHomepage(home) {
		return ""
	}
	host := strings.TrimPrefix(hostOf(home), "www.")
	if host == "" || isSocialHost(host) {
		return ""
	}
	label := host
	if i := strings.Index(host, "."); i > 0 {
		label = host[:i]
	}
	compact := strings.NewReplacer("-", "", "_", "").Replace(label)
	if h := probeableHandle(compact); h != "" {
		return h
	}
	return probeableHandle(label)
}

func merchantProbeHandles(row Merchant) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(h string) {
		h = probeableHandle(h)
		if h == "" {
			return
		}
		key := strings.ToLower(h)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}
	// Exact Latin shop name, or the official-site domain label.
	// Do not squeeze "Licht Kraus" into lichtkraus — that invents handles and false-positives.
	add(row.Name)
	add(handleFromHomepage(row.Homepage))
	return out
}

func probeableHandle(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(s, "@"))
	if s == "" || strings.HasPrefix(s, "MS4wLjAB") {
		return ""
	}
	if utf8.RuneCountInString(s) < 3 || utf8.RuneCountInString(s) > 24 {
		return ""
	}
	if hasCJK(s) {
		return ""
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			return ""
		}
	}
	if !looksLikeHandle(s) {
		return ""
	}
	if genericProbeHandles[strings.ToLower(s)] {
		return ""
	}
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if digits == len(s) {
		return ""
	}
	return s
}

// groupExpandedHits keeps a merchant's Facebook/Instagram/TikTok rows together
// after score sort, using the expand via-link, the same Latin handle, or the same name.
func groupExpandedHits(hits []Hit) []Hit {
	if len(hits) < 2 {
		return hits
	}
	used := make([]bool, len(hits))
	out := make([]Hit, 0, len(hits))
	related := func(a, b Hit) bool {
		if a.HomepageURL != "" && b.Extra != nil && strings.EqualFold(strings.TrimSpace(b.Extra["via"]), a.HomepageURL) {
			return true
		}
		if b.HomepageURL != "" && a.Extra != nil && strings.EqualFold(strings.TrimSpace(a.Extra["via"]), b.HomepageURL) {
			return true
		}
		ha := strings.ToLower(probeableHandle(a.Handle))
		hb := strings.ToLower(probeableHandle(b.Handle))
		if ha != "" && ha == hb {
			return true
		}
		na, nb := foldSearchText(a.Name), foldSearchText(b.Name)
		return na != "" && na == nb && utf8.RuneCountInString(strings.TrimSpace(a.Name)) >= 4
	}
	for i := range hits {
		if used[i] {
			continue
		}
		used[i] = true
		out = append(out, hits[i])
		for j := i + 1; j < len(hits); j++ {
			if used[j] || !related(hits[i], hits[j]) {
				continue
			}
			used[j] = true
			out = append(out, hits[j])
		}
	}
	return out
}

// sameHandleURLs is the Sherlock-style sister-profile list we actually keep:
// only platforms ParseSocialURL can verify, and LinkedIn company pages
// (Sherlock's data.json uses linkedin.com/in/, which is a person, not a firm).
// sherlock-project/sherlock (~9万 star, 400+ 站) and soxoj/maigret (3000+ 站)
// need a username. We only call this with a handle from an official site or
// an already-found social homepage — not a GLEIF legal name.
func sameHandleURLs(handle string) []string {
	h := strings.Trim(handle, "/")
	return []string{
		"https://www.instagram.com/" + h + "/",
		"https://www.tiktok.com/@" + h,
		"https://www.youtube.com/@" + h,
		"https://x.com/" + h,
		"https://www.facebook.com/" + h,
		"https://www.linkedin.com/company/" + h,
		"https://www.threads.net/@" + h,
		"https://t.me/" + h,
		"https://www.pinterest.com/" + h + "/",
		"https://www.reddit.com/user/" + h,
		"https://www.twitch.tv/" + h,
	}
}

func outboundOfficialSites(pageURL string, raw []byte, limit int) []string {
	if limit <= 0 {
		return nil
	}
	self := hostOf(pageURL)
	seen := map[string]struct{}{}
	var out []string
	for _, u := range candidatePagesFromHTML(raw) {
		host := hostOf(u)
		if host == "" || host == self || isSocialHost(host) || skippedPageHost(host) {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, u)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (c *Client) probeProfileExists(ctx context.Context, rawURL string) bool {
	if c == nil || rawURL == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	doc, err := c.fetchDocument(ctx, rawURL)
	if err != nil || doc == nil {
		return false
	}
	title, _, _ := ogMeta(doc.Body)
	if profileLooksGone(title, doc.Body) {
		return false
	}
	hit, ok := ParseSocialURL(doc.FinalURL, title, "")
	return ok && isSocialHomepage(hit)
}

func profileLooksGone(title string, body []byte) bool {
	blob := strings.ToLower(title + " " + string(body))
	if len(blob) > 2500 {
		blob = blob[:2500]
	}
	for _, n := range []string{
		"isn't available", "isn’t available", "page not found", "couldn't find this account",
		"couldn’t find this account", "sorry, this page", "content isn't available",
		"user not found", "账号不存在", "该页面不存在", "找不到这个账号",
	} {
		if strings.Contains(blob, n) {
			return true
		}
	}
	return false
}
