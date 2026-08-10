package enrich

import (
	"regexp"
	"strings"

	"github.com/mcnijman/go-emailaddress"
)

// roleLocalParts are department mailboxes worth contacting directly. Ordered
// by how close the department sits to a purchasing decision.
var roleLocalParts = []string{
	"purchasing", "purchase", "procurement", "buyer", "buying", "sourcing",
	"sales", "export", "import", "trade", "commercial", "business",
	"bd", "partner", "partnership", "wholesale", "oem", "odm",
	"ceo", "owner", "director", "manager", "gm",
	"marketing", "pr", "press", "media",
}

// genericLocalParts reach a human but not a specific one.
var genericLocalParts = []string{
	"info", "contact", "hello", "hi", "enquiry", "enquiries", "inquiry",
	"inquiries", "office", "mail", "email", "general", "reception", "admin",
	"kontakt", "contacto", "contatto", "correo", "iletisim",
}

// supportLocalParts are unlikely to reach a buyer and should rank last.
var supportLocalParts = []string{
	"support", "help", "helpdesk", "service", "customerservice", "care",
	"noreply", "no-reply", "donotreply", "bounce", "mailer-daemon",
	"abuse", "postmaster", "webmaster", "hostmaster", "privacy", "legal",
	"dpo", "gdpr", "unsubscribe", "billing", "invoice", "accounts",
	"accounting", "jobs", "career", "careers", "hr", "recruitment",
}

// junkEmailDomains belong to tooling, placeholders and site builders rather
// than to the company being researched.
var junkEmailDomains = []string{
	"example.com", "example.org", "example.net", "domain.com", "email.com",
	"yourdomain.com", "yoursite.com", "sentry.io", "sentry-cdn.com",
	"wixpress.com", "wix.com", "squarespace.com", "godaddy.com",
	"shopify.com", "myshopify.com", "cloudflare.com", "w3.org",
	"schema.org", "googlemail.local", "test.com", "acme.com",
	"company.com", "mysite.com", "site.com", "website.com",
	"sentry.wixpress.com",
}

// junkLocalParts show up in tracking pixels, CSS sprites and code samples.
var junkLocalParts = []string{
	"user", "username", "youremail", "your-email", "your_email", "name",
	"firstname", "lastname", "someone", "somebody", "test", "example",
	"sample", "foo", "bar", "abc", "xxx", "email address", "sentry",
}

// assetSuffixes catch "logo@2x.png"-style strings that look like addresses to
// a naive regex.
var assetSuffixes = []string{
	".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".css",
	".js", ".woff", ".woff2", ".ttf", ".eot", ".mp4", ".webm", ".pdf",
}

// obfuscatedEmailRe matches the "info (at) example (dot) com" spelling that
// sites use to slow down naive harvesters. Separators may be bracketed,
// parenthesised or bare, with optional surrounding whitespace.
var obfuscatedEmailRe = regexp.MustCompile(
	`(?i)([a-z0-9][a-z0-9._%+\-]{0,63})\s*[\[({<]?\s*(?:@|\(at\)|\[at\]|\{at\}|\bat\b)\s*[\])}>]?\s*` +
		`([a-z0-9][a-z0-9.\-]{0,61})\s*[\[({<]?\s*(?:\.|\(dot\)|\[dot\]|\{dot\}|\bdot\b)\s*[\])}>]?\s*([a-z]{2,24})`,
)

// cfEmailRe matches Cloudflare's email-obfuscation payload, which is a
// hex-encoded address XORed with its own first byte.
var cfEmailRe = regexp.MustCompile(`(?i)data-cfemail="([0-9a-f]{6,})"`)

// ExtractEmails pulls addresses out of a page body, de-obfuscating the common
// evasion patterns, and classifies each one. companyDomain (may be empty) is
// used to flag on-domain addresses.
func ExtractEmails(body []byte, companyDomain, source string) []Email {
	text := string(body)

	candidates := make([]string, 0, 8)

	for _, found := range emailaddress.Find(body, false) {
		candidates = append(candidates, found.String())
	}

	candidates = append(candidates, findObfuscatedEmails(text)...)
	candidates = append(candidates, decodeCloudflareEmails(text)...)

	return classifyEmails(candidates, companyDomain, source)
}

// ExtractMailtoEmails reads addresses out of mailto: hrefs. These are the
// highest-confidence signal because the site owner published them as links.
func ExtractMailtoEmails(hrefs []string, companyDomain, source string) []Email {
	candidates := make([]string, 0, len(hrefs))

	for _, href := range hrefs {
		if !strings.HasPrefix(strings.ToLower(href), "mailto:") {
			continue
		}

		value := href[len("mailto:"):]
		// mailto links may carry ?subject=... and comma-separated recipients.
		if idx := strings.IndexAny(value, "?#"); idx >= 0 {
			value = value[:idx]
		}

		for _, part := range strings.Split(value, ",") {
			candidates = append(candidates, strings.TrimSpace(unescapePercent(part)))
		}
	}

	return classifyEmails(candidates, companyDomain, source)
}

