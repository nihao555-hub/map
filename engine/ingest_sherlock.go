package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"
)

const (
	defaultSherlockLimit     = 15000
	defaultSherlockNameLimit = 8000
	defaultSherlockWorkers   = 12
)

func (c *Client) ingestSherlock(ctx context.Context, dir *Directory, opt IngestOptions) IngestStats {
	started := time.Now()
	if c == nil || dir == nil {
		return IngestStats{Source: "sherlock", Took: time.Since(started), Note: "skipped"}
	}
	templates, err := c.loadSherlockTemplates(ctx)
	if err != nil {
		st := IngestStats{Source: "sherlock", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "sherlock", started, 0, st.Err)
		return st
	}
	limit := opt.SherlockLimit
	if limit == 0 {
		limit = defaultSherlockLimit
	}
	workers := opt.SherlockWorkers
	if workers <= 0 {
		workers = defaultSherlockWorkers
	}
	trusted, err := dir.ListSherlockTargets(ctx, limit)
	if err != nil {
		st := IngestStats{Source: "sherlock", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "sherlock", started, 0, st.Err)
		return st
	}
	nameLimit := opt.SherlockNameLimit
	if nameLimit == 0 {
		nameLimit = defaultSherlockNameLimit
	}
	names, err := dir.ListDistinctiveGLEIF(ctx, nameLimit)
	if err != nil {
		st := IngestStats{Source: "sherlock", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "sherlock", started, 0, st.Err)
		return st
	}

	var (
		tried    atomic.Int64
		attached atomic.Int64
		profiles atomic.Int64
	)
	run := func(rows []Merchant, distinctive bool) error {
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(workers)
		for i := range rows {
			row := rows[i]
			g.Go(func() error {
				if gctx.Err() != nil {
					return nil
				}
				tried.Add(1)
				got := c.probeSherlockMerchant(gctx, row, templates, distinctive)
				if len(got) == 0 {
					return nil
				}
				if err := dir.UpsertProfiles(gctx, got); err != nil {
					return err
				}
				attached.Add(1)
				profiles.Add(int64(len(got)))
				n := tried.Load()
				if n%300 == 0 {
					logIngest("sherlock %d attached=%d profiles=%d", n, attached.Load(), profiles.Load())
				}
				return nil
			})
		}
		return g.Wait()
	}
	if err := run(trusted, false); err != nil {
		st := IngestStats{Source: "sherlock", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "sherlock", started, 0, st.Err)
		return st
	}
	if err := run(names, true); err != nil {
		st := IngestStats{Source: "sherlock", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "sherlock", started, 0, st.Err)
		return st
	}
	st := IngestStats{
		Source: "sherlock",
		Rows:   int(attached.Load()),
		Took:   time.Since(started),
		Note:   fmt.Sprintf("trusted=%d distinctive=%d templates=%d tried=%d profiles=%d", len(trusted), len(names), len(templates), tried.Load(), profiles.Load()),
	}
	_ = dir.RecordRun(ctx, "sherlock", started, st.Rows, st.Note)
	logIngest("sherlock attached %d (profiles=%d)", st.Rows, profiles.Load())
	return st
}

func (c *Client) loadSherlockTemplates(ctx context.Context) ([]sherlockTemplate, error) {
	fallback := parseSherlockSocials([]byte(`{}`))
	path := filepath.Join(os.TempDir(), "merchant-ingest", "sherlock-data.json")
	if err := downloadCachedURL(ctx, c.httpClient(), sherlockDataURL, path, 8<<10); err != nil {
		logIngest("sherlock catalog download fail, using built-in sister list: %v", err)
		return fallback, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fallback, nil
	}
	out := parseSherlockSocials(raw)
	if len(out) == 0 {
		return fallback, nil
	}
	return out, nil
}

