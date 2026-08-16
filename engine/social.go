package engine

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	tiktokHandleRe   = regexp.MustCompile(`(?i)(?:^|https?://)?(?:www\.|m\.)?tiktok\.com/@([A-Za-z0-9._]+)`)
	douyinUserRe     = regexp.MustCompile(`(?i)(?:^|https?://)?(?:www\.|m\.)?douyin\.com/user/([A-Za-z0-9_\-]+)`)
	instagramRe      = regexp.MustCompile(`(?i)(?:^|https?://)?(?:www\.)?instagram\.com/([A-Za-z0-9._]+)`)
	youtubeAtRe      = regexp.MustCompile(`(?i)(?:^|https?://)?(?:www\.)?youtube\.com/@([A-Za-z0-9._\-]+)`)
	youtubeChanRe    = regexp.MustCompile(`(?i)youtube\.com/channel/([A-Za-z0-9_\-]+)`)
	facebookIDRe     = regexp.MustCompile(`(?i)(?:facebook\.com|fb\.com)/profile\.php\?id=(\d+)`)
	facebookPeopleRe = regexp.MustCompile(`(?i)(?:facebook\.com|fb\.com)/people/([^/?#]+)/(\d+)`)
	facebookUserRe   = regexp.MustCompile(`(?i)(?:facebook\.com|fb\.com)/([A-Za-z0-9.]+)`)
	linkedinInRe     = regexp.MustCompile(`(?i)linkedin\.com/in/([A-Za-z0-9_\-%]+)`)
	linkedinCoRe     = regexp.MustCompile(`(?i)linkedin\.com/company/([A-Za-z0-9_\-%]+)`)
	xHandleRe        = regexp.MustCompile(`(?i)(?:twitter|x)\.com/([A-Za-z0-9_]+)`)
	pinterestRe      = regexp.MustCompile(`(?i)pinterest\.(?:com|co\.[a-z]{2})/([A-Za-z0-9_]+)`)
	threadsRe        = regexp.MustCompile(`(?i)threads\.net/@([A-Za-z0-9._]+)`)
	xiaohongshuRe    = regexp.MustCompile(`(?i)(?:www\.)?xiaohongshu\.com/user/profile/([A-Za-z0-9]+)`)
	kuaishouRe       = regexp.MustCompile(`(?i)(?:www\.)?kuaishou\.com/profile/([A-Za-z0-9_\-]+)`)
	weiboUIDRe       = regexp.MustCompile(`(?i)(?:www\.|m\.)?weibo\.(?:com|cn)/u/(\d+)`)
	weiboNameRe      = regexp.MustCompile(`(?i)(?:www\.)?weibo\.com/n/([^/?#\s]+)`)
	bilibiliRe       = regexp.MustCompile(`(?i)space\.bilibili\.com/(\d+)`)
	telegramRe       = regexp.MustCompile(`(?i)(?:t\.me|telegram\.me)/([A-Za-z][A-Za-z0-9_]{3,31})`)
	redditUserRe     = regexp.MustCompile(`(?i)(?:www\.)?reddit\.com/(?:user|u)/([A-Za-z0-9_\-]+)`)
	twitchRe         = regexp.MustCompile(`(?i)(?:www\.)?twitch\.tv/([A-Za-z0-9_]+)`)
)

