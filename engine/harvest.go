package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// DefaultHarvestKeywords is a 外贸品类清单 used by batch dork harvest.
var DefaultHarvestKeywords = []string{
	"LED灯", "家具", "电动工具", "服装", "鞋子", "化妆品",
	"包装", "阀门", "太阳能", "汽车配件", "塑料制品", "食品",
}

// DefaultHarvestCountries are markets we fan the same formulas across.
var DefaultHarvestCountries = []string{"", "TH", "MY", "VN", "US", "DE"}

// HarvestOptions controls a batch Google-dork sweep for social homepages.
type HarvestOptions struct {
	DBPath     string
	Keywords   []string
	Countries  []string
	Role       string
	Workers    int
	QueryLimit int
	// Fast seeds Wikidata TikTok/抖音全量标识，再按东南亚→中东→欧美扫。
	Fast bool
	// Regions is sea, me, west. Empty uses the default order when Fast.
	Regions []string
	// Deadline stops a fast harvest so a 2-hour window can finish.
	Deadline time.Duration
	// SeedOnly stops after the Wikidata TikTok/抖音 dump.
	SeedOnly bool
	// Sidecar uses TikTok-Api / f2 keyword search and skips public dorks.
	Sidecar bool
}

// HarvestStats is one batch run's timing and unique social pages.
type HarvestStats struct {
	Keywords int            `json:"keywords"`
	Queries  int            `json:"queries"`
	Hits     int            `json:"hits"`
	Inserted int            `json:"inserted"`
	Profiles int            `json:"profiles"`
	ByPlat   map[string]int `json:"by_platform,omitempty"`
	Took     time.Duration  `json:"took"`
	Err      string         `json:"error,omitempty"`
}

func (s HarvestStats) String() string {
	if s.Err != "" {
		return fmt.Sprintf("harvest: FAIL %s (%s)", s.Err, s.Took.Round(time.Millisecond))
	}
	return fmt.Sprintf("harvest: keywords=%d queries=%d hits=%d inserted=%d profiles=%d in %s",
		s.Keywords, s.Queries, s.Hits, s.Inserted, s.Profiles, s.Took.Round(time.Millisecond))
}

func harvestWanted() map[string]bool {
	return map[string]bool{
		PlatformFacebook:  true,
		PlatformInstagram: true,
		PlatformLinkedIn:  true,
		PlatformTikTok:    true,
		PlatformYouTube:   true,
		PlatformX:         true,
	}
}

// HarvestTradeDorks runs 外贸 Google 公式 against public indexes and stores
// unique social homepages as directory merchants (source=dork).
func (c *Client) HarvestTradeDorks(ctx context.Context, opt HarvestOptions) (HarvestStats, error) {
	started := time.Now()
	if opt.DBPath == "" {
		opt.DBPath = ResolveMerchantDB("")
	}
	if len(opt.Keywords) == 0 {
		opt.Keywords = append([]string{}, DefaultHarvestKeywords...)
	}
	if len(opt.Countries) == 0 {
		opt.Countries = append([]string{}, DefaultHarvestCountries...)
	}
	if opt.Workers <= 0 {
		opt.Workers = 8
	}
	if opt.Role == "" {
		opt.Role = RoleBuyer
	}
	dir, err := OpenDirectory(opt.DBPath)
	if err != nil {
		return HarvestStats{Took: time.Since(started), Err: err.Error()}, err
	}
	defer dir.Close()

	wanted := harvestWanted()
	var (
		mu      sync.Mutex
		seen    = map[string]Hit{}
		queries int
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opt.Workers)
	for _, kw := range opt.Keywords {
		kw := strings.TrimSpace(kw)
		if kw == "" {
			continue
		}
		for _, cc := range opt.Countries {
			cc := cc
			g.Go(func() error {
				if gctx.Err() != nil {
					return nil
				}
				terms := harvestDorkTerms(kw, cc)
				dorks := tradeGuruQueries(terms, wanted, cc, opt.Role)
				if opt.QueryLimit > 0 && len(dorks) > opt.QueryLimit {
					dorks = dorks[:opt.QueryLimit]
				}
				batch, nq := c.runTradeDorks(gctx, dorks, wanted)
				mu.Lock()
				queries += nq
				for _, h := range batch {
					id := dorkExtID(h)
					if id == "" {
						continue
					}
					if prev, ok := seen[id]; !ok || (h.Name != "" && (prev.Name == "" || prev.Name == prev.Handle)) {
						if h.Extra == nil {
							h.Extra = map[string]string{}
						}
						h.Extra["ext_id"] = id
						h.Extra["src"] = "dork"
						h.Extra["shop"] = harvestShop(kw)
						if h.Country == "" {
							h.Country = strings.ToUpper(cc)
						}
						seen[id] = h
					}
				}
				mu.Unlock()
				logIngest("dork %s %s +%d (unique=%d)", kw, firstNonEmpty(cc, "ALL"), len(batch), len(seen))
				return nil
			})
		}
	}
	_ = g.Wait()

	rows := make([]Merchant, 0, len(seen))
	byPlat := map[string]int{}
	for _, h := range seen {
		rows = append(rows, hitToMerchant(h, dorkExtID(h)))
		byPlat[h.Platform]++
	}
	inserted, err := dir.InsertBatch(ctx, rows)
	st := HarvestStats{
		Keywords: len(opt.Keywords),
		Queries:  queries,
		Hits:     len(seen),
		Inserted: inserted,
		Profiles: len(rows),
		ByPlat:   byPlat,
		Took:     time.Since(started),
	}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, "dork-harvest", started, inserted, st.String())
	return st, err
}

