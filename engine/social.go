package engine

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	tiktokHandleRe = regexp.MustCompile(`(?i)(?:^|https?://)?(?:www\.|m\.)?tiktok\.com/@([A-Za-z0-9._]+)`)
	douyinUserRe   = regexp.MustCompile(`(?i)(?:^|https?://)?(?:www\.|m\.)?douyin\.com/user/([A-Za-z0-9_\-]+)`)
	douyinShortRe  = regexp.MustCompile(`(?i)(?:v|www)\.douyin\.com/([A-Za-z0-9]+)`)
	instagramRe    = regexp.MustCompile(`(?i)(?:^|https?://)?(?:www\.)?instagram\.com/([A-Za-z0-9._]+)`)
	youtubeAtRe    = regexp.MustCompile(`(?i)(?:^|https?://)?(?:www\.)?youtube\.com/@([A-Za-z0-9._\-]+)`)
	youtubeChanRe  = regexp.MustCompile(`(?i)youtube\.com/channel/([A-Za-z0-9_\-]+)`)
)

var reservedPaths = map[string]struct{}{
	"tag": {}, "music": {}, "video": {}, "explore": {}, "search": {},
	"live": {}, "place": {}, "discover": {}, "messages": {}, "login": {},
	"p": {}, "reel": {}, "stories": {}, "accounts": {}, "about": {},
	"watch": {}, "results": {}, "channel": {}, "c": {}, "user": {},
	"feed": {}, "hashtag": {}, "trending": {}, "foryou": {},
}

// ParseSocialURL extracts a profile hit from a TikTok / Douyin / Instagram / YouTube URL.
func ParseSocialURL(raw, title, snippet string) (Hit, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Hit{}, false
	}

	decoded := unwrapRedirect(raw)

	if m := tiktokHandleRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle := strings.TrimRight(m[1], ".")
		if _, skip := reservedPaths[strings.ToLower(handle)]; skip {
			return Hit{}, false
		}

		return tiktokHit(handle, title, snippet, "websearch"), true
	}

	if m := douyinUserRe.FindStringSubmatch(decoded); len(m) == 2 {
		return douyinHit(m[1], title, snippet, decoded, "websearch"), true
	}

	if m := instagramRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle := strings.TrimRight(m[1], ".")
		if _, skip := reservedPaths[strings.ToLower(handle)]; skip {
			return Hit{}, false
		}

		home := "https://www.instagram.com/" + handle + "/"

		return Hit{
			ID:          "instagram:" + strings.ToLower(handle),
			Kind:        KindPeople,
			Platform:    PlatformInstagram,
			Name:        displayName(title, handle),
			Handle:      handle,
			Title:       title,
			Snippet:     snippet,
			HomepageURL: home,
			MessageURL:  home,
			MessageHint: "打开 Instagram 主页后点击 Message（需登录官方账号，系统不会代发私信）",
			Source:      "websearch",
			Score:       70,
		}, true
	}

	if m := youtubeAtRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle := m[1]
		home := "https://www.youtube.com/@" + handle

		return Hit{
			ID:          "youtube:@" + strings.ToLower(handle),
			Kind:        KindPeople,
			Platform:    PlatformYouTube,
			Name:        displayName(title, handle),
			Handle:      handle,
			Title:       title,
			Snippet:     snippet,
			HomepageURL: home,
			MessageURL:  home,
			MessageHint: "YouTube 无统一私信；请通过主页「关于」里的邮箱/社媒联系",
			Source:      "websearch",
			Score:       60,
		}, true
	}

	if m := youtubeChanRe.FindStringSubmatch(decoded); len(m) == 2 {
		home := "https://www.youtube.com/channel/" + m[1]

		return Hit{
			ID:          "youtube:" + m[1],
			Kind:        KindPeople,
			Platform:    PlatformYouTube,
			Name:        displayName(title, m[1]),
			Handle:      m[1],
			Title:       title,
			Snippet:     snippet,
			HomepageURL: home,
			MessageURL:  home,
			MessageHint: "YouTube 无统一私信；请通过主页「关于」里的邮箱/社媒联系",
			Source:      "websearch",
			Score:       55,
		}, true
	}

	if m := douyinShortRe.FindStringSubmatch(decoded); len(m) == 2 && strings.Contains(strings.ToLower(decoded), "douyin") {
		home := "https://v.douyin.com/" + m[1]

		return Hit{
			ID:          "douyin:short:" + m[1],
			Kind:        KindPeople,
			Platform:    PlatformDouyin,
			Name:        displayName(title, m[1]),
			Handle:      m[1],
			Title:       title,
			Snippet:     snippet,
			HomepageURL: home,
			MessageURL:  home,
			MessageHint: "打开抖音主页后点击「私信」（需登录官方 App，系统不会代发）",
			Source:      "websearch",
			Score:       50,
		}, true
	}

	return Hit{}, false
}

func tiktokHit(handle, title, snippet, source string) Hit {
	handle = strings.TrimSpace(handle)
	home := "https://www.tiktok.com/@" + handle

	return Hit{
		ID:          "tiktok:" + strings.ToLower(handle),
		Kind:        KindPeople,
		Platform:    PlatformTikTok,
		Name:        displayName(title, handle),
		Handle:      handle,
		Title:       title,
		Snippet:     snippet,
		HomepageURL: home,
		MessageURL:  home,
		MessageHint: "打开 TikTok 主页后点击 Message（需登录官方账号，系统不会代发私信）",
		Source:      source,
		Score:       80,
	}
}

func douyinHit(userID, title, snippet, homepage, source string) Hit {
	if homepage == "" {
		homepage = "https://www.douyin.com/user/" + userID
	}

	return Hit{
		ID:          "douyin:" + userID,
		Kind:        KindPeople,
		Platform:    PlatformDouyin,
		Name:        displayName(title, userID),
		Handle:      userID,
		Title:       title,
		Snippet:     snippet,
		HomepageURL: homepage,
		MessageURL:  homepage,
		MessageHint: "打开抖音主页后点击「私信」（需登录官方 App，系统不会代发）",
		Source:      source,
		Score:       80,
	}
}

func displayName(title, fallback string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return fallback
	}

	title = strings.TrimSpace(strings.Split(title, "|")[0])
	title = strings.TrimSpace(strings.Split(title, " - ")[0])
	title = strings.TrimSpace(strings.Split(title, " – ")[0])

	if i := strings.Index(title, "(@"); i > 0 {
		return strings.TrimSpace(title[:i])
	}

	if i := strings.Index(title, "（@"); i > 0 {
		return strings.TrimSpace(title[:i])
	}

	return title
}

func unwrapRedirect(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	for _, key := range []string{"uddg", "u", "url", "q"} {
		if v := u.Query().Get(key); v != "" {
			if decoded, err := url.QueryUnescape(v); err == nil && strings.HasPrefix(decoded, "http") {
				return decoded
			}
		}
	}

	return raw
}

func looksLikeHandle(s string) bool {
	s = strings.TrimPrefix(strings.TrimSpace(s), "@")
	if len(s) < 2 || len(s) > 24 {
		return false
	}

	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' {
			continue
		}

		return false
	}

	return true
}
