package enrich

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Page is one already-fetched company web page.
type Page struct {
	// URL is the final URL after redirects; relative links resolve against it.
	URL string
	// HTML is the raw response body, used for signals that live in markup
	// rather than in visible text (platform fingerprints, obfuscated emails).
	HTML []byte
	// Doc is the parsed document.
	Doc *goquery.Document
}

// AnalyzePage turns one page into a partial profile. Callers merge the partials
// from every crawled page into a single CompanyProfile.
//
// companyDomain is the company's own registrable domain, used to tell
// first-party addresses from third-party ones. Pass "" when unknown.
func AnalyzePage(page Page, companyDomain string) *CompanyProfile {
	profile := &CompanyProfile{}

	if page.Doc == nil {
		return profile
	}

	source := page.URL

	if companyDomain == "" {
		companyDomain = RegistrableDomain(HostFromURL(page.URL))
	}

	hrefs := collectAttr(page.Doc, "a[href]", "href")
	text := VisibleText(page.Doc)
	html := string(page.HTML)

	profile.PagesCrawled = []string{source}

	// mailto links are authored contact points, so they are collected before
	// the noisier body-text scan and win on classification during merge.
	profile.Emails = mergeEmails(
		ExtractMailtoEmails(hrefs, companyDomain, source),
		ExtractEmails(page.HTML, companyDomain, source),
	)

	profile.Phones = mergePhones(
		ExtractPhonesFromLinks(hrefs, source),
		ExtractPhonesFromText(text, source),
	)

	profile.Socials = ExtractSocials(hrefs, source)
	profile.People = mergePeople(extractPeopleFromDOM(page.Doc, source), ExtractPeopleFromText(text, source))

	profile.Description = metaDescription(page.Doc)
	profile.LegalName = metaSiteName(page.Doc)
	profile.LogoURL = resolveURL(source, logoURL(page.Doc))
	profile.FoundedYear = DetectFoundedYear(text)
	profile.EmployeeRange = DetectEmployeeRange(text)
	profile.TradeRoles = DetectTradeRoles(text)
	profile.Certifications = DetectCertifications(text)
	profile.Markets = DetectMarkets(text)
	profile.Languages = DetectLanguages(page.Doc)
	profile.ProductKeywords = extractProductKeywords(page.Doc)
	profile.RegistrationIDs = ExtractRegistrationIDsFromText(text, source)
	profile.Addresses = extractAddresses(page.Doc)
	profile.Platform, profile.HasEcommerce = DetectPlatform(html)

	// Structured data is the most reliable source available, so it is merged
	// last and wins wherever it is present.
	profile.Merge(ParseJSONLD(collectJSONLD(page.Doc), source))

	return profile
}

// VisibleText returns the human-readable text of a document with scripts,
// styles and template noise removed, collapsed to single spaces.
func VisibleText(doc *goquery.Document) string {
	clone := goquery.NewDocumentFromNode(doc.Get(0))
	clone.Find("script,style,noscript,template,svg,iframe").Remove()

	return strings.Join(strings.Fields(clone.Text()), " ")
}

// DetectLanguages lists the locales a site publishes in, using hreflang
// annotations and the document language. A company serving many locales is
// usually already exporting.
func DetectLanguages(doc *goquery.Document) []string {
	seen := make(map[string]bool, 8)

	var out []string

	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == "x-default" {
			return
		}

		// Keep only the language subtag so "en-GB" and "en-US" collapse.
		if idx := strings.IndexAny(value, "-_"); idx > 0 {
			value = value[:idx]
		}

		if len(value) != 2 || seen[value] {
			return
		}

		seen[value] = true
		out = append(out, value)
	}

	if lang, ok := doc.Find("html").First().Attr("lang"); ok {
		add(lang)
	}

	doc.Find("link[rel='alternate'][hreflang]").Each(func(_ int, s *goquery.Selection) {
		if hreflang, ok := s.Attr("hreflang"); ok {
			add(hreflang)
		}
	})

	doc.Find("meta[property='og:locale'],meta[property='og:locale:alternate']").Each(func(_ int, s *goquery.Selection) {
		if content, ok := s.Attr("content"); ok {
			add(content)
		}
	})

	return out
}

func metaDescription(doc *goquery.Document) string {
	for _, selector := range []string{
		"meta[name='description']",
		"meta[property='og:description']",
		"meta[name='twitter:description']",
	} {
		if content, ok := doc.Find(selector).First().Attr("content"); ok {
			if trimmed := strings.Join(strings.Fields(content), " "); trimmed != "" {
				return trimmed
			}
		}
	}

	return ""
}

