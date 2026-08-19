package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Recent Common Crawl indexes. Platforms usually block crawlers, so this
// often inserts 0; it is still the public web crawl we can query.
var commonCrawlIndexes = []string{
	"https://index.commoncrawl.org/CC-MAIN-2026-30-index",
	"https://index.commoncrawl.org/CC-MAIN-2026-26-index",
	"https://index.commoncrawl.org/CC-MAIN-2026-21-index",
}

var commonCrawlPrefixes = []string{
	"www.tiktok.com/@",
	"www.douyin.com/user/",
}

const commonCrawlPageLimit = 1000

func (c *Client) ingestCommonCrawlShortVideo(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil {
		return IngestStats{Source: "commoncrawl-short-video", Took: time.Since(started), Note: "skipped"}
	}
	seen := map[string]struct{}{}
	inserted := 0
	pages := 0
	for _, index := range commonCrawlIndexes {
		for _, prefix := range commonCrawlPrefixes {
			if err := ctx.Err(); err != nil {
				st := IngestStats{Source: "commoncrawl-short-video", Rows: inserted, Took: time.Since(started), Err: err.Error()}
				_ = dir.RecordRun(ctx, "commoncrawl-short-video", started, inserted, st.Err)
				return st
			}
			raw, err := c.fetchCommonCrawlPage(ctx, index, prefix)
			if err != nil {
				logIngest("commoncrawl %s %s: %v", index, prefix, err)
				continue
			}
			pages++
			rows := commonCrawlMerchants(raw, seen)
			if len(rows) == 0 {
				continue
			}
			n, err := dir.InsertBatch(ctx, rows)
			if err != nil {
				st := IngestStats{Source: "commoncrawl-short-video", Rows: inserted, Took: time.Since(started), Err: err.Error()}
				_ = dir.RecordRun(ctx, "commoncrawl-short-video", started, inserted, st.Err)
				return st
			}
			inserted += n
			logIngest("commoncrawl %s %s +%d inserted=%d unique=%d", index, prefix, n, inserted, len(seen))
		}
	}
	st := IngestStats{
		Source: "commoncrawl-short-video",
		Rows:   inserted,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("pages=%d unique=%d", pages, len(seen)),
	}
	_ = dir.RecordRun(ctx, "commoncrawl-short-video", started, inserted, st.Note)
	return st
}

func (c *Client) fetchCommonCrawlPage(ctx context.Context, index, prefix string) ([]byte, error) {
	q := url.Values{}
	q.Set("url", prefix)
	q.Set("matchType", "prefix")
	q.Set("output", "json")
	q.Set("filter", "status:200")
	q.Set("limit", fmt.Sprintf("%d", commonCrawlPageLimit))
	return c.get(ctx, strings.TrimRight(index, "?")+"?"+q.Encode(), map[string]string{
		"User-Agent": "map-engine/commoncrawl (https://github.com/nihao555-hub/map)",
		"Accept":     "application/json",
	})
}

func commonCrawlMerchants(raw []byte, seen map[string]struct{}) []Merchant {
	var out []Merchant
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var row struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		hit, ok := ParseSocialURL(row.URL, "", "")
		if !ok || (hit.Platform != PlatformTikTok && hit.Platform != PlatformDouyin) {
			continue
		}
		handle := strings.TrimSpace(hit.Handle)
		if hit.Platform == PlatformTikTok {
			handle = strings.ToLower(handle)
			if !validTikTokHandle(handle) {
				continue
			}
		} else if !validDouyinUserID(handle) {
			continue
		}
		id := hit.Platform + ":" + handle
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		home := shortVideoHomepage(hit.Platform, handle)
		out = append(out, Merchant{
			ExtID:    id,
			Source:   "commoncrawl",
			Name:     handle,
			Shop:     hit.Platform,
			Homepage: home,
			Profiles: []Profile{{
				ExtID:    id,
				Platform: hit.Platform,
				URL:      home,
				Handle:   handle,
				Source:   "commoncrawl-cdx",
			}},
		})
	}
	return out
}
