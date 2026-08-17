package engine

import (
	"hash/fnv"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Public business directories used as yellow pages. Listing HTML is parsed
// for name / phone / official website; we do not log in or bypass challenges.
var yellowPageDirectories = []struct {
	Host      string
	Countries []string
}{
	{Host: "europages.com", Countries: nil},
	{Host: "yellowpages.co.id", Countries: []string{"ID"}},
	{Host: "yellowpages.com.my", Countries: []string{"MY"}},
	{Host: "yellowpages.com.sg", Countries: []string{"SG"}},
	{Host: "hotfrog.com", Countries: nil},
	{Host: "cylex.net", Countries: nil},
}

var (
	telHrefRe   = regexp.MustCompile(`(?i)^(?:tel|phone):(\+?[\d().\s-]{8,22})$`)
	telInBlobRe = regexp.MustCompile(`(?i)(?:tel|phone):(\+?[\d().\s-]{8,22})`)
	phoneBlobRe = regexp.MustCompile(`(?i)(?:\+|00)(?:[\d().\s-]{8,18}\d)`)
)

var yellowPageTitleJunk = []string{
	" | europages", " - europages", " | yellow pages", " - yellow pages",
	" | hotfrog", " - hotfrog", " | cylex", " - cylex",
	" | yellowpages", " - yellowpages",
}

func isYellowPageHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return false
	}
	for _, d := range yellowPageDirectories {
		if host == d.Host || strings.HasSuffix(host, "."+d.Host) {
			return true
		}
	}
	return strings.Contains(host, "yellowpages.") || strings.Contains(host, "europages.") ||
		strings.Contains(host, "hotfrog.") || strings.Contains(host, "cylex.")
}

func yellowPageQueries(keywords, countries []string) []string {
	if len(keywords) == 0 {
		keywords = append([]string{}, DefaultHarvestKeywords...)
	}
	if len(countries) == 0 {
		countries = []string{"ID", "TH", "MY", "VN", "SG", "PH"}
	}
	seen := map[string]bool{}
	var out []string
	add := func(q string) {
		q = strings.TrimSpace(q)
		if q == "" || seen[q] {
			return
		}
		seen[q] = true
		out = append(out, q)
	}
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		qt := quoteSearchTerm(kw)
		for _, cc := range countries {
			label := strings.TrimSpace(LookupCountry(cc).Query)
			if label == "" {
				label = cc
			}
			for _, dir := range yellowPageDirectories {
				if len(dir.Countries) > 0 && !containsFolded(dir.Countries, cc) {
					continue
				}
				add("site:" + dir.Host + " " + qt)
				if label != "" {
					add("site:" + dir.Host + " " + qt + " " + label)
				}
			}
		}
	}
	return out
}

func containsFolded(got []string, want string) bool {
	want = strings.ToUpper(strings.TrimSpace(want))
	for _, g := range got {
		if strings.EqualFold(strings.TrimSpace(g), want) {
			return true
		}
	}
	return false
}

func extractYellowPageURLs(raw []byte) []string {
	var out []string
	for _, u := range candidatePagesFromHTML(raw) {
		if isYellowPageHost(hostOf(u)) {
			out = append(out, u)
		}
	}
	return out
}