func (c *Client) runTradeDorks(ctx context.Context, dorks []publicQuery, wanted map[string]bool) ([]Hit, int) {
	if len(dorks) == 0 {
		return nil, 0
	}
	var (
		mu   sync.Mutex
		hits []Hit
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(publicSearchLimit)
	for _, pq := range dorks {
		pq := pq
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			batch, _, err := c.searchDorkFirstPage(gctx, pq.query, pq.platform)
			if err != nil {
				return nil
			}
			mu.Lock()
			for _, h := range batch {
				if pq.platform != "" && h.Platform != pq.platform {
					continue
				}
				if len(wanted) > 0 && !wanted[h.Platform] {
					continue
				}
				if !isSocialHomepage(h) || !keepDorkHit(h) {
					continue
				}
				hits = append(hits, h)
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return hits, len(dorks)
}

func (c *Client) searchDorkFirstPage(ctx context.Context, query, platform string) ([]Hit, string, error) {
	attempts := c.waitReadyIndexes(ctx, []string{"google", "bing", "duckduckgo", "brave"})
	var (
		merged []Hit
		srcs   []string
	)
	for _, a := range attempts {
		if uniqueHitCount(merged) >= 30 {
			break
		}
		if err := waitNamedIndex(ctx, a.name); err != nil {
			return merged, strings.Join(uniqueStrings(srcs), "+"), err
		}
		raw, err := a.fn(ctx, query)
		if err != nil {
			if isRateLimitedErr(err) {
				c.markIndexLimited(a.name)
			}
			continue
		}
		if looksLikeChallenge(raw) {
			c.markIndexLimited(a.name)
			continue
		}
		hits := filterHitsPlatform(extractProfilesFromHTML(raw, a.name), platform)
		if len(hits) == 0 {
			continue
		}
		before := uniqueHitCount(merged)
		merged = append(merged, hits...)
		if uniqueHitCount(merged) > before {
			srcs = append(srcs, a.name)
		}
	}
	return merged, strings.Join(uniqueStrings(srcs), "+"), nil
}

func dorkExtID(hit Hit) string {
	plat := strings.ToLower(strings.TrimSpace(hit.Platform))
	handle := strings.ToLower(strings.TrimSpace(hit.Handle))
	if plat == "" {
		return ""
	}
	if handle != "" {
		return "dork:" + plat + ":" + handle
	}
	home := strings.ToLower(strings.TrimSpace(hit.HomepageURL))
	if home == "" {
		return ""
	}
	return "dork:" + plat + ":" + strings.Trim(strings.NewReplacer("https://", "", "http://", "", "www.", "").Replace(home), "/")
}

func keepDorkHit(hit Hit) bool {
	name := strings.ToLower(strings.TrimSpace(hit.Name))
	handle := strings.ToLower(strings.TrimSpace(hit.Handle))
	if handle == "" {
		return false
	}
	if _, skip := reservedPaths[handle]; skip {
		return false
	}
	if _, skip := reservedPaths[name]; skip {
		return false
	}
	switch name {
	case "facebook", "instagram", "linkedin", "youtube", "tiktok", "videos", "watch", "home", "about":
		return false
	}
	if hit.Platform == PlatformYouTube && strings.Contains(strings.ToLower(hit.HomepageURL), "/channel/") &&
		(strings.Contains(name, "youtube") || name == "") {
		return false
	}
	return true
}

func harvestDorkTerms(keyword, country string) []string {
	raw := uniqueFoldedStrings(append([]string{keyword}, LocalSearchTerms(keyword, country)...))
	var en, other []string
	for _, t := range raw {
		if hasCJK(t) {
			other = append(other, t)
		} else {
			en = append(en, t)
		}
	}
	// English first: Facebook/LinkedIn SERPs rank Latin product names.
	return clipTerms(append(en, other...), 3)
}

func harvestShop(keyword string) string {
	if tags := shopTagsForKeyword(keyword); len(tags) > 0 {
		return tags[0]
	}
	return strings.ToLower(strings.TrimSpace(keyword))
}