func (c *Client) probeSherlockMerchant(ctx context.Context, row Merchant, templates []sherlockTemplate, distinctive bool) []Profile {
	handles := trustedSherlockHandles(row)
	needLI := false
	if distinctive {
		if h := distinctiveNameHandle(row.Name); h != "" {
			handles = append(handles, h)
			needLI = distinctiveNeedsLinkedIn(h)
		}
	}
	if len(handles) == 0 {
		return nil
	}
	have := map[string]struct{}{}
	for _, p := range row.Profiles {
		if p.Platform != "" && p.Platform != PlatformWebsite {
			have[p.Platform] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	var got []Profile
	for _, handle := range handles {
		for _, raw := range sherlockURLs(handle, templates) {
			if ctx.Err() != nil {
				break
			}
			hit, ok := ParseSocialURL(raw, row.Name, "")
			if !ok || !isSocialHomepage(hit) || hit.Platform == PlatformWebsite {
				continue
			}
			if _, ok := have[hit.Platform]; ok {
				continue
			}
			key := hit.Platform + "|" + strings.ToLower(hit.HomepageURL)
			if _, ok := seen[key]; ok {
				continue
			}
			if !c.probeProfileExists(ctx, hit.HomepageURL) {
				continue
			}
			seen[key] = struct{}{}
			got = append(got, Profile{
				ExtID:    row.ExtID,
				Platform: hit.Platform,
				URL:      hit.HomepageURL,
				Handle:   hit.Handle,
				Verified: true,
				Source:   "sherlock",
			})
		}
	}
	if distinctive {
		return filterSherlockHits(got, needLI)
	}
	return got
}

func (d *Directory) ListSherlockTargets(ctx context.Context, limit int) ([]Merchant, error) {
	if d == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT m.ext_id, m.source, m.name, m.shop, m.country, m.city, m.homepage, m.phone
		FROM merchants m
		WHERE (
		    (m.homepage != '' AND m.homepage NOT LIKE '%gleif.org%'
		      AND m.homepage NOT LIKE '%openstreetmap.org%'
		      AND m.homepage NOT LIKE '%wikidata.org%')
		    OR EXISTS (
		      SELECT 1 FROM merchant_profiles p
		      WHERE p.ext_id=m.ext_id AND p.platform != 'website'
		    )
		  )
		  AND (
		    SELECT COUNT(*) FROM merchant_profiles p
		    WHERE p.ext_id=m.ext_id AND p.platform != 'website'
		  ) < 5
		ORDER BY CASE m.source WHEN 'gleif' THEN 0 WHEN 'wikidata' THEN 1 ELSE 2 END, m.id
		LIMIT ?`
	rows, err := d.scanMerchants(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Merchant, 0, len(rows))
	for _, row := range d.attachProfiles(ctx, rows) {
		if len(trustedSherlockHandles(row)) == 0 {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (d *Directory) ListDistinctiveGLEIF(ctx context.Context, limit int) ([]Merchant, error) {
	if d == nil || limit <= 0 {
		return nil, nil
	}
	qrows, err := d.db.QueryContext(ctx, `SELECT ext_id, name, country FROM merchants
		WHERE source='gleif'
		  AND NOT EXISTS (SELECT 1 FROM merchant_profiles p WHERE p.ext_id=merchants.ext_id)`)
	if err != nil {
		return nil, err
	}
	defer qrows.Close()
	type slot struct {
		m        Merchant
		handle   string
		conflict bool
	}
	by := map[string]*slot{}
	for qrows.Next() {
		var extID, name, country string
		if err := qrows.Scan(&extID, &name, &country); err != nil {
			return nil, err
		}
		h := distinctiveNameHandle(name)
		if h == "" {
			continue
		}
		key := strings.ToLower(h)
		if cur, ok := by[key]; ok {
			cur.conflict = true
			continue
		}
		by[key] = &slot{
			handle: h,
			m:      Merchant{ExtID: extID, Source: "gleif", Name: name, Country: country},
		}
	}
	if err := qrows.Err(); err != nil {
		return nil, err
	}
	keep := make([]slot, 0, len(by))
	for _, s := range by {
		if s.conflict {
			continue
		}
		keep = append(keep, *s)
	}
	sort.Slice(keep, func(i, j int) bool {
		return utf8.RuneCountInString(keep[i].handle) > utf8.RuneCountInString(keep[j].handle)
	})
	if len(keep) > limit {
		keep = keep[:limit]
	}
	out := make([]Merchant, 0, len(keep))
	for _, s := range keep {
		out = append(out, s.m)
	}
	return out, nil
}
