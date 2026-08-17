package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

// EnrichOptions controls offline homepage probing and social extraction.
type EnrichOptions struct {
	DBPath  string
	Limit   int
	Workers int
}

// EnrichStats is one enrich run's timing.
type EnrichStats struct {
	Tried    int           `json:"tried"`
	Profiles int           `json:"profiles"`
	Verified int           `json:"verified"`
	Took     time.Duration `json:"took"`
	Err      string        `json:"error,omitempty"`
}

func (s EnrichStats) String() string {
	if s.Err != "" {
		return fmt.Sprintf("enrich: FAIL %s (%s)", s.Err, s.Took.Round(time.Millisecond))
	}
	return fmt.Sprintf("enrich: tried=%d profiles=%d verified=%d in %s", s.Tried, s.Profiles, s.Verified, s.Took.Round(time.Millisecond))
}

// EnrichMerchants fetches official sites for OSM shops and stores verified socials.
func (c *Client) EnrichMerchants(ctx context.Context, opt EnrichOptions) (EnrichStats, error) {
	started := time.Now()
	if opt.DBPath == "" {
		opt.DBPath = DefaultMerchantDB
	}
	if opt.Workers <= 0 {
		opt.Workers = 8
	}
	if opt.Limit <= 0 {
		opt.Limit = 20000
	}
	dir, err := OpenDirectory(opt.DBPath)
	if err != nil {
		return EnrichStats{Took: time.Since(started), Err: err.Error()}, err
	}
	defer dir.Close()

	seen := map[string]struct{}{}
	var rows []Merchant
	pending, err := dir.ListHomepagesMissingSocials(ctx, opt.Limit)
	if err != nil {
		return EnrichStats{Took: time.Since(started), Err: err.Error()}, err
	}
	for _, row := range pending {
		seen[row.ExtID] = struct{}{}
		rows = append(rows, row)
	}
	if len(rows) < opt.Limit {
		probes, err := dir.ListToProbe(ctx, opt.Limit-len(rows))
		if err != nil {
			return EnrichStats{Took: time.Since(started), Err: err.Error()}, err
		}
		for _, row := range probes {
			if _, ok := seen[row.ExtID]; ok {
				continue
			}
			seen[row.ExtID] = struct{}{}
			rows = append(rows, row)
		}
	}

	var (
		tried    atomic.Int64
		profiles atomic.Int64
		verified atomic.Int64
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opt.Workers)
	for i := range rows {
		row := rows[i]
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			tried.Add(1)
			got := c.enrichMerchant(gctx, row)
			if len(got) > 0 {
				if err := dir.UpsertProfiles(gctx, got); err != nil {
					return err
				}
				profiles.Add(int64(len(got)))
				for _, p := range got {
					if p.Verified {
						verified.Add(1)
					}
				}
			}
			_ = dir.MarkEnriched(gctx, []string{row.ExtID})
			_ = dir.MarkProbed(gctx, []string{row.ExtID})
			n := tried.Load()
			if n%200 == 0 {
				fmt.Fprintf(os.Stderr, "%s enrich %d/%d profiles=%d\n",
					time.Now().Format("15:04:05"), n, len(rows), profiles.Load())
			}
			return nil
		})
	}
	waitErr := g.Wait()
	if waitErr == nil {
		if n, err := c.reprobeUnverified(ctx, dir, 5000, opt.Workers); err != nil {
			waitErr = err
		} else {
			verified.Add(int64(n))
		}
	}
	st := EnrichStats{
		Tried:    int(tried.Load()),
		Profiles: int(profiles.Load()),
		Verified: int(verified.Load()),
		Took:     time.Since(started),
	}
	if waitErr != nil {
		st.Err = waitErr.Error()
	}
	_ = dir.RecordRun(ctx, "enrich", started, st.Verified, st.String())
	return st, waitErr
}

