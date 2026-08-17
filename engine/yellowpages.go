package engine

import (
	"encoding/json"
	"hash/fnv"
	"net/url"
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
	{Host: "europages.co.uk", Countries: nil},
	{Host: "yellowpages.co.id", Countries: []string{"ID"}},
	{Host: "yellowpages.com.my", Countries: []string{"MY"}},
	{Host: "yellowpages.com.sg", Countries: []string{"SG"}},
	{Host: "yellowpages.com.ph", Countries: []string{"PH"}},
	{Host: "hotfrog.com", Countries: nil},
	{Host: "cylex.net", Countries: nil},
	{Host: "gelbeseiten.de", Countries: []string{"DE"}},
	{Host: "11880.com", Countries: []string{"DE"}},
}

// yellowPageKeywordEN maps the built-in 外贸品类 to directory search English.
var yellowPageKeywordEN = map[string]string{
	"LED灯": "lighting",
	"家具":   "furniture",
	"电动工具": "tools",
	"服装":   "garment",
	"鞋子":   "shoes",
	"化妆品":  "cosmetics",
	"包装":   "packaging",
	"阀门":   "valve",
	"太阳能":  "solar",
	"汽车配件": "auto parts",
	"塑料制品": "plastic",
	"食品":   "food",
}

var extraYellowPageTerms = []string{
	"wholesale", "manufacturer", "factory", "electronics", "textile", "hardware",
}

var europagesCountrySlug = map[string]string{
	"ID": "indonesia",
	"TH": "thailand",
	"MY": "malaysia",
	"VN": "vietnam",
	"SG": "singapore",
	"PH": "philippines",
	"DE": "germany",
}

var yellowPageJunkHost = []string{
	"wipe.de", "adition.com", "golocal.de", "kennstdueinen.de",
	"werkenntdenbesten.de", "wirfindendeinenjob.de", "cleverb2b.de",
	"postleitzahlen.de", "consentmanager.net", "doubleclick.net",
	"googlesyndication.com", "google-analytics.com", "googletagmanager.com",
	"facebook.net", "cloudfront.net", "googleadservices.com",
}

var publicMailHost = map[string]bool{
	"gmail.com": true, "yahoo.com": true, "hotmail.com": true, "outlook.com": true,
	"icloud.com": true, "qq.com": true, "163.com": true, "126.com": true,
	"11880.com": true, "gelbeseiten.de": true, "europages.com": true,
	"europages.co.uk": true, "exportersindia.com": true, "tradeindia.com": true,
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
		strings.Contains(host, "hotfrog.") || strings.Contains(host, "cylex.") ||
		strings.Contains(host, "gelbeseiten.") || host == "11880.com" || strings.HasSuffix(host, ".11880.com")
}

func yellowPageSearchTerms(keywords []string) []string {
	if len(keywords) == 0 {
		keywords = append([]string{}, DefaultHarvestKeywords...)
	}
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			return
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	for _, kw := range keywords {
		kw = strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		if en := strings.TrimSpace(yellowPageKeywordEN[kw]); en != "" {
			add(en)
		}
		if isMostlyASCII(kw) {
			add(kw)
		}
	}
	for _, extra := range extraYellowPageTerms {
		add(extra)
	}
	return out
}

func isMostlyASCII(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func yellowPageSlug(term string) string {
	term = strings.ToLower(strings.TrimSpace(term))
	var b strings.Builder
	lastDash := false
	for _, r := range term {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func yellowPageQueries(keywords, countries []string) []string {
	terms := yellowPageSearchTerms(keywords)
	if len(countries) == 0 {
		countries = []string{"ID", "TH", "MY", "VN", "SG", "PH", "DE"}
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
	for _, kw := range terms {
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

func yellowPageDirectURLs(keywords, countries []string) []string {
	terms := yellowPageSearchTerms(keywords)
	if len(countries) == 0 {
		countries = []string{"ID", "TH", "MY", "VN", "SG", "PH", "DE"}
	}
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		out = append(out, raw)
	}
	for _, term := range terms {
		slug := yellowPageSlug(term)
		if slug == "" {
			continue
		}
		q := url.QueryEscape(term)
		add("https://www.gelbeseiten.de/suche/" + slug + "/bundesweit")
		add("https://www.gelbeseiten.de/suche/" + slug + "/bundesweit?seite=2")
		add("https://www.11880.com/suche/" + slug + "/bundesweit")
		add("https://www.11880.com/suche/" + slug + "/bundesweit?page=2")
		for _, cc := range countries {
			if ep := europagesCountrySlug[strings.ToUpper(cc)]; ep != "" {
				add("https://www.europages.co.uk/companies/" + ep + "/" + slug + ".html")
			}
		}
		add("https://www.europages.co.uk/companies/" + slug + ".html")
		add("https://www.yellowpages.co.id/search?search_keywords=" + q)
		add("https://www.yellowpages.com.my/search?q=" + q)
		add("https://www.yellowpages.com.sg/search?q=" + q)
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
	return extractYellowPageURLsFrom("", raw)
}

func extractYellowPageURLsFrom(pageURL string, raw []byte) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(rawURL string) {
		u := resolvePageHref(pageURL, rawURL)
		if u == "" || !isYellowPageHost(hostOf(u)) || isYellowPageSearchURL(u) || !isYellowPageListingURL(u) {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	if doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(raw))); err == nil {
		doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			add(href)
		})
	}
	for _, u := range candidatePagesFromHTML(raw) {
		add(u)
	}
	for _, u := range listingURLsFromJSONLD(raw) {
		add(u)
	}
	return out
}

func resolvePageHref(base, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "javascript:") || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "mailto:") {
		return ""
	}
	href = unwrapRedirect(href)
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return firstHTTPURL(href)
	}
	if base == "" {
		return ""
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return b.ResolveReference(ref).String()
}

func isYellowPageSearchURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	p := strings.ToLower(u.Path + "?" + u.RawQuery)
	if strings.Contains(p, "/suche/") || strings.Contains(p, "/search") ||
		strings.Contains(p, "search_keywords") || strings.Contains(p, "search_terms") {
		return true
	}
	if strings.Contains(p, "/companies/") && strings.HasSuffix(strings.ToLower(u.Path), ".html") {
		return true
	}
	return false
}

