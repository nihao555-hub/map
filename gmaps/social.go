package gmaps

import (
	"net/url"
	"regexp"
	"strings"
)

// SocialLinks 从 Google Maps 官网字段 / 商家网页里拆出的社媒链接
type SocialLinks struct {
	Facebook  string
	Instagram string
	LinkedIn  string
	Twitter   string
	TikTok    string
	YouTube   string
}

var (
	hrefSocialRe = regexp.MustCompile(`(?i)href=["'](https?://[^"']+)["']`)
	socialHosts  = []struct {
		needles []string
		field   string
	}{
		{[]string{"facebook.com", "fb.com", "fb.me"}, "facebook"},
		{[]string{"instagram.com"}, "instagram"},
		{[]string{"linkedin.com"}, "linkedin"},
		{[]string{"twitter.com", "x.com"}, "twitter"},
		{[]string{"tiktok.com"}, "tiktok"},
		{[]string{"youtube.com", "youtu.be"}, "youtube"},
	}
)

// classifySocialURL 识别社媒 URL；非社媒返回空 kind
func classifySocialURL(raw string) (kind, normalized string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	lower := strings.ToLower(raw)
	if !strings.Contains(lower, "http") {
		if strings.HasPrefix(lower, "//") {
			raw = "https:" + raw
			lower = strings.ToLower(raw)
		} else if strings.Contains(lower, ".") {
			raw = "https://" + strings.TrimPrefix(raw, "/")
			lower = strings.ToLower(raw)
		}
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		// 宽松匹配：整串包含 host
		for _, s := range socialHosts {
			for _, n := range s.needles {
				if strings.Contains(lower, n) {
					return s.field, strings.TrimSpace(raw)
				}
			}
		}

		return "", ""
	}

	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	host = strings.TrimPrefix(host, "mobile.")

	for _, s := range socialHosts {
		for _, n := range s.needles {
			if host == n || strings.HasSuffix(host, "."+n) || strings.Contains(host, n) {
				// 规范化：保留 https + host + path
				u.Scheme = "https"
				u.RawQuery = ""
				u.Fragment = ""
				return s.field, strings.TrimRight(u.String(), "/")
			}
		}
	}

	return "", ""
}

// applySocialURL 写入空字段（不覆盖已有值）
func (e *Entry) applySocialURL(raw string) {
	if e == nil {
		return
	}

	kind, link := classifySocialURL(raw)
	if kind == "" || link == "" {
		return
	}

	switch kind {
	case "facebook":
		if e.Facebook == "" {
			e.Facebook = link
		}
	case "instagram":
		if e.Instagram == "" {
			e.Instagram = link
		}
	case "linkedin":
		if e.LinkedIn == "" {
			e.LinkedIn = link
		}
	case "twitter":
		if e.Twitter == "" {
			e.Twitter = link
		}
	case "tiktok":
		if e.TikTok == "" {
			e.TikTok = link
		}
	case "youtube":
		if e.YouTube == "" {
			e.YouTube = link
		}
	}
}

// PromoteSocialFromMapsFields 从 Maps 的 website / 预约 / 外卖链接里拆社媒
func (e *Entry) PromoteSocialFromMapsFields() {
	if e == nil {
		return
	}

	e.applySocialURL(e.WebSite)
	for _, ls := range e.Reservations {
		e.applySocialURL(ls.Link)
	}
	for _, ls := range e.OrderOnline {
		e.applySocialURL(ls.Link)
	}
	e.applySocialURL(e.Menu.Link)
}

// extractSocialFromHTML 从网页 HTML 里抓社媒链接
func extractSocialFromHTML(body []byte) SocialLinks {
	var out SocialLinks
	if len(body) == 0 {
		return out
	}

	seen := map[string]bool{}
	for _, m := range hrefSocialRe.FindAllSubmatch(body, -1) {
		if len(m) < 2 {
			continue
		}
		kind, link := classifySocialURL(string(m[1]))
		if kind == "" || link == "" || seen[kind] {
			continue
		}
		seen[kind] = true
		switch kind {
		case "facebook":
			out.Facebook = link
		case "instagram":
			out.Instagram = link
		case "linkedin":
			out.LinkedIn = link
		case "twitter":
			out.Twitter = link
		case "tiktok":
			out.TikTok = link
		case "youtube":
			out.YouTube = link
		}
	}

	return out
}

func (e *Entry) mergeSocial(s SocialLinks) {
	if e == nil {
		return
	}
	if e.Facebook == "" {
		e.Facebook = s.Facebook
	}
	if e.Instagram == "" {
		e.Instagram = s.Instagram
	}
	if e.LinkedIn == "" {
		e.LinkedIn = s.LinkedIn
	}
	if e.Twitter == "" {
		e.Twitter = s.Twitter
	}
	if e.TikTok == "" {
		e.TikTok = s.TikTok
	}
	if e.YouTube == "" {
		e.YouTube = s.YouTube
	}
}
