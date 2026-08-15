package engine

import (
	"strings"
	"time"
)

// Platform identifiers used by the intelligent engine search.
const (
	PlatformTikTok      = "tiktok"
	PlatformDouyin      = "douyin"
	PlatformInstagram   = "instagram"
	PlatformYouTube     = "youtube"
	PlatformFacebook    = "facebook"
	PlatformLinkedIn    = "linkedin"
	PlatformX           = "x"
	PlatformPinterest   = "pinterest"
	PlatformThreads     = "threads"
	PlatformXiaohongshu = "xiaohongshu"
	PlatformKuaishou    = "kuaishou"
	PlatformWeibo       = "weibo"
	PlatformBilibili    = "bilibili"
	PlatformTelegram    = "telegram"
	PlatformReddit      = "reddit"
	PlatformTwitch      = "twitch"
	PlatformExhibition  = "exhibition"
	PlatformCustoms     = "customs"
)

// DefaultPeoplePlatforms is the set used when the UI sends no platforms.
// All searchable networks start on; the user can uncheck chips.
var DefaultPeoplePlatforms = []string{
	PlatformTikTok,
	PlatformDouyin,
	PlatformFacebook,
	PlatformInstagram,
	PlatformYouTube,
	PlatformLinkedIn,
	PlatformXiaohongshu,
	PlatformKuaishou,
	PlatformWeibo,
	PlatformBilibili,
	PlatformX,
	PlatformPinterest,
	PlatformThreads,
	PlatformTelegram,
	PlatformReddit,
	PlatformTwitch,
}

// Kind selects which customer-discovery module to run.
const (
	KindPeople     = "people"
	KindMarketing  = "marketing"
	KindExhibition = "exhibition"
	KindCustoms    = "customs"

	ModeHomepage  = "homepage"
	ModeMarketing = "marketing"

	ChannelEmail    = "email"
	ChannelWhatsApp = "whatsapp"

	RoleBuyer  = "buyer"
	RoleSeller = "seller"
)

// Query is a single customer-discovery search request.
type Query struct {
	Keyword   string   `json:"keyword"`
	Kind      string   `json:"kind"`
	Platforms []string `json:"platforms,omitempty"`
	Limit     int      `json:"limit,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Channel   string   `json:"channel,omitempty"`
	Precise   bool     `json:"precise,omitempty"`
	Country   string   `json:"country,omitempty"`
	Role      string   `json:"role,omitempty"`
	Year      int      `json:"year,omitempty"`
}

// NormalizeRole maps UI input onto buyer (default) or seller search.
func NormalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleSeller, "sell", "supplier", "factory", "厂家", "卖家", "批发":
		return RoleSeller
	default:
		return RoleBuyer
	}
}

// Hit is one discovered person, homepage, exhibition, or trade record.
type Hit struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	Platform     string            `json:"platform"`
	Name         string            `json:"name"`
	Handle       string            `json:"handle,omitempty"`
	Title        string            `json:"title,omitempty"`
	Snippet      string            `json:"snippet,omitempty"`
	HomepageURL  string            `json:"homepage_url,omitempty"`
	MessageURL   string            `json:"message_url,omitempty"`
	MessageHint  string            `json:"message_hint,omitempty"`
	Contact      string            `json:"contact,omitempty"`
	Channel      string            `json:"channel,omitempty"`
	Source       string            `json:"source,omitempty"`
	Score        int               `json:"score"`
	Role         string            `json:"role,omitempty"`
	Country      string            `json:"country,omitempty"`
	CountryLabel string            `json:"country_label,omitempty"`
	Extra        map[string]string `json:"extra,omitempty"`
}

// Result is the API payload returned to the Web UI.
type Result struct {
	Keyword    string    `json:"keyword"`
	Kind       string    `json:"kind"`
	Hits       []Hit     `json:"hits"`
	Warnings   []string  `json:"warnings,omitempty"`
	Sources    []string  `json:"sources"`
	TookMS     int64     `json:"took_ms"`
	SearchedAt time.Time `json:"searched_at"`
	Note       string    `json:"note,omitempty"`
	Expanded   []string  `json:"expanded,omitempty"`
	Cached     bool      `json:"cached,omitempty"`
}
