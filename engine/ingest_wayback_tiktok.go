package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
)

const (
	waybackCDXURL          = "https://web.archive.org/cdx/search/cdx"
	waybackTikTokPageLimit = 4000
	waybackTikTokMaxPages  = 8
)

type waybackSocialTarget struct {
	Source    string
	Platform  string
	Shop      string
	URLPrefix string
	Accept    func(raw string) (handle string, ok bool)
}

func waybackTikTokTargets() []waybackSocialTarget {
	return []waybackSocialTarget{{
		Source: "wayback-tiktok", Platform: PlatformTikTok, Shop: "tiktok",
		URLPrefix: "www.tiktok.com/@", Accept: waybackTikTokHandle,
	}}
}

// waybackDouyinShards skips the giant "m" bucket (MS4w…) and splits
// the common Douyin sec_uid prefix so CDX pagination is not truncated.
func waybackDouyinShards() []string {
	out := make([]string, 0, 128)
	for _, p := range socialValuePrefixes() {
		if p != "m" {
			out = append(out, p)
		}
	}
	const stem = "MS4wLjABAAAA"
	extra := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_-"
	for i := 0; i < len(extra); i++ {
		out = append(out, stem+extra[i:i+1])
	}
	return out
}

func waybackDouyinTargets() []waybackSocialTarget {
	return []waybackSocialTarget{{
		Source: "wayback-douyin", Platform: PlatformDouyin, Shop: "douyin",
		URLPrefix: "www.douyin.com/user/", Accept: waybackDouyinHandle,
	}}
}

// ingestWaybackTikTok pulls unique tiktok.com/@ handles from the Internet
// Archive CDX index. This is the only public URL listing besides Wikidata;
// it is not TikTok's account database.
func (c *Client) ingestWaybackTikTok(ctx context.Context, dir *Directory) IngestStats {
	return c.ingestWaybackSocial(ctx, dir, "wayback-tiktok", waybackTikTokTargets())
}

func (c *Client) ingestWaybackDouyin(ctx context.Context, dir *Directory) IngestStats {
	return c.ingestWaybackSocial(ctx, dir, "wayback-douyin", waybackDouyinTargets())
}

func (c *Client) ingestWaybackSocial(ctx context.Context, dir *Directory, source string, targets []waybackSocialTarget) IngestStats {
	started := time.Now()
	if c == nil {
		return IngestStats{Source: source, Took: time.Since(started), Note: "skipped"}
	}
	seen := map[string]struct{}{}
	inserted := 0
	pages := 0
	for _, target := range targets {
		prefixes := socialValuePrefixes()
		if target.Platform == PlatformDouyin {
			prefixes = waybackDouyinShards()
		}
		maxPages := waybackTikTokMaxPages
		if target.Platform == PlatformDouyin {
			maxPages = 16
		}
		for _, prefix := range prefixes {
			if err := ctx.Err(); err != nil {
				st := IngestStats{Source: source, Rows: inserted, Took: time.Since(started), Err: err.Error()}
				_ = dir.RecordRun(ctx, source, started, inserted, st.Err)
				return st
			}
			for page := 0; page < maxPages; page++ {
				raw, err := c.fetchWaybackSocialPage(ctx, target.URLPrefix+prefix, page)
				if err != nil {
					logIngest("wayback %s %s%s page=%d: %v", source, target.URLPrefix, prefix, page, err)
					time.Sleep(2 * time.Second)
					raw, err = c.fetchWaybackSocialPage(ctx, target.URLPrefix+prefix, page)
				}
				if err != nil {
					break
				}
				pages++
				handles := parseWaybackSocialHandles(raw, target.Accept)
				var rows []Merchant
				for _, handle := range handles {
					id := target.Platform + ":" + handle
					if _, ok := seen[id]; ok {
						continue
					}
					seen[id] = struct{}{}
					home := shortVideoHomepage(target.Platform, handle)
					rows = append(rows, Merchant{
						ExtID:    id,
						Source:   "wayback",
						Name:     handle,
						Shop:     target.Shop,
						Homepage: home,
						Profiles: []Profile{{
							ExtID:    id,
							Platform: target.Platform,
							URL:      home,
							Handle:   handle,
							Source:   "wayback-cdx",
						}},
					})
				}
				if len(rows) > 0 {
					n, err := dir.InsertBatch(ctx, rows)
					if err != nil {
						st := IngestStats{Source: source, Rows: inserted, Took: time.Since(started), Err: err.Error()}
						_ = dir.RecordRun(ctx, source, started, inserted, st.Err)
						return st
					}
					inserted += n
				}
				logIngest("wayback %s %s%s page=%d handles=%d inserted=%d unique=%d", source, target.URLPrefix, prefix, page, len(handles), inserted, len(seen))
				if len(handles) < waybackTikTokPageLimit/4 {
					break
				}
				time.Sleep(1200 * time.Millisecond)
			}
		}
	}
	st := IngestStats{
		Source: source,
		Rows:   inserted,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("pages=%d unique=%d", pages, len(seen)),
	}
	_ = dir.RecordRun(ctx, source, started, inserted, st.Note)
	logIngest("wayback %s done inserted=%d unique=%d in %s", source, inserted, len(seen), st.Took.Round(time.Millisecond))
	return st
}