var reservedPaths = map[string]struct{}{
	"tag": {}, "music": {}, "video": {}, "explore": {}, "search": {},
	"live": {}, "place": {}, "discover": {}, "messages": {}, "login": {},
	"p": {}, "reel": {}, "stories": {}, "accounts": {}, "about": {},
	"watch": {}, "results": {}, "channel": {}, "c": {}, "user": {},
	"feed": {}, "hashtag": {}, "trending": {}, "foryou": {},
	"share": {}, "sharer": {}, "dialog": {}, "groups": {}, "events": {},
	"reels": {}, "marketplace": {}, "gaming": {}, "photos": {}, "photo": {},
	"videos": {}, "posts": {}, "permalink": {}, "people": {}, "pages": {},
	"privacy": {}, "settings": {}, "notifications": {}, "friends": {},
	"ads": {}, "business": {}, "jobs": {}, "home": {}, "intent": {},
	"compose": {}, "signup": {}, "download": {}, "pin": {}, "ideas": {},
	"today": {}, "i": {}, "tos": {}, "help": {}, "recover": {},
	"facebook": {}, "youtube": {}, "instagram": {}, "twitter": {}, "linkedin": {},
	"tiktok": {}, "douyin": {}, "tv": {}, "story": {}, "story.php": {},
	"photo.php": {}, "video.php": {}, "watch.php": {},
	"joinchat": {}, "addstickers": {}, "addemoji": {}, "addtheme": {},
	"proxy": {}, "socks": {}, "directory": {}, "clips": {}, "inventory": {},
	"drops": {}, "turbo": {}, "subscriptions": {}, "wallet": {},
	"index": {}, "index.php": {}, "index.html": {}, "privacy.php": {},
	"login.php": {}, "sharer.php": {}, "policy": {}, "policies": {},
	"tr": {}, "save": {}, "beacon": {}, "pixel": {}, "tracking": {},
	"plugins": {}, "ajax": {}, "cdn": {}, "static": {},
}

