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

// ingestWaybackTikTok pulls unique tiktok.com/@ handles from the Internet
// Archive CDX index. This is the only public URL listing besides Wikidata;
// it is not TikTok's account database.
func (c *Client) ingestWaybackTikTok(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil {
		return IngestStats{Source: "wayback-tiktok", Took: time.Since(started), Note: "skipped"}
	}
	seen := map[string]struct{}{}
	inserted := 0
	pages := 0
	for _, prefix := range socialValuePrefixes() {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: "wayback-tiktok", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "wayback-tiktok", started, inserted, st.Err)
			return st
		}
		for page := 0; page < waybackTikTokMaxPages; page++ {
			raw, err := c.fetchWaybackTikTokPage(ctx, prefix, page)
			if err != nil {
				logIngest("wayback tiktok %s page=%d: %v", prefix, page, err)
				time.Sleep(2 * time.Second)
				raw, err = c.fetchWaybackTikTokPage(ctx, prefix, page)
			}
			if err != nil {
				break
			}
			pages++
			handles := parseWaybackTikTokHandles(raw)
			var rows []Merchant
			for _, handle := range handles {
				id := "tiktok:" + handle
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				home := "https://www.tiktok.com/@" + handle
				rows = append(rows, Merchant{
					ExtID:    id,
					Source:   "wayback",
					Name:     handle,
					Shop:     "tiktok",
					Homepage: home,
					Profiles: []Profile{{
						ExtID:    id,
						Platform: PlatformTikTok,
						URL:      home,
						Handle:   handle,
						Source:   "wayback-cdx",
					}},
				})
			}
			if len(rows) > 0 {
				n, err := dir.InsertBatch(ctx, rows)
				if err != nil {
					st := IngestStats{Source: "wayback-tiktok", Rows: inserted, Took: time.Since(started), Err: err.Error()}
					_ = dir.RecordRun(ctx, "wayback-tiktok", started, inserted, st.Err)
					return st
				}
				inserted += n
			}
			logIngest("wayback tiktok %s page=%d handles=%d inserted=%d unique=%d", prefix, page, len(handles), inserted, len(seen))
			if len(handles) < waybackTikTokPageLimit/4 {
				break
			}
			time.Sleep(1200 * time.Millisecond)
		}
	}
	st := IngestStats{
		Source: "wayback-tiktok",
		Rows:   inserted,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("pages=%d unique=%d", pages, len(seen)),
	}
	_ = dir.RecordRun(ctx, "wayback-tiktok", started, inserted, st.Note)
	logIngest("wayback tiktok done inserted=%d unique=%d in %s", inserted, len(seen), st.Took.Round(time.Millisecond))
	return st
}

func (c *Client) fetchWaybackTikTokPage(ctx context.Context, prefix string, page int) ([]byte, error) {
	q := url.Values{}
	q.Set("url", "www.tiktok.com/@"+prefix)
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
		"User-Agent": "map-engine/wayback-tiktok (https://github.com/nihao555-hub/map)",
		"Accept":     "application/json",
	})
}

func parseWaybackTikTokHandles(raw []byte) []string {
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
		handle, ok := waybackTikTokHandle(row[0])
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
