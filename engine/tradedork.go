package engine

import (
	"net/url"
	"regexp"
	"strings"
)

// tradeGuruQueries builds the Google dorks 外贸获客常用 to surface
// merchant social homepages (profile pages, not posts/reels).
func tradeGuruQueries(terms []string, wanted map[string]bool, country, role string) []publicQuery {
	terms = clipTerms(uniqueFoldedStrings(terms), 3)
	if len(terms) == 0 {
		return nil
	}
	engGeo := CountryQueryToken(country, false)
	role = NormalizeRole(role)

	var out []publicQuery
	seen := map[string]bool{}
	add := func(platform, query string) {
		query = strings.TrimSpace(query)
		if query == "" {
			return
		}
		if platform != "" && len(wanted) > 0 && !wanted[platform] {
			return
		}
		key := platform + "\t" + query
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, publicQuery{platform: platform, query: query, preferGoogle: true})
	}

	for _, term := range terms {
		qt := quoteSearchTerm(term)
		if qt == "" {
			continue
		}

		if wanted[PlatformFacebook] {
			add(PlatformFacebook, "inurl:facebook.com "+qt+" -inurl:posts -inurl:photos -inurl:videos -inurl:reel")
			add(PlatformFacebook, "site:facebook.com/pages "+qt)
			add(PlatformFacebook, "intitle:"+qt+" site:facebook.com")
			add(PlatformFacebook, "site:facebook.com "+qt+" (manufacturer OR supplier OR importer OR wholesaler)")
			add(PlatformFacebook, "site:facebook.com \"We are\" "+qt)
			add(PlatformFacebook, "site:facebook.com "+qt+" (WhatsApp OR \"contact us\" OR about)")
			if engGeo != "" {
				add(PlatformFacebook, "inurl:facebook.com "+qt+" "+engGeo+" -inurl:posts -inurl:photos")
			}
		}
		if wanted[PlatformInstagram] {
			add(PlatformInstagram, "inurl:instagram.com "+qt+" -inurl:/p/ -inurl:/reel/")
			add(PlatformInstagram, "site:instagram.com "+qt+" (shop OR store OR official)")
		}
		if wanted[PlatformLinkedIn] {
			add(PlatformLinkedIn, "site:linkedin.com/company "+qt)
			add(PlatformLinkedIn, "intitle:"+qt+" site:linkedin.com/company")
			if role == RoleSeller {
				add(PlatformLinkedIn, "site:linkedin.com/company "+qt+" manufacturer")
			} else {
				add(PlatformLinkedIn, "site:linkedin.com/company "+qt+" (importer OR distributor OR \"trading company\")")
			}
			if engGeo != "" {
				add(PlatformLinkedIn, "site:linkedin.com/company "+qt+" "+engGeo)
			}
		}
		if wanted[PlatformTikTok] {
			add(PlatformTikTok, "site:tiktok.com/@ "+qt)
		}
		if wanted[PlatformYouTube] {
			add(PlatformYouTube, "site:youtube.com/@ "+qt)
		}
		if wanted[PlatformX] {
			add(PlatformX, "site:x.com "+qt+" -inurl:status")
		}

		// Open-web harvest: company sites that publish their social URLs.
		if wanted[PlatformFacebook] || wanted[PlatformInstagram] || wanted[PlatformLinkedIn] {
			add("", qt+` "facebook.com/" "instagram.com/"`)
			add("", qt+` "Follow us on Facebook"`)
			add("", qt+` intext:"linkedin.com/company"`)
			if role != RoleSeller {
				add("", `"looking for" `+qt+` (supplier OR manufacturer) (facebook.com OR linkedin.com)`)
			}
		}
	}
	return out
}

func quoteSearchTerm(term string) string {
	term = strings.TrimSpace(term)
	if term == "" {
		return ""
	}
	if strings.ContainsAny(term, " \t") && !strings.HasPrefix(term, `"`) {
		return `"` + term + `"`
	}
	return term
}

var googleRedirectRe = regexp.MustCompile(`/url\?(?:amp;)?(?:q|url)=([^&"']+)`)

func decodeGoogleRedirectsBlob(html string) []string {
	if !strings.Contains(html, "/url?") {
		return nil
	}
	var out []string
	for _, m := range googleRedirectRe.FindAllStringSubmatch(html, blobExtractCap*2) {
		if len(m) != 2 {
			continue
		}
		raw, err := url.QueryUnescape(m[1])
		if err != nil {
			raw = m[1]
		}
		if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
			out = append(out, raw)
		}
	}
	return out
}

func decodeGoogleRedirect(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return href
	}
	if strings.HasPrefix(href, "/url?") || strings.Contains(href, "google.com/url?") {
		if strings.HasPrefix(href, "/url?") {
			href = "https://www.google.com" + href
		}
		u, err := url.Parse(href)
		if err != nil {
			return href
		}
		q := u.Query().Get("q")
		if q == "" {
			q = u.Query().Get("url")
		}
		if strings.HasPrefix(q, "http://") || strings.HasPrefix(q, "https://") {
			return q
		}
	}
	return href
}