// ParseSocialURL extracts a profile hit from a supported social homepage URL.
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
			MessageHint: "YouTube 无统一私信；请通过主页「关于」里的邮箱/社媒联系（系统不会代发）",
			Source:      "websearch",
			Score:       60,
		}, true
	}

	if m := facebookIDRe.FindStringSubmatch(decoded); len(m) == 2 {
		home := "https://www.facebook.com/profile.php?id=" + m[1]
		return makeHit(PlatformFacebook, "id:"+m[1], m[1], home, title, snippet,
			"打开 Facebook 主页后点击 Message（需登录官方账号，系统不会代发私信）", 75), true
	}

	if m := facebookPeopleRe.FindStringSubmatch(decoded); len(m) == 3 {
		name, _ := url.PathUnescape(m[1])
		home := "https://www.facebook.com/people/" + m[1] + "/" + m[2]
		return makeHit(PlatformFacebook, "people:"+m[2], name, home, title, snippet,
			"打开 Facebook 主页后点击 Message（需登录官方账号，系统不会代发私信）", 74), true
	}

	if m := facebookUserRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle := strings.TrimRight(m[1], ".")
		if _, skip := reservedPaths[strings.ToLower(handle)]; skip {
			return Hit{}, false
		}
		if strings.EqualFold(handle, "profile.php") {
			return Hit{}, false
		}
		home := "https://www.facebook.com/" + handle
		return makeHit(PlatformFacebook, handle, handle, home, title, snippet,
			"打开 Facebook 主页后点击 Message（需登录官方账号，系统不会代发私信）", 75), true
	}

	if m := linkedinInRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle, _ := url.PathUnescape(m[1])
		home := "https://www.linkedin.com/in/" + m[1]
		return makeHit(PlatformLinkedIn, "in:"+strings.ToLower(handle), handle, home, title, snippet,
			"打开 LinkedIn 主页后点击 Message（需登录官方账号，系统不会代发私信）", 85), true
	}

	if m := linkedinCoRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle, _ := url.PathUnescape(m[1])
		home := "https://www.linkedin.com/company/" + m[1]
		return makeHit(PlatformLinkedIn, "company:"+strings.ToLower(handle), handle, home, title, snippet,
			"打开 LinkedIn 公司页后通过官网/联系人沟通（系统不会代发私信）", 80), true
	}

	if m := xHandleRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle := m[1]
		if _, skip := reservedPaths[strings.ToLower(handle)]; skip {
			return Hit{}, false
		}
		home := "https://x.com/" + handle
		return makeHit(PlatformX, handle, handle, home, title, snippet,
			"打开 X 主页后点击 Message（需登录官方账号，系统不会代发私信）", 70), true
	}

	if m := pinterestRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle := m[1]
		if _, skip := reservedPaths[strings.ToLower(handle)]; skip {
			return Hit{}, false
		}
		home := "https://www.pinterest.com/" + handle + "/"
		return makeHit(PlatformPinterest, handle, handle, home, title, snippet,
			"Pinterest 无统一私信；请通过主页链接的官网/社媒联系（系统不会代发）", 55), true
	}

	if m := threadsRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle := m[1]
		if _, skip := reservedPaths[strings.ToLower(handle)]; skip {
			return Hit{}, false
		}
		home := "https://www.threads.net/@" + handle
		return makeHit(PlatformThreads, handle, handle, home, title, snippet,
			"打开 Threads 主页后点击 Message（需登录官方账号，系统不会代发私信）", 65), true
	}

	if m := xiaohongshuRe.FindStringSubmatch(decoded); len(m) == 2 {
		id := m[1]
		home := "https://www.xiaohongshu.com/user/profile/" + id
		return makeHit(PlatformXiaohongshu, id, id, home, title, snippet,
			"打开小红书主页后点击「私信」（需登录官方 App，系统不会代发）", 75), true
	}

	if m := kuaishouRe.FindStringSubmatch(decoded); len(m) == 2 {
		id := m[1]
		home := "https://www.kuaishou.com/profile/" + id
		return makeHit(PlatformKuaishou, id, id, home, title, snippet,
			"打开快手主页后点击「私信」（需登录官方 App，系统不会代发）", 70), true
	}

	if m := weiboUIDRe.FindStringSubmatch(decoded); len(m) == 2 {
		id := m[1]
		home := "https://weibo.com/u/" + id
		return makeHit(PlatformWeibo, "u:"+id, id, home, title, snippet,
			"打开微博主页后点击「私信」（需登录官方账号，系统不会代发）", 70), true
	}

	if m := weiboNameRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle, _ := url.PathUnescape(m[1])
		home := "https://weibo.com/n/" + m[1]
		return makeHit(PlatformWeibo, "n:"+strings.ToLower(handle), handle, home, title, snippet,
			"打开微博主页后点击「私信」（需登录官方账号，系统不会代发）", 65), true
	}

	if m := bilibiliRe.FindStringSubmatch(decoded); len(m) == 2 {
		id := m[1]
		home := "https://space.bilibili.com/" + id
		return makeHit(PlatformBilibili, id, id, home, title, snippet,
			"打开 B 站空间后通过「发消息」联系（需登录官方账号，系统不会代发）", 65), true
	}

	if m := telegramRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle := m[1]
		if _, skip := reservedPaths[strings.ToLower(handle)]; skip {
			return Hit{}, false
		}
		home := "https://t.me/" + handle
		return makeHit(PlatformTelegram, handle, handle, home, title, snippet,
			"打开 Telegram 主页后点击 Message（需登录官方账号，系统不会代发私信）", 70), true
	}

	if m := redditUserRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle := m[1]
		home := "https://www.reddit.com/user/" + handle
		return makeHit(PlatformReddit, handle, handle, home, title, snippet,
			"打开 Reddit 主页后点击 Chat（需登录官方账号，系统不会代发私信）", 55), true
	}

	if m := twitchRe.FindStringSubmatch(decoded); len(m) == 2 {
		handle := m[1]
		if _, skip := reservedPaths[strings.ToLower(handle)]; skip {
			return Hit{}, false
		}
		home := "https://www.twitch.tv/" + handle
		return makeHit(PlatformTwitch, handle, handle, home, title, snippet,
			"打开 Twitch 主页后点击 Whisper（需登录官方账号，系统不会代发私信）", 55), true
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
			MessageHint: "YouTube 无统一私信；请通过主页「关于」里的邮箱/社媒联系（系统不会代发）",
			Source:      "websearch",
			Score:       55,
		}, true
	}

	return Hit{}, false
}