func (c *Client) reprobeUnverified(ctx context.Context, dir *Directory, limit, workers int) (int, error) {
	rows, err := dir.ListUnverifiedProfiles(ctx, limit)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	fmt.Fprintf(os.Stderr, "%s re-probe unverified=%d\n", time.Now().Format("15:04:05"), len(rows))
	var flipped atomic.Int64
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for i := range rows {
		p := rows[i]
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			if !c.probeProfileExists(gctx, p.URL) {
				return nil
			}
			p.Verified = true
			p.Source = "reprobe"
			if err := dir.UpsertProfiles(gctx, []Profile{p}); err != nil {
				return err
			}
			flipped.Add(1)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return int(flipped.Load()), err
	}
	n := int(flipped.Load())
	fmt.Fprintf(os.Stderr, "%s re-probe flipped_verified=%d\n", time.Now().Format("15:04:05"), n)
	return n, nil
}

func (c *Client) enrichHits(ctx context.Context, hits []Hit, limit int) []Hit {
	if c == nil || c.DisablePublic || limit <= 0 || len(hits) == 0 {
		return hits
	}
	dir := c.directory()
	if dir == nil {
		return hits
	}
	budget, cancel := context.WithTimeout(ctx, 16*time.Second)
	defer cancel()

	seen := map[string]struct{}{}
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(budget)
	g.SetLimit(4)
	n := 0
	for _, hit := range hits {
		extID := merchantExtID(hit)
		if extID == "" || !strings.HasPrefix(extID, "osm:") {
			continue
		}
		if registryOnlyHomepage(hit.HomepageURL) || strings.Contains(strings.ToLower(hit.HomepageURL), "openstreetmap.org") {
			continue
		}
		if len(hit.Profiles) >= 2 {
			continue
		}
		if _, ok := seen[extID]; ok {
			continue
		}
		seen[extID] = struct{}{}
		n++
		if n > limit {
			break
		}
		row := hitToMerchant(hit, extID)
		g.Go(func() error {
			got := c.enrichMerchant(gctx, row)
			if len(got) == 0 {
				return nil
			}
			_ = dir.UpsertProfiles(gctx, got)
			_ = dir.MarkEnriched(gctx, []string{extID})
			mu.Lock()
			for i := range hits {
				if merchantExtID(hits[i]) != extID {
					continue
				}
				hits[i] = attachProfilesToHit(hits[i], got)
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return hits
}

func (c *Client) persistHitProfiles(ctx context.Context, hits []Hit) {
	dir := c.directory()
	if dir == nil || len(hits) == 0 {
		return
	}
	var rows []Profile
	for _, hit := range hits {
		extID := merchantExtID(hit)
		if extID == "" {
			continue
		}
		if hit.HomepageURL != "" && hit.Platform != "" && isSocialHomepage(hit) {
			rows = append(rows, Profile{
				ExtID:    extID,
				Platform: hit.Platform,
				URL:      hit.HomepageURL,
				Handle:   hit.Handle,
				Verified: hit.Verified,
				Source:   "search",
			})
		}
		for _, p := range hit.Profiles {
			if p.HomepageURL == "" || p.Platform == "" {
				continue
			}
			rows = append(rows, Profile{
				ExtID:    extID,
				Platform: p.Platform,
				URL:      p.HomepageURL,
				Handle:   p.Handle,
				Verified: p.Verified,
				Source:   "search",
			})
		}
	}
	_ = dir.UpsertProfiles(ctx, rows)
}

func (c *Client) enrichMerchant(ctx context.Context, row Merchant) []Profile {
	if ctx.Err() != nil || strings.TrimSpace(row.ExtID) == "" {
		return nil
	}
	seen := map[string]struct{}{}
	var out []Profile
	add := func(p Profile) {
		if p.URL == "" || p.Platform == "" {
			return
		}
		p.ExtID = row.ExtID
		key := p.Platform + "|" + strings.ToLower(p.URL)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	for _, p := range row.Profiles {
		if p.Verified {
			add(p)
			continue
		}
		if c.probeProfileExists(ctx, p.URL) {
			p.Verified = true
			p.Source = firstNonEmpty(p.Source, "probe")
			add(p)
		}
	}
	home := strings.TrimSpace(row.Homepage)
	hasWebsite := false
	for _, p := range row.Profiles {
		if p.Platform == PlatformWebsite && p.Verified {
			hasWebsite = true
			break
		}
	}
	if isRealHomepage(home) && !hasWebsite {
		if social, ok := ParseSocialURL(home, row.Name, ""); ok {
			if c.probeProfileExists(ctx, social.HomepageURL) {
				add(Profile{Platform: social.Platform, URL: social.HomepageURL, Handle: social.Handle, Verified: true, Source: "homepage"})
			}
		} else if doc, err := c.fetchDocument(ctx, home); err == nil && doc != nil && !profileLooksGone("", doc.Body) {
			add(Profile{Platform: PlatformWebsite, URL: firstNonEmpty(doc.FinalURL, home), Verified: true, Source: "website"})
			for _, h := range extractProfilesFromHTML(doc.Body, "enrich") {
				if !isSocialHomepage(h) || h.Platform == PlatformWebsite {
					continue
				}
				if c.probeProfileExists(ctx, h.HomepageURL) {
					add(Profile{Platform: h.Platform, URL: h.HomepageURL, Handle: h.Handle, Verified: true, Source: "website"})
				}
			}
		}
	}
	for _, handle := range merchantProbeHandles(row) {
		for i, raw := range sameHandleURLs(handle) {
			if i >= maxHandleProbePlat || ctx.Err() != nil {
				break
			}
			h, ok := ParseSocialURL(raw, row.Name, "")
			if !ok {
				continue
			}
			key := h.Platform + "|" + strings.ToLower(h.HomepageURL)
			if _, ok := seen[key]; ok {
				continue
			}
			if c.probeProfileExists(ctx, h.HomepageURL) {
				add(Profile{Platform: h.Platform, URL: h.HomepageURL, Handle: h.Handle, Verified: true, Source: "probe"})
			}
		}
	}
	return out
}

func merchantExtID(hit Hit) string {
	if hit.Extra != nil {
		if id := strings.TrimSpace(hit.Extra["ext_id"]); id != "" {
			return id
		}
	}
	id := strings.TrimSpace(hit.ID)
	for _, prefix := range []string{"osm:", "gleif:", "wd:"} {
		if strings.HasPrefix(id, prefix) {
			return strings.TrimPrefix(id, prefix)
		}
	}
	return ""
}

func hitToMerchant(hit Hit, extID string) Merchant {
	src := ""
	shop := ""
	if hit.Extra != nil {
		src = hit.Extra["src"]
		shop = hit.Extra["shop"]
	}
	row := Merchant{
		ExtID:    extID,
		Source:   firstNonEmpty(src, "osm"),
		Name:     hit.Name,
		Shop:     shop,
		Country:  hit.Country,
		Homepage: hit.HomepageURL,
	}
	if hit.HomepageURL != "" && hit.Platform != "" {
		row.Profiles = append(row.Profiles, Profile{
			ExtID:    extID,
			Platform: hit.Platform,
			URL:      hit.HomepageURL,
			Handle:   hit.Handle,
			Verified: hit.Verified,
		})
	}
	for _, p := range hit.Profiles {
		row.Profiles = append(row.Profiles, Profile{
			ExtID:    extID,
			Platform: p.Platform,
			URL:      p.HomepageURL,
			Handle:   p.Handle,
			Verified: p.Verified,
		})
	}
	return row
}

func attachProfilesToHit(hit Hit, rows []Profile) Hit {
	for _, p := range rows {
		if p.URL == "" {
			continue
		}
		if strings.EqualFold(p.URL, hit.HomepageURL) {
			hit.Verified = hit.Verified || p.Verified
			continue
		}
		dup := false
		for _, have := range hit.Profiles {
			if strings.EqualFold(have.HomepageURL, p.URL) || have.Platform == p.Platform && p.Platform != PlatformWebsite {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		h := Hit{
			Platform:    p.Platform,
			Name:        hit.Name,
			Handle:      p.Handle,
			HomepageURL: p.URL,
			MessageURL:  p.URL,
			Verified:    p.Verified,
			Extra:       map[string]string{"via": hit.HomepageURL, "ext_id": p.ExtID},
		}
		hit.Profiles = append(hit.Profiles, h)
		if p.Verified {
			hit.Score += 6
		}
	}
	return hit
}
