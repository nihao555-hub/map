package enrich

import (
	"net/url"
	"strings"
)

// Social network identifiers used in Social.Network.
const (
	NetworkLinkedIn   = "linkedin"
	NetworkFacebook   = "facebook"
	NetworkInstagram  = "instagram"
	NetworkTwitter    = "twitter"
	NetworkYouTube    = "youtube"
	NetworkTikTok     = "tiktok"
	NetworkPinterest  = "pinterest"
	NetworkWhatsApp   = "whatsapp"
	NetworkTelegram   = "telegram"
	NetworkWeChat     = "wechat"
	NetworkWeibo      = "weibo"
	NetworkXing       = "xing"
	NetworkVK         = "vk"
	NetworkAlibaba    = "alibaba"
	NetworkMadeInCN   = "made-in-china"
	NetworkAmazon     = "amazon"
	NetworkEtsy       = "etsy"
	NetworkYelp       = "yelp"
	NetworkTrustpilot = "trustpilot"
	NetworkGitHub     = "github"
	NetworkVimeo      = "vimeo"
	NetworkThreads    = "threads"
	NetworkSkype      = "skype"
)

// socialHosts maps a hostname suffix to the network it identifies. B2B
// marketplaces (Alibaba, Made-in-China) and review sites (Trustpilot, Yelp)
// are included because for trade research they carry as much signal as the
// consumer social networks.
var socialHosts = []struct {
	suffix  string
	network string
}{
	{"linkedin.com", NetworkLinkedIn},
	{"facebook.com", NetworkFacebook},
	{"fb.com", NetworkFacebook},
	{"fb.me", NetworkFacebook},
	{"messenger.com", NetworkFacebook},
	{"instagram.com", NetworkInstagram},
	{"twitter.com", NetworkTwitter},
	{"x.com", NetworkTwitter},
	{"threads.net", NetworkThreads},
	{"threads.com", NetworkThreads},
	{"youtube.com", NetworkYouTube},
	{"youtu.be", NetworkYouTube},
	{"tiktok.com", NetworkTikTok},
	{"pinterest.com", NetworkPinterest},
	{"pin.it", NetworkPinterest},
	{"wa.me", NetworkWhatsApp},
	{"whatsapp.com", NetworkWhatsApp},
	{"t.me", NetworkTelegram},
	{"telegram.me", NetworkTelegram},
	{"telegram.org", NetworkTelegram},
	{"weixin.qq.com", NetworkWeChat},
	{"weibo.com", NetworkWeibo},
	{"weibo.cn", NetworkWeibo},
	{"xing.com", NetworkXing},
	{"vk.com", NetworkVK},
	{"alibaba.com", NetworkAlibaba},
	{"1688.com", NetworkAlibaba},
	{"made-in-china.com", NetworkMadeInCN},
	{"amazon.com", NetworkAmazon},
	{"etsy.com", NetworkEtsy},
	{"yelp.com", NetworkYelp},
	{"trustpilot.com", NetworkTrustpilot},
	{"github.com", NetworkGitHub},
	{"vimeo.com", NetworkVimeo},
	{"skype.com", NetworkSkype},
}

// socialNoiseSegments are the share-intent, login and help paths that appear on
// almost every site and say nothing about who the company is.
var socialNoiseSegments = []string{
	"/sharer", "/share", "/share.php", "/intent/", "/dialog/",
	"/login", "/signup", "/help", "/about/", "/legal", "/policies",
	"/tr?", "/plugins/", "/embed", "/widgets", "/apps/",
	"/developers", "/business/", "/settings",
}

// ExtractSocials classifies outbound links into company social profiles.
// Links that merely share the current page are dropped.
func ExtractSocials(hrefs []string, base string) []Social {
	out := make([]Social, 0, len(hrefs))
	seen := make(map[string]bool, len(hrefs))

	for _, href := range hrefs {
		absolute := resolveURL(base, href)
		if absolute == "" {
			continue
		}

		social, ok := classifySocialURL(absolute)
		if !ok {
			continue
		}

		key := social.Network + "|" + strings.ToLower(social.URL)
		if seen[key] {
			continue
		}

		seen[key] = true
		out = append(out, social)
	}

	return out
}

