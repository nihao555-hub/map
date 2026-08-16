package engine

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

const defaultWebsiteSocialLimit = 30000

func (c *Client) ingestWebsiteSocials(ctx context.Context, dir *Directory, opt IngestOptions) IngestStats {
	started := time.Now()
	if c == nil || dir == nil {
		return IngestStats{Source: "website-socials", Took: time.Since(started), Note: "skipped"}
	}
	limit := opt.WebsiteLimit
	if limit == 0 {
		limit = defaultWebsiteSocialLimit
	}
	if limit < 0 {
		limit = 100000
	}
	workers := opt.WebsiteWorkers
	if workers <= 0 {
		workers = 16
	}
	rows, err := dir.ListHomepagesMissingSocials(ctx, limit)
	if err != nil {
		st := IngestStats{Source: "website-socials", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "website-socials", started, 0, st.Err)
		return st
	}
	var (
		tried    atomic.Int64
		attached atomic.Int64
		profiles atomic.Int64
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for i := range rows {
		row := rows[i]
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			tried.Add(1)
			got := c.scrapeOfficialSocials(gctx, row)
			if len(got) > 0 {
				if err := dir.UpsertProfiles(gctx, got); err != nil {
					return err
				}
				attached.Add(1)
				profiles.Add(int64(len(got)))
			}
			_ = dir.MarkEnriched(gctx, []string{row.ExtID})
			n := tried.Load()
			if n%400 == 0 {
				logIngest("website-socials %d/%d attached=%d profiles=%d", n, len(rows), attached.Load(), profiles.Load())
			}
			return nil
		})
	}
	waitErr := g.Wait()
	st := IngestStats{
		Source: "website-socials",
		Rows:   int(attached.Load()),
		Took:   time.Since(started),
		Note:   fmt.Sprintf("official sites missing socials, listed=%d tried=%d profiles=%d", len(rows), tried.Load(), profiles.Load()),
	}
	if waitErr != nil {
		st.Err = waitErr.Error()
	}
	_ = dir.RecordRun(ctx, "website-socials", started, st.Rows, st.Note)
	logIngest("website-socials attached %d / %d (profiles=%d)", st.Rows, len(rows), profiles.Load())
	return st
}

func (c *Client) scrapeOfficialSocials(ctx context.Context, row Merchant) []Profile {
	if c == nil || ctx.Err() != nil || strings.TrimSpace(row.ExtID) == "" {
		return nil
	}
	home := strings.TrimSpace(row.Homepage)
	if !isRealHomepage(home) {
		return nil
	}
	doc, err := c.fetchDocument(ctx, home)
	if err != nil || doc == nil || len(doc.Body) == 0 {
		return nil
	}
	return profilesFromOfficialHTML(row.ExtID, firstNonEmpty(doc.FinalURL, home), doc.Body)
}

func profilesFromOfficialHTML(extID, finalURL string, body []byte) []Profile {
	seen := map[string]struct{}{}
	var out []Profile
	add := func(p Profile) {
		if p.URL == "" || p.Platform == "" {
			return
		}
		p.ExtID = extID
		key := p.Platform + "|" + strings.ToLower(p.URL)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	if isRealHomepage(finalURL) {
		add(Profile{Platform: PlatformWebsite, URL: finalURL, Source: "website"})
	}
	for _, h := range extractProfilesFromHTML(body, "website") {
		if !isSocialHomepage(h) || h.Platform == PlatformWebsite {
			continue
		}
		add(Profile{
			Platform: h.Platform,
			URL:      h.HomepageURL,
			Handle:   h.Handle,
			Source:   "website",
		})
	}
	return out
}