func metaSiteName(doc *goquery.Document) string {
	if content, ok := doc.Find("meta[property='og:site_name']").First().Attr("content"); ok {
		if trimmed := strings.TrimSpace(content); trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func logoURL(doc *goquery.Document) string {
	for _, selector := range []string{
		"img[class*='logo']",
		"img[id*='logo']",
		"img[alt*='logo' i]",
		"meta[property='og:image']",
		"link[rel='apple-touch-icon']",
	} {
		selection := doc.Find(selector).First()
		if selection.Length() == 0 {
			continue
		}

		for _, attr := range []string{"src", "content", "href", "data-src"} {
			if value, ok := selection.Attr(attr); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}

	return ""
}

func collectAttr(doc *goquery.Document, selector, attr string) []string {
	var out []string

	doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
		if value, ok := s.Attr(attr); ok {
			out = append(out, value)
		}
	})

	return out
}

func collectJSONLD(doc *goquery.Document) []string {
	var out []string

	doc.Find("script[type='application/ld+json']").Each(func(_ int, s *goquery.Selection) {
		if content := strings.TrimSpace(s.Text()); content != "" {
			out = append(out, content)
		}
	})

	return out
}

func extractAddresses(doc *goquery.Document) []string {
	var out []string

	doc.Find("address,[itemtype*='PostalAddress'],[class*='address']").Each(func(_ int, s *goquery.Selection) {
		text := strings.Join(strings.Fields(s.Text()), " ")
		if len(text) >= minAddressLength && len(text) <= maxAddressLength {
			out = append(out, text)
		}
	})

	return mergeStrings(nil, out)
}

// Address length bounds: shorter strings are labels ("Address:"), longer ones
// are whole footers that happen to sit in an address-classed container.
const (
	minAddressLength = 12
	maxAddressLength = 200
)

// maxProductKeywordWords keeps headings that read like product names and drops
// marketing sentences.
const maxProductKeywordWords = 6

func extractProductKeywords(doc *goquery.Document) []string {
	var out []string

	if content, ok := doc.Find("meta[name='keywords']").First().Attr("content"); ok {
		for _, keyword := range strings.Split(content, ",") {
			keyword = strings.Join(strings.Fields(keyword), " ")
			if keyword != "" && len(strings.Fields(keyword)) <= maxProductKeywordWords {
				out = append(out, keyword)
			}
		}
	}

	doc.Find("h1,h2,h3").Each(func(_ int, s *goquery.Selection) {
		heading := strings.Join(strings.Fields(s.Text()), " ")
		if heading == "" || len(strings.Fields(heading)) > maxProductKeywordWords {
			return
		}

		if nameStopWords[strings.ToLower(heading)] {
			return
		}

		out = append(out, heading)
	})

	return mergeStrings(nil, out)
}

// nameCandidateSelector targets the elements team and staff cards use for a
// person's name.
const nameCandidateSelector = "h2,h3,h4,h5,h6,strong,b,[class*='name'],[class*='member'] > *,[class*='team'] > *"

// maxPersonCardText bounds how much text a person card may hold; beyond that we
// are looking at an article, not a staff entry.
const maxPersonCardText = 300

// extractPeopleFromDOM reads staff cards: a short heading holding a name, with
// the job title in the next element or in the surrounding container.
func extractPeopleFromDOM(doc *goquery.Document, source string) []Person {
	var out []Person

	doc.Find(nameCandidateSelector).Each(func(_ int, s *goquery.Selection) {
		name := strings.Join(strings.Fields(s.Text()), " ")
		if name == "" || len(name) > maxPersonCardText {
			return
		}

		for _, title := range personTitleCandidates(s, name) {
			if person, ok := newPerson(name, title, source); ok {
				out = append(out, person)

				return
			}
		}
	})

	return out
}

func personTitleCandidates(s *goquery.Selection, name string) []string {
	candidates := make([]string, 0, 3)

	if next := strings.Join(strings.Fields(s.Next().Text()), " "); next != "" {
		candidates = append(candidates, next)
	}

	parentText := strings.Join(strings.Fields(s.Parent().Text()), " ")
	if len(parentText) <= maxPersonCardText {
		if remainder := strings.TrimSpace(strings.Replace(parentText, name, "", 1)); remainder != "" {
			candidates = append(candidates, strings.Trim(remainder, " -–—|,:"))
		}
	}

	return candidates
}