func classifyEmails(candidates []string, companyDomain, source string) []Email {
	seen := make(map[string]bool, len(candidates))
	out := make([]Email, 0, len(candidates))

	for _, raw := range candidates {
		address, ok := normalizeEmail(raw)
		if !ok || seen[address] {
			continue
		}

		seen[address] = true

		local, domain, _ := strings.Cut(address, "@")

		out = append(out, Email{
			Address:  address,
			Kind:     classifyLocalPart(local),
			OnDomain: companyDomain != "" && sameRegistrableDomain(domain, companyDomain),
			Source:   source,
		})
	}

	return out
}

func normalizeEmail(raw string) (string, bool) {
	raw = strings.TrimSpace(strings.Trim(raw, ".,;:<>()[]{}\"'"))
	if raw == "" {
		return "", false
	}

	parsed, err := emailaddress.Parse(raw)
	if err != nil {
		return "", false
	}

	address := strings.ToLower(parsed.String())

	local, domain, ok := strings.Cut(address, "@")
	if !ok {
		return "", false
	}

	for _, suffix := range assetSuffixes {
		if strings.HasSuffix(address, suffix) {
			return "", false
		}
	}

	for _, junk := range junkEmailDomains {
		if domain == junk || strings.HasSuffix(domain, "."+junk) {
			return "", false
		}
	}

	for _, junk := range junkLocalParts {
		if local == junk {
			return "", false
		}
	}

	// A local part that is entirely hex and long is almost always a tracking
	// or cache-busting token rather than a mailbox.
	if len(local) >= 16 && isHex(local) {
		return "", false
	}

	return address, true
}

func classifyLocalPart(local string) EmailKind {
	normalized := strings.NewReplacer(".", "", "-", "", "_", "").Replace(strings.ToLower(local))

	for _, needle := range supportLocalParts {
		if normalized == strings.ReplaceAll(needle, "-", "") {
			return EmailKindSupport
		}
	}

	for _, needle := range genericLocalParts {
		if normalized == needle {
			return EmailKindGeneric
		}
	}

	for _, needle := range roleLocalParts {
		if normalized == needle || strings.HasPrefix(normalized, needle) {
			return EmailKindRole
		}
	}

	// firstname.lastname / f.lastname style addresses are individuals.
	if strings.ContainsAny(local, "._-") {
		return EmailKindPersonal
	}

	// A bare word that matches none of the known mailbox vocabularies is more
	// likely a person's name than a department.
	if len(local) >= 3 {
		return EmailKindPersonal
	}

	return EmailKindGeneric
}

func findObfuscatedEmails(text string) []string {
	matches := obfuscatedEmailRe.FindAllStringSubmatch(text, -1)

	out := make([]string, 0, len(matches))

	for _, m := range matches {
		out = append(out, m[1]+"@"+m[2]+"."+m[3])
	}

	return out
}

// decodeCloudflareEmails reverses Cloudflare's data-cfemail encoding: the
// first hex byte is the XOR key for every following byte.
func decodeCloudflareEmails(text string) []string {
	matches := cfEmailRe.FindAllStringSubmatch(text, -1)

	out := make([]string, 0, len(matches))

	for _, m := range matches {
		encoded := m[1]
		if len(encoded)%2 != 0 {
			continue
		}

		bytesOut := make([]byte, 0, len(encoded)/2-1)

		key, ok := hexByte(encoded[0:2])
		if !ok {
			continue
		}

		valid := true

		for i := 2; i < len(encoded); i += 2 {
			b, bok := hexByte(encoded[i : i+2])
			if !bok {
				valid = false

				break
			}

			bytesOut = append(bytesOut, b^key)
		}

		if valid && len(bytesOut) > 0 {
			out = append(out, string(bytesOut))
		}
	}

	return out
}

func hexByte(s string) (byte, bool) {
	var v byte

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch {
		case c >= '0' && c <= '9':
			v = v<<4 | (c - '0')
		case c >= 'a' && c <= 'f':
			v = v<<4 | (c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			v = v<<4 | (c - 'A' + 10)
		default:
			return 0, false
		}
	}

	return v, true
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}

	return true
}

func unescapePercent(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}

	var b strings.Builder

	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if v, ok := hexByte(s[i+1 : i+3]); ok {
				b.WriteByte(v)

				i += 2

				continue
			}
		}

		b.WriteByte(s[i])
	}

	return b.String()
}