func makeHit(platform, id, handle, home, title, snippet, hint string, score int) Hit {
	if handle == "" {
		handle = id
	}

	return Hit{
		ID:          platform + ":" + strings.ToLower(id),
		Kind:        KindPeople,
		Platform:    platform,
		Name:        displayName(title, handle),
		Handle:      handle,
		Title:       title,
		Snippet:     snippet,
		HomepageURL: home,
		MessageURL:  home,
		MessageHint: hint,
		Source:      "websearch",
		Score:       score,
	}
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

	if i := strings.LastIndex(title, " - 抖音"); i > 0 {
		rest := strings.TrimSpace(title[:i])
		if j := strings.LastIndex(rest, " - "); j >= 0 {
			if author := strings.TrimSpace(rest[j+3:]); author != "" && len([]rune(author)) <= 36 {
				title = author
			} else {
				title = rest
			}
		} else {
			title = rest
		}
		title = strings.TrimSpace(strings.TrimSuffix(title, "的抖音"))
	}

	title = strings.TrimSpace(strings.Split(title, "|")[0])
	title = strings.TrimSpace(strings.Split(title, " - ")[0])
	title = strings.TrimSpace(strings.Split(title, " | ")[0])
	if i := strings.LastIndex(title, "›"); i >= 0 {
		if rest := strings.TrimSpace(title[i+len("›"):]); rest != "" {
			title = rest
			if parts := strings.Fields(title); len(parts) >= 2 {
				first := strings.TrimPrefix(parts[0], "@")
				nicer := strings.Join(parts[1:], " ")
				if looksLikeHandle(first) && nicer != "" {
					title = nicer
				}
			}
		}
	}
	title = strings.TrimSpace(strings.TrimSuffix(title, "的抖音"))
	title = strings.TrimSpace(strings.TrimSuffix(title, "- 抖音"))
	title = strings.TrimSpace(strings.TrimSuffix(title, "– 抖音"))

	if i := strings.Index(title, "(@"); i > 0 {
		return strings.TrimSpace(title[:i])
	}

	if i := strings.Index(title, "（@"); i > 0 {
		return strings.TrimSpace(title[:i])
	}

	if i := strings.LastIndex(title, "@"); i > 0 {
		author := strings.TrimSpace(title[i+1:])
		if sp := strings.IndexAny(author, " 　|/"); sp > 0 {
			author = strings.TrimSpace(author[:sp])
		}
		if author != "" && len([]rune(author)) <= 24 && !looksLikeHandle(author) {
			return author
		}
	}

	if len([]rune(title)) > 28 || strings.Contains(title, "#") {
		return fallback
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

// isSocialHomepage reports whether the hit is a profile/page, not a video or note.
func isSocialHomepage(hit Hit) bool {
	u := strings.ToLower(strings.TrimSpace(hit.HomepageURL))
	if u == "" || isContentURL(u) {
		return false
	}

	switch hit.Platform {
	case PlatformDouyin:
		return strings.Contains(u, "/user/")
	case PlatformTikTok:
		return strings.Contains(u, "/@")
	case PlatformYouTube:
		return strings.Contains(u, "/@") || strings.Contains(u, "/channel/")
	case PlatformXiaohongshu:
		return strings.Contains(u, "/user/profile/")
	case PlatformKuaishou:
		return strings.Contains(u, "/profile/")
	case PlatformBilibili:
		return strings.Contains(u, "space.bilibili.com/")
	case PlatformWeibo:
		return strings.Contains(u, "weibo.com/u/") || strings.Contains(u, "weibo.com/n/")
	case PlatformLinkedIn:
		return strings.Contains(u, "/in/") || strings.Contains(u, "/company/")
	case PlatformInstagram, PlatformFacebook, PlatformX, PlatformTelegram,
		PlatformThreads, PlatformPinterest, PlatformReddit, PlatformTwitch:
		return true
	case PlatformWebsite:
		return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
	default:
		return false
	}
}
