package engine

import (
	"context"
	"fmt"
	"strings"
)

// ingestPublicShortVideoIndexes walks every public listing we can enumerate
// for TikTok / Douyin homepages. It cannot dump the platforms themselves.
func (c *Client) ingestPublicShortVideoIndexes(ctx context.Context, dir *Directory) (int, error) {
	if c == nil {
		return 0, fmt.Errorf("client required")
	}
	orig := c.WikidataURL
	if orig == "" || strings.Contains(orig, "query.wikidata.org") {
		c.WikidataURL = QleverWikidataSPARQL
	}
	defer func() { c.WikidataURL = orig }()

	steps := []struct {
		name string
		run  func() IngestStats
	}{
		{"wikidata-all", func() IngestStats { return c.ingestWikidataShortVideoAll(ctx, dir) }},
		{"wikidata-official", func() IngestStats { return c.ingestWikidataOfficialShortVideo(ctx, dir) }},
		{"wayback-tiktok", func() IngestStats { return c.ingestWaybackTikTok(ctx, dir) }},
		{"wayback-douyin", func() IngestStats { return c.ingestWaybackDouyin(ctx, dir) }},
		{"osm-tiktok", func() IngestStats { return c.ingestOSMTikTok(ctx, dir) }},
		{"commoncrawl", func() IngestStats { return c.ingestCommonCrawlShortVideo(ctx, dir) }},
	}
	var (
		failed []string
		total  int
	)
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		st := step.run()
		total += st.Rows
		logIngest("short-video public %s %s", step.name, st)
		if st.Err != "" {
			failed = append(failed, step.name+": "+st.Err)
		}
	}
	if len(failed) == len(steps) {
		return total, fmt.Errorf("public indexes failed: %s", strings.Join(failed, "; "))
	}
	return total, nil
}
