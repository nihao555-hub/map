package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	defaultYellowPageWorkers = 32
	defaultYellowPageLimit   = 8000
)

// HarvestYellowPages is the directory → website/phone → socials pipeline.
// 1) public yellow-page indexes for listing URLs
// 2) fetch listings for name / phone / official site
// 3) scrape those sites (and existing OSM/Wikidata homepages) for socials
func (c *Client) HarvestYellowPages(ctx context.Context, opt HarvestOptions) (HarvestStats, error) {
	started := time.Now()
	if opt.DBPath == "" {
		opt.DBPath = ResolveMerchantDB("")
	}
	if len(opt.Keywords) == 0 {
		opt.Keywords = append([]string{}, DefaultHarvestKeywords...)
	}
	if len(opt.Countries) == 0 {
		opt.Countries = []string{"ID", "TH", "MY", "VN", "SG", "PH"}
	}
	if opt.Workers <= 0 {
		opt.Workers = defaultYellowPageWorkers
	}
	if opt.Deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opt.Deadline)
		defer cancel()
	}
	dir, err := OpenDirectory(opt.DBPath)
	if err != nil {
		return HarvestStats{Took: time.Since(started), Err: err.Error()}, err
	}
	defer dir.Close()

	queries := yellowPageQueries(opt.Keywords, opt.Countries)
	if opt.QueryLimit > 0 && len(queries) > opt.QueryLimit {
		queries = queries[:opt.QueryLimit]
	}
	logIngest("yellow-pages discover queries=%d workers=%d", len(queries), opt.Workers)
	listingURLs, nq := c.discoverYellowPageURLs(ctx, queries, minInt(opt.Workers, 8))
	logIngest("yellow-pages listings=%d from %d queries", len(listingURLs), nq)

	rows := c.fetchYellowPageListings(ctx, listingURLs, opt.Workers)
	inserted, err := dir.InsertBatch(ctx, rows)
	if err != nil {
		st := HarvestStats{Queries: nq, Hits: len(rows), Inserted: inserted, Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "yellow-pages", started, inserted, st.Err)
		return st, err
	}
	logIngest("yellow-pages inserted %d / %d listings", inserted, len(rows))

	scraped, profiles, phones := c.scrapeDirectoryHomepages(ctx, dir, opt.Workers, defaultYellowPageLimit)
	st := HarvestStats{
		Keywords: len(opt.Keywords),
		Queries:  nq,
		Hits:     len(rows),
		Inserted: inserted,
		Profiles: profiles,
		ByPlat:   map[string]int{"yellowpages": inserted, "website-scrape": scraped},
		Took:     time.Since(started),
	}
	_ = dir.RecordRun(ctx, "yellow-pages", started, inserted,
		fmt.Sprintf("listings=%d inserted=%d scraped=%d profiles=%d phones=%d", len(rows), inserted, scraped, profiles, phones))
	logIngest("yellow-pages done listings=%d inserted=%d scraped=%d profiles=%d phones=%d in %s",
		len(rows), inserted, scraped, profiles, phones, st.Took.Round(time.Millisecond))
	return st, nil
}

func (c *Client) discoverYellowPageURLs(ctx context.Context, queries []string, workers int) ([]string, int) {
	if workers <= 0 {
		workers = 8
	}
	var (
		mu   sync.Mutex
		seen = map[string]struct{}{}
		out  []string
		nq   int
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for _, q := range queries {
		q := q
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			raw, _, err := c.fetchIndexHTML(gctx, q, []string{"duckduckgo", "bing", "brave"})
			mu.Lock()
			nq++
			mu.Unlock()
			if err != nil || len(raw) == 0 {
				return nil
			}
			urls := extractYellowPageURLs(raw)
			mu.Lock()
			for _, u := range urls {
				if _, ok := seen[u]; ok {
					continue
				}
				seen[u] = struct{}{}
				out = append(out, u)
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return out, nq
}

func (c *Client) fetchYellowPageListings(ctx context.Context, urls []string, workers int) []Merchant {
	if workers <= 0 {
		workers = defaultYellowPageWorkers
	}
	var (
		mu  sync.Mutex
		out []Merchant
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for _, raw := range urls {
		raw := raw
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			doc, err := c.fetchDocument(gctx, raw)
			if err != nil || doc == nil || len(doc.Body) == 0 {
				return nil
			}
			row := parseYellowPageListing(firstNonEmpty(doc.FinalURL, raw), doc.Body)
			if strings.TrimSpace(row.Name) == "" || strings.TrimSpace(row.ExtID) == "" {
				return nil
			}
			mu.Lock()
			out = append(out, row)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return out
}

func (c *Client) scrapeDirectoryHomepages(ctx context.Context, dir *Directory, workers, limit int) (scraped, profiles, phones int) {
	if workers <= 0 {
		workers = defaultYellowPageWorkers
	}
	if limit <= 0 {
		limit = defaultYellowPageLimit
	}
	rows, err := dir.ListHomepagesMissingSocials(ctx, limit)
	if err != nil {
		logIngest("yellow-pages scrape list: %v", err)
		return 0, 0, 0
	}
	var (
		gotSites  atomic.Int64
		gotProfs  atomic.Int64
		gotPhones atomic.Int64
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for i := range rows {
		row := rows[i]
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			phone, profs := c.scrapeOfficialSite(gctx, row)
			if len(profs) > 0 {
				if err := dir.UpsertProfiles(gctx, profs); err != nil {
					return err
				}
				gotSites.Add(1)
				gotProfs.Add(int64(len(profs)))
			}
			if phone != "" {
				_ = dir.SetPhone(gctx, row.ExtID, phone)
				gotPhones.Add(1)
			}
			_ = dir.MarkEnriched(gctx, []string{row.ExtID})
			n := gotSites.Load() + gotPhones.Load()
			if n > 0 && n%200 == 0 {
				logIngest("yellow-pages scrape sites=%d profiles=%d phones=%d / %d",
					gotSites.Load(), gotProfs.Load(), gotPhones.Load(), len(rows))
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		logIngest("yellow-pages scrape: %v", err)
	}
	return int(gotSites.Load()), int(gotProfs.Load()), int(gotPhones.Load())
}

func (c *Client) scrapeOfficialSite(ctx context.Context, row Merchant) (string, []Profile) {
	if c == nil || ctx.Err() != nil || strings.TrimSpace(row.ExtID) == "" {
		return "", nil
	}
	home := strings.TrimSpace(row.Homepage)
	if !isRealHomepage(home) {
		return "", nil
	}
	doc, err := c.fetchDocument(ctx, home)
	if err != nil || doc == nil || len(doc.Body) == 0 {
		return "", nil
	}
	final := firstNonEmpty(doc.FinalURL, home)
	profs := profilesFromOfficialHTML(row.ExtID, final, doc.Body)
	phone := firstPhoneFromHTML(nil, string(doc.Body))
	if extra := contactPageURL(final); extra != "" {
		if more, err := c.fetchDocument(ctx, extra); err == nil && more != nil && len(more.Body) > 0 {
			profs = append(profs, profilesFromOfficialHTML(row.ExtID, more.FinalURL, more.Body)...)
			if phone == "" {
				phone = firstPhoneFromHTML(nil, string(more.Body))
			}
		}
	}
	seen := map[string]struct{}{}
	var uniq []Profile
	for _, p := range profs {
		key := p.Platform + "|" + strings.ToLower(p.URL)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniq = append(uniq, p)
	}
	return phone, uniq
}