func shortVideoHomepage(platform, handle string) string {
	if platform == PlatformDouyin {
		return "https://www.douyin.com/user/" + handle
	}
	return "https://www.tiktok.com/@" + handle
}

func (c *Client) fetchWaybackSocialPage(ctx context.Context, urlPrefix string, page int) ([]byte, error) {
	q := url.Values{}
	q.Set("url", urlPrefix)
	q.Set("matchType", "prefix")
	q.Set("output", "json")
	q.Set("fl", "original")
	q.Set("collapse", "urlkey")
	q.Set("filter", "statuscode:200")
	q.Set("limit", fmt.Sprintf("%d", waybackTikTokPageLimit))
	if page > 0 {
		q.Set("page", fmt.Sprintf("%d", page))
	}
	return c.get(ctx, waybackCDXURL+"?"+q.Encode(), map[string]string{
		"User-Agent": "map-engine/wayback-social (https://github.com/nihao555-hub/map)",
		"Accept":     "application/json",
	})
}

func (c *Client) fetchWaybackTikTokPage(ctx context.Context, prefix string, page int) ([]byte, error) {
	return c.fetchWaybackSocialPage(ctx, "www.tiktok.com/@"+prefix, page)
}

func parseWaybackTikTokHandles(raw []byte) []string {
	return parseWaybackSocialHandles(raw, waybackTikTokHandle)
}

func parseWaybackDouyinHandles(raw []byte) []string {
	return parseWaybackSocialHandles(raw, waybackDouyinHandle)
}

func parseWaybackSocialHandles(raw []byte, accept func(string) (string, bool)) []string {
	var rows [][]string
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for i, row := range rows {
		if i == 0 && len(row) > 0 && strings.EqualFold(row[0], "original") {
			continue
		}
		if len(row) == 0 {
			continue
		}
		handle, ok := accept(row[0])
		if !ok {
			continue
		}
		if _, dup := seen[handle]; dup {
			continue
		}
		seen[handle] = struct{}{}
		out = append(out, handle)
	}
	return out
}

func waybackTikTokHandle(raw string) (string, bool) {
	hit, ok := ParseSocialURL(raw, "", "")
	if !ok || hit.Platform != PlatformTikTok {
		return "", false
	}
	h := strings.ToLower(strings.TrimSpace(hit.Handle))
	if !validTikTokHandle(h) {
		return "", false
	}
	return h, true
}

func waybackDouyinHandle(raw string) (string, bool) {
	hit, ok := ParseSocialURL(raw, "", "")
	if !ok || hit.Platform != PlatformDouyin {
		return "", false
	}
	h := strings.TrimSpace(hit.Handle)
	if !validDouyinUserID(h) {
		return "", false
	}
	return h, true
}

func validTikTokHandle(h string) bool {
	if n := len([]rune(h)); n < 2 || n > 24 {
		return false
	}
	if strings.ContainsAny(h, `%!$'\"<>(){}[]`) || strings.Contains(h, "..") {
		return false
	}
	if strings.HasPrefix(h, ".") || strings.HasSuffix(h, ".") {
		return false
	}
	letters := 0
	for _, r := range h {
		if unicode.IsLetter(r) {
			letters++
			continue
		}
		if unicode.IsDigit(r) || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return letters > 0
}

func validDouyinUserID(h string) bool {
	if n := len(h); n < 6 || n > 80 {
		return false
	}
	if strings.ContainsAny(h, `%!$'\"<>(){}[]/?&=`) || strings.Contains(h, "..") {
		return false
	}
	letters := 0
	digits := 0
	for _, r := range h {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r):
			digits++
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return letters > 0 || digits >= 6
}
