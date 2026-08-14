package engine

import "time"

// Platform identifiers used by the intelligent engine search.
const (
	PlatformTikTok     = "tiktok"
	PlatformDouyin     = "douyin"
	PlatformInstagram  = "instagram"
	PlatformYouTube    = "youtube"
	PlatformExhibition = "exhibition"
	PlatformCustoms    = "customs"
)

// Kind selects which customer-discovery module to run.
const (
	KindPeople     = "people"
	KindExhibition = "exhibition"
	KindCustoms    = "customs"
)

// Query is a single customer-discovery search request.
type Query struct {
	Keyword   string   `json:"keyword"`
	Kind      string   `json:"kind"`
	Platforms []string `json:"platforms,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

// Hit is one discovered person, homepage, exhibition, or trade record.
type Hit struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Platform    string            `json:"platform"`
	Name        string            `json:"name"`
	Handle      string            `json:"handle,omitempty"`
	Title       string            `json:"title,omitempty"`
	Snippet     string            `json:"snippet,omitempty"`
	HomepageURL string            `json:"homepage_url,omitempty"`
	MessageURL  string            `json:"message_url,omitempty"`
	MessageHint string            `json:"message_hint,omitempty"`
	Source      string            `json:"source"`
	Score       int               `json:"score"`
	Extra       map[string]string `json:"extra,omitempty"`
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
}