func isYellowPageListingURL(raw string) bool {
	if !isYellowPageHost(hostOf(raw)) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	p := strings.ToLower(strings.TrimSpace(u.Path))
	if p == "" || p == "/" {
		return false
	}
	if strings.Contains(p, "/gsbiz/") || strings.Contains(p, "/branchenbuch/") {
		return true
	}
	if isYellowPageSearchURL(raw) {
		return false
	}
	for _, junk := range []string{"/legal", "/privacy", "/languages", "/cookie", "/terms", "/login", "/about", "/contact-us"} {
		if strings.Contains(p, junk) {
			return false
		}
	}
	return strings.Count(p, "/") >= 1
}

func listingURLsFromJSONLD(body []byte) []string {
	var out []string
	walkJSONLD(body, func(obj map[string]any) {
		if !jsonLDIsType(obj, "LocalBusiness", "Organization") {
			return
		}
		if s := jsonLDString(obj["url"]); s != "" {
			out = append(out, s)
		}
	})
	return out
}

func parseYellowPageListing(pageURL string, body []byte) Merchant {
	if isYellowPageSearchURL(pageURL) {
		return Merchant{}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return Merchant{}
	}
	ld := firstJSONLDBusiness(body)
	name := cleanYellowPageName(firstNonEmpty(
		strings.TrimSpace(doc.Find("h1").First().Text()),
		ld.Name,
		ogTitle(body),
		strings.TrimSpace(doc.Find("title").First().Text()),
	))
	phone := firstNonEmpty(firstPhoneFromHTML(doc, string(body)), normalizePhone(ld.Phone))
	home := firstNonEmpty(
		officialWebsiteFromListing(pageURL, doc),
		officialWebsiteFromJSONLD(pageURL, ld),
		websiteFromEmail(ld.Email),
	)
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
	if home == "" && phone == "" && len(profiles) == 0 {
		return Merchant{}
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
		City:     ld.City,
		Country:  ld.Country,
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
		if host == "" || host == pageHost || isYellowPageHost(host) || isSocialHost(host) ||
			skippedPageHost(host) || isYellowPageJunkHost(host) {
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

func isYellowPageJunkHost(host string) bool {
	host = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(host), "www."))
	for _, junk := range yellowPageJunkHost {
		if host == junk || strings.HasSuffix(host, "."+junk) {
			return true
		}
	}
	return false
}

func websiteFromEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at < 0 || at+1 >= len(email) {
		return ""
	}
	host := strings.TrimPrefix(email[at+1:], "www.")
	if host == "" || publicMailHost[host] || isYellowPageHost(host) || isSocialHost(host) || isYellowPageJunkHost(host) {
		return ""
	}
	return "https://" + host
}

type jsonLDBusiness struct {
	Name    string
	Phone   string
	Email   string
	URL     string
	City    string
	Country string
}

func firstJSONLDBusiness(body []byte) jsonLDBusiness {
	var got jsonLDBusiness
	walkJSONLD(body, func(obj map[string]any) {
		if got.Name != "" && got.Phone != "" {
			return
		}
		if !jsonLDIsType(obj, "LocalBusiness", "Organization") {
			return
		}
		if got.Name == "" {
			got.Name = jsonLDString(obj["name"])
		}
		if got.Phone == "" {
			got.Phone = jsonLDString(obj["telephone"])
		}
		if got.Email == "" {
			got.Email = jsonLDString(obj["email"])
		}
		if got.URL == "" {
			got.URL = jsonLDString(obj["url"])
		}
		if addr, ok := obj["address"].(map[string]any); ok {
			if got.City == "" {
				got.City = jsonLDString(addr["addressLocality"])
			}
			if got.Country == "" {
				got.Country = jsonLDString(addr["addressCountry"])
			}
		}
	})
	return got
}

func officialWebsiteFromJSONLD(pageURL string, ld jsonLDBusiness) string {
	u := firstHTTPURL(ld.URL)
	if u == "" {
		return websiteFromEmail(ld.Email)
	}
	host := strings.TrimPrefix(hostOf(u), "www.")
	pageHost := strings.TrimPrefix(hostOf(pageURL), "www.")
	if host == "" || host == pageHost || isYellowPageHost(host) || isSocialHost(host) || isYellowPageJunkHost(host) {
		return websiteFromEmail(ld.Email)
	}
	return u
}

func walkJSONLD(body []byte, fn func(map[string]any)) {
	if fn == nil {
		return
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return
	}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			fn(t)
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return
		}
		var payload any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return
		}
		walk(payload)
	})
}

func jsonLDIsType(obj map[string]any, types ...string) bool {
	raw := obj["@type"]
	var got []string
	switch t := raw.(type) {
	case string:
		got = []string{t}
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok {
				got = append(got, s)
			}
		}
	}
	for _, have := range got {
		for _, want := range types {
			if strings.EqualFold(have, want) {
				return true
			}
		}
	}
	return false
}

func jsonLDString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []any:
		if len(t) > 0 {
			return jsonLDString(t[0])
		}
	}
	return ""
}