func parseYellowPageListing(pageURL string, body []byte) Merchant {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return Merchant{}
	}
	name := cleanYellowPageName(firstNonEmpty(
		strings.TrimSpace(doc.Find("h1").First().Text()),
		ogTitle(body),
		strings.TrimSpace(doc.Find("title").First().Text()),
	))
	phone := firstPhoneFromHTML(doc, string(body))
	home := officialWebsiteFromListing(pageURL, doc)
	var profiles []Profile
	for _, h := range extractProfilesFromHTML(body, "yellowpages") {
		if !isSocialHomepage(h) || h.Platform == PlatformWebsite {
			continue
		}
		profiles = append(profiles, Profile{
			Platform: h.Platform,
			URL:      h.HomepageURL,
			Handle:   h.Handle,
			Source:   "yellowpages",
		})
	}
	if name == "" {
		if home != "" {
			name = strings.TrimPrefix(hostOf(home), "www.")
		} else {
			return Merchant{}
		}
	}
	ext := yellowPageExtID(pageURL, home)
	for i := range profiles {
		profiles[i].ExtID = ext
	}
	if home != "" {
		profiles = append([]Profile{{
			ExtID:    ext,
			Platform: PlatformWebsite,
			URL:      home,
			Source:   "yellowpages",
		}}, profiles...)
	}
	return Merchant{
		ExtID:    ext,
		Source:   "yellowpages",
		Name:     name,
		Homepage: home,
		Phone:    phone,
		Profiles: profiles,
	}
}

func yellowPageExtID(listingURL, website string) string {
	if host := strings.TrimPrefix(hostOf(website), "www."); host != "" && !isYellowPageHost(host) {
		return "yp:" + host
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(listingURL))))
	return "yp:" + strings.ToLower(hostOf(listingURL)) + ":" + hex64(h.Sum64())
}

func hex64(n uint64) string {
	const digits = "0123456789abcdef"
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n&15]
		n >>= 4
	}
	return string(buf[i:])
}

func cleanYellowPageName(name string) string {
	name = strings.TrimSpace(name)
	low := strings.ToLower(name)
	for _, junk := range yellowPageTitleJunk {
		if i := strings.Index(low, junk); i > 0 {
			name = strings.TrimSpace(name[:i])
			low = strings.ToLower(name)
		}
	}
	return strings.TrimSpace(name)
}

func firstPhoneFromHTML(doc *goquery.Document, blob string) string {
	if doc != nil {
		var found string
		doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
			if found != "" {
				return
			}
			href, _ := s.Attr("href")
			if p := normalizePhone(href); p != "" {
				found = p
			}
		})
		if found != "" {
			return found
		}
	}
	if m := telInBlobRe.FindStringSubmatch(blob); len(m) == 2 {
		if p := normalizePhone(m[1]); p != "" {
			return p
		}
	}
	if m := phoneBlobRe.FindString(blob); m != "" {
		return normalizePhone(m)
	}
	return ""
}

func normalizePhone(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := telHrefRe.FindStringSubmatch(raw); len(m) == 2 {
		raw = m[1]
	}
	var b strings.Builder
	for i, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			continue
		}
		if r == '+' && i == 0 {
			b.WriteRune(r)
		}
	}
	s := b.String()
	n := len(strings.TrimPrefix(s, "+"))
	if n < 8 || n > 16 {
		return ""
	}
	return s
}

func officialWebsiteFromListing(pageURL string, doc *goquery.Document) string {
	if doc == nil {
		return ""
	}
	pageHost := strings.TrimPrefix(hostOf(pageURL), "www.")
	var fallback string
	var found string
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		if found != "" {
			return
		}
		href, _ := s.Attr("href")
		u := firstHTTPURL(unwrapRedirect(href))
		if u == "" || assertPublicHTTPURL(u) != nil {
			return
		}
		host := strings.TrimPrefix(hostOf(u), "www.")
		if host == "" || host == pageHost || isYellowPageHost(host) || isSocialHost(host) || skippedPageHost(host) {
			return
		}
		text := strings.ToLower(strings.TrimSpace(s.Text() + " " + attrOr(s, "title") + " " + attrOr(s, "class")))
		if strings.Contains(text, "website") || strings.Contains(text, "homepage") ||
			strings.Contains(text, "official") || strings.Contains(text, "官网") ||
			strings.Contains(text, "visit") || strings.Contains(text, "kunjungi") {
			found = u
			return
		}
		if fallback == "" {
			fallback = u
		}
	})
	return firstNonEmpty(found, fallback)
}

func attrOr(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return v
}
