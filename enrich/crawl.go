package enrich

import (
	"net/url"
	"sort"
	"strings"
)

// pageIntent scores a candidate follow-up page. Lower priority is fetched
// first, because the crawl is capped: we want the contact and imprint pages
// before we spend a request on a product listing.
type pageIntent struct {
	priority int
	needles  []string
}

// pageIntents ranks the page types that carry background-research value, in
// several languages since the target companies are usually not English-first.
var pageIntents = []pageIntent{
	{1, []string{
		"contact", "contacts", "contact-us", "contactus", "kontakt",
		"contacto", "contatti", "contactez", "iletisim", "contato",
		"联系", "联系我们", "お問い合わせ", "문의",
	}},
	{2, []string{
		"impressum", "imprint", "legal-notice", "legal-information",
		"mentions-legales", "aviso-legal", "note-legali", "chi-siamo-legale",
	}},
	{3, []string{
		"about", "about-us", "aboutus", "company", "who-we-are",
		"our-story", "ueber-uns", "uber-uns", "quienes-somos", "sobre",
		"chi-siamo", "a-propos", "hakkimizda", "关于我们", "公司简介", "企业介绍",
	}},
	{4, []string{
		"team", "our-team", "management", "leadership", "staff", "people",
		"unser-team", "equipo", "equipe", "团队", "管理层",
	}},
	{5, []string{
		"certificate", "certificates", "certification", "certifications",
		"quality", "qualitaet", "zertifikate", "calidad", "认证", "资质",
	}},
	{6, []string{
		"export", "international", "distributors", "dealers", "partners",
		"wholesale", "oem", "markets", "出口", "代理",
	}},
	{7, []string{
		"products", "product", "catalogue", "catalog", "produkte",
		"productos", "prodotti", "产品",
	}},
}

// CommonContactPaths are tried when a homepage exposes no useful internal
// links (single-page apps, JavaScript navigation). They cover the paths that
// the overwhelming majority of company sites use.
var CommonContactPaths = []string{
	"/contact", "/contact-us", "/kontakt", "/about", "/about-us", "/impressum",
}

// SelectFollowUpPages picks up to limit same-site pages worth fetching for
// background research, best first. Fragments, files, tracking parameters and
// off-site links are discarded.
func SelectFollowUpPages(base string, hrefs []string, limit int) []string {
	if limit <= 0 {
		return nil
	}

	baseHost := RegistrableDomain(HostFromURL(base))

	type candidate struct {
		url      string
		priority int
		order    int
	}

	seen := make(map[string]bool, len(hrefs))
	candidates := make([]candidate, 0, len(hrefs))

	baseNormalized := normalizeCrawlURL(base)
	if baseNormalized != "" {
		seen[baseNormalized] = true
	}

	for i, href := range hrefs {
		absolute := resolveURL(base, href)
		if absolute == "" {
			continue
		}

		if baseHost != "" && RegistrableDomain(HostFromURL(absolute)) != baseHost {
			continue
		}

		if hasNonPageExtension(absolute) {
			continue
		}

		normalized := normalizeCrawlURL(absolute)
		if normalized == "" || seen[normalized] {
			continue
		}

		priority, ok := classifyPageIntent(absolute)
		if !ok {
			continue
		}

		seen[normalized] = true
		candidates = append(candidates, candidate{url: normalized, priority: priority, order: i})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}

		return candidates[i].order < candidates[j].order
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.url)
	}

	return out
}

func classifyPageIntent(rawURL string) (int, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0, false
	}

	// Match on the path only: query strings carry session and tracking values
	// that would otherwise trigger false matches.
	haystack := strings.ToLower(parsed.Path)
	if haystack == "" || haystack == "/" {
		return 0, false
	}

	for _, intent := range pageIntents {
		for _, needle := range intent.needles {
			if strings.Contains(haystack, needle) {
				return intent.priority, true
			}
		}
	}

	return 0, false
}

// nonPageExtensions are downloadable assets: fetching them costs a request and
// yields no company facts.
var nonPageExtensions = []string{
	".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".zip",
	".rar", ".7z", ".tar", ".gz", ".dwg", ".png", ".jpg", ".jpeg", ".gif",
	".webp", ".svg", ".ico", ".mp4", ".mp3", ".avi", ".mov", ".css", ".js",
	".xml", ".json", ".rss", ".txt", ".exe", ".dmg", ".apk",
}

func hasNonPageExtension(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true
	}

	lower := strings.ToLower(parsed.Path)

	for _, ext := range nonPageExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}

	return false
}

// normalizeCrawlURL drops fragments, tracking parameters and trailing slashes
// so the same page linked three different ways is fetched once.
func normalizeCrawlURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}

	parsed.Fragment = ""

	query := parsed.Query()
	for key := range query {
		if strings.HasPrefix(strings.ToLower(key), "utm_") {
			query.Del(key)
		}
	}

	parsed.RawQuery = query.Encode()

	if parsed.Path != "/" {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}

	return parsed.String()
}