func classifySocialURL(rawURL string) (Social, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return Social{}, false
	}

	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	if host == "" {
		return Social{}, false
	}

	network := ""

	for _, candidate := range socialHosts {
		if host == candidate.suffix || strings.HasSuffix(host, "."+candidate.suffix) {
			network = candidate.network

			break
		}
	}

	if network == "" {
		return Social{}, false
	}

	lowerPath := strings.ToLower(parsed.Path)
	if lowerPath == "" || lowerPath == "/" {
		// A bare domain link is a platform badge, not a profile — except for
		// contact-style networks where the handle lives in the query string.
		if parsed.RawQuery == "" {
			return Social{}, false
		}
	}

	pathAndQuery := lowerPath
	if parsed.RawQuery != "" {
		pathAndQuery += "?" + strings.ToLower(parsed.RawQuery)
	}

	for _, noise := range socialNoiseSegments {
		if strings.Contains(pathAndQuery, noise) {
			return Social{}, false
		}
	}

	social := Social{
		Network: network,
		URL:     canonicalSocialURL(parsed),
		Handle:  socialHandle(network, parsed),
	}

	social.IsPersonProfile = network == NetworkLinkedIn && strings.HasPrefix(lowerPath, "/in/")

	return social, true
}

// canonicalSocialURL strips tracking parameters and trailing slashes so the
// same profile linked from three pages collapses into one entry.
func canonicalSocialURL(parsed *url.URL) string {
	cleaned := *parsed
	cleaned.Fragment = ""

	query := cleaned.Query()
	for key := range query {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" || lower == "ref" || lower == "originalsubdomain" {
			query.Del(key)
		}
	}

	cleaned.RawQuery = query.Encode()

	if cleaned.Path != "/" {
		cleaned.Path = strings.TrimRight(cleaned.Path, "/")
	}

	if cleaned.Scheme == "" {
		cleaned.Scheme = "https"
	}

	return cleaned.String()
}

func socialHandle(network string, parsed *url.URL) string {
	switch network {
	case NetworkWhatsApp:
		if handle := parsed.Query().Get("phone"); handle != "" {
			return handle
		}

		return strings.Trim(parsed.Path, "/")
	case NetworkLinkedIn:
		segments := splitPath(parsed.Path)
		if len(segments) >= 2 {
			return segments[1]
		}

		return ""
	case NetworkYouTube:
		segments := splitPath(parsed.Path)
		if len(segments) == 0 {
			return ""
		}

		if strings.HasPrefix(segments[0], "@") {
			return strings.TrimPrefix(segments[0], "@")
		}

		if len(segments) >= 2 {
			return segments[1]
		}

		return segments[0]
	default:
		segments := splitPath(parsed.Path)
		if len(segments) == 0 {
			return ""
		}

		return strings.TrimPrefix(segments[0], "@")
	}
}

func splitPath(path string) []string {
	parts := strings.Split(strings.Trim(path, "/"), "/")

	out := make([]string, 0, len(parts))

	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}

	return out
}

// resolveURL turns href into an absolute URL against base, dropping anchors,
// javascript: handlers and other non-navigational values.
func resolveURL(base, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}

	lower := strings.ToLower(href)
	for _, scheme := range []string{"javascript:", "mailto:", "data:", "about:", "sms:", "fax:"} {
		if strings.HasPrefix(lower, scheme) {
			return ""
		}
	}

	parsedHref, err := url.Parse(href)
	if err != nil {
		return ""
	}

	if parsedHref.IsAbs() {
		return parsedHref.String()
	}

	parsedBase, err := url.Parse(base)
	if err != nil {
		return ""
	}

	return parsedBase.ResolveReference(parsedHref).String()
}
