package engine

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	defaultAttachSocialWorkers = 10
	attachSocialBatch          = 80
)

var attachSocialPhases = []string{"homepage", "osm", "wikidata", "other"}

func (c *Client) ingestAttachSocials(ctx context.Context, dir *Directory, opt IngestOptions) IngestStats {
	started := time.Now()
	if c == nil || dir == nil {
		return IngestStats{Source: "attach-socials", Took: time.Since(started), Note: "skipped"}
	}
	workers := opt.AttachWorkers
	if workers <= 0 {
		workers = defaultAttachSocialWorkers
	}
	limit := opt.AttachLimit
	already, err := dir.MarkProbedIfHasSocial(ctx)
	if err != nil {
		st := IngestStats{Source: "attach-socials", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "attach-socials", started, 0, st.Err)
		return st
	}
	pending, err := dir.CountMerchantsMissingSocials(ctx)
	if err != nil {
		st := IngestStats{Source: "attach-socials", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "attach-socials", started, 0, st.Err)
		return st
	}
	logIngest("attach-socials start pending=%d already-had-social=%d workers=%d limit=%d (0=all)", pending, already, workers, limit)

	var (
		tried    atomic.Int64
		attached atomic.Int64
		profiles atomic.Int64
	)
	stop := false
	for _, phase := range attachSocialPhases {
		if stop || ctx.Err() != nil {
			break
		}
		for ctx.Err() == nil && !stop {
			remain := 0
			if limit > 0 {
				remain = limit - int(tried.Load())
				if remain <= 0 {
					stop = true
					break
				}
			}
			batch := attachSocialBatch
			if phase != "other" {
				batch = 20000
			}
			if remain > 0 && remain < batch {
				batch = remain
			}
			rows, err := dir.ListMerchantsMissingSocials(ctx, batch, phase)
			if err != nil {
				st := IngestStats{Source: "attach-socials", Took: time.Since(started), Err: err.Error()}
				_ = dir.RecordRun(ctx, "attach-socials", started, int(attached.Load()), st.Err)
				return st
			}
			if len(rows) == 0 {
				break
			}
			g, gctx := errgroup.WithContext(ctx)
			g.SetLimit(workers)
			for i := range rows {
				row := rows[i]
				g.Go(func() error {
					if gctx.Err() != nil {
						return nil
					}
					tried.Add(1)
					got := c.collectMerchantSocials(gctx, row)
					if len(got) > 0 {
						if err := dir.UpsertProfiles(gctx, got); err != nil {
							return err
						}
						nSocial := 0
						for _, p := range got {
							if p.Platform != "" && p.Platform != PlatformWebsite {
								nSocial++
							}
						}
						if nSocial > 0 {
							attached.Add(1)
							profiles.Add(int64(nSocial))
						}
					}
					_ = dir.MarkProbed(gctx, []string{row.ExtID})
					if isRealHomepage(row.Homepage) {
						_ = dir.MarkEnriched(gctx, []string{row.ExtID})
					}
					n := tried.Load()
					if n%100 == 0 || n <= 20 {
						logIngest("attach-socials %s tried=%d attached=%d profiles=%d pending~%d",
							phase, n, attached.Load(), profiles.Load(), pending)
					}
					return nil
				})
			}
			if waitErr := g.Wait(); waitErr != nil {
				st := IngestStats{
					Source: "attach-socials",
					Rows:   int(attached.Load()),
					Took:   time.Since(started),
					Err:    waitErr.Error(),
					Note:   fmt.Sprintf("pending=%d tried=%d profiles=%d", pending, tried.Load(), profiles.Load()),
				}
				_ = dir.RecordRun(ctx, "attach-socials", started, st.Rows, st.Err)
				return st
			}
			if limit > 0 && int(tried.Load()) >= limit {
				stop = true
			}
		}
	}

	st := IngestStats{
		Source: "attach-socials",
		Rows:   int(attached.Load()),
		Took:   time.Since(started),
		Note:   fmt.Sprintf("missing-socials pending=%d tried=%d profiles=%d", pending, tried.Load(), profiles.Load()),
	}
	_ = dir.RecordRun(ctx, "attach-socials", started, st.Rows, st.Note)
	logIngest("attach-socials attached %d / tried %d (profiles=%d, listed=%d)", st.Rows, tried.Load(), profiles.Load(), pending)
	return st
}

func (c *Client) collectMerchantSocials(ctx context.Context, row Merchant) []Profile {
	if c == nil || ctx.Err() != nil || strings.TrimSpace(row.ExtID) == "" || strings.TrimSpace(row.Name) == "" {
		return nil
	}
	hit := merchantLookupHit(row)
	if isRealHomepage(row.Homepage) {
		for _, p := range c.scrapeOfficialSocials(ctx, row) {
			if p.Platform == "" || p.Platform == PlatformWebsite || strings.TrimSpace(p.URL) == "" {
				continue
			}
			hit.Profiles = append(hit.Profiles, Hit{
				Platform:    p.Platform,
				HomepageURL: p.URL,
				Handle:      p.Handle,
				Verified:    p.Verified,
				Extra:       map[string]string{"src": firstNonEmpty(p.Source, "website")},
			})
		}
	}
	return c.lookupMerchantSocials(ctx, hit, nil)
}

func merchantLookupHit(row Merchant) Hit {
	return Hit{
		Name:        row.Name,
		Title:       row.Name,
		Country:     strings.ToUpper(strings.TrimSpace(row.Country)),
		HomepageURL: row.Homepage,
		Extra: map[string]string{
			"src":    row.Source,
			"ext_id": row.ExtID,
			"city":   row.City,
			"shop":   row.Shop,
		},
	}
}
