package enrich

import (
	"net/url"
	"strings"
)

// secondLevelLabels are the registry-operated labels that sit between a
// company's name and the country code, e.g. the "co" in "acme.co.uk". Matching
// on them keeps "acme.co.uk" and "shop.acme.co.uk" recognisable as the same
// company without shipping a full public-suffix list.
var secondLevelLabels = map[string]bool{
	"co": true, "com": true, "net": true, "org": true, "gov": true,
	"edu": true, "ac": true, "mil": true, "or": true, "ne": true,
	"go": true, "in": true, "info": true, "biz": true, "web": true,
}

// HostFromURL returns the lowercase hostname of rawURL without a leading
// "www.", or "" when rawURL is not parseable as an absolute URL.
func HostFromURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}

	if !strings.Contains(rawURL, "//") {
		rawURL = "https://" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
}

// RegistrableDomain reduces a hostname to the part a company registers,
// e.g. "shop.acme.co.uk" becomes "acme.co.uk".
func RegistrableDomain(host string) string {
	host = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
	if host == "" {
		return ""
	}

	labels := strings.Split(host, ".")
	if len(labels) <= 2 {
		return host
	}

	last := labels[len(labels)-1]
	secondLast := labels[len(labels)-2]

	// "acme.co.uk": keep three labels when the middle one is a registry label
	// and the TLD is a two-letter country code.
	if len(last) == 2 && secondLevelLabels[secondLast] && len(labels) >= 3 {
		return strings.Join(labels[len(labels)-3:], ".")
	}

	return secondLast + "." + last
}

func sameRegistrableDomain(a, b string) bool {
	ra, rb := RegistrableDomain(a), RegistrableDomain(b)

	return ra != "" && ra == rb
}

func digitsOnly(s string) string {
	var b strings.Builder

	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}

	return b.String()
}
