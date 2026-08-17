package engine

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

var nameDorkNeedles = []string{
	"lighting", "furniture", "led", "lamp", "footwear", "apparel",
	"textile", "cosmetic", "packaging", "valve", "solar", "hardware",
	"garment", "shoes", "plastics", "power tool",
}

var nameDorkSkip = []string{
	"pension", "trust fund", "commingled", "sicav", "etf",
	"limited partnership", "power and light", "revisionsanpart",
}

var nameDorkMarkets = map[string]int{
	"TH": 100, "VN": 100, "MY": 100, "ID": 100, "SG": 100, "PH": 100,
	"KH": 90, "LA": 90, "MM": 90, "BN": 90,
	"DE": 70, "NL": 70, "IT": 65, "ES": 60, "GB": 65, "FR": 60,
	"BE": 55, "AT": 55, "CH": 55, "PL": 50,
	"US": 45, "CA": 40, "AU": 40, "AE": 50, "SA": 45, "IN": 20,
}

func nameDorkCandidate(row Merchant) bool {
	name := strings.TrimSpace(row.Name)
	if name == "" {
		return false
	}
	low := strings.ToLower(name)
	for _, skip := range nameDorkSkip {
		if strings.Contains(low, skip) {
			return false
		}
	}
	if strings.Contains(low, " fund") || strings.HasSuffix(low, " fund") {
		return false
	}
	if _, ok := nameDorkMarkets[strings.ToUpper(strings.TrimSpace(row.Country))]; !ok {
		return false
	}
	folded := foldLegalName(name)
	if len(folded) < 10 || len(strings.Fields(folded)) < 2 {
		return false
	}
	if !hasNameDorkToken(folded) {
		return false
	}
	return nameCountryKey(name, row.Country) != ""
}

func hasNameDorkToken(folded string) bool {
	f := " " + strings.ToLower(folded) + " "
	for _, tok := range []string{
		" lighting ", " furniture ", " led ", " lamp ", " lamps ",
		" footwear ", " apparel ", " textile ", " cosmetic ", " cosmetics ",
		" packaging ", " valve ", " valves ", " solar ", " hardware ",
		" garment ", " shoes ", " plastics ", " plastic ",
	} {
		if strings.Contains(f, tok) {
			return true
		}
	}
	return strings.Contains(folded, "power tool")
}

func nameDorkScore(row Merchant) int {
	cc := strings.ToUpper(strings.TrimSpace(row.Country))
	score := nameDorkMarkets[cc]
	low := strings.ToLower(row.Name)
	for _, bonus := range []string{"lighting", "furniture", "led", "lamp", "apparel", "textile", "cosmetic", "valve", "solar"} {
		if strings.Contains(low, bonus) {
			score += 20
			break
		}
	}
	for _, bonus := range []string{"trading", "import", "export", "manufactur", "wholesale"} {
		if strings.Contains(low, bonus) {
			score += 15
			break
		}
	}
	return score
}

func nameDorkQuery(row Merchant) string {
	folded := foldLegalName(row.Name)
	if folded == "" {
		folded = strings.TrimSpace(row.Name)
	}
	qt := quoteSearchTerm(folded)
	geo := CountryQueryToken(row.Country, false)
	q := qt + ` (site:linkedin.com/company OR site:facebook.com OR site:instagram.com)`
	if geo != "" {
		q += " " + geo
	}
	return q
}

func nameHitMatches(row Merchant, hit Hit) bool {
	if !keepDorkHit(hit) {
		return false
	}
	tokens := nameMatchTokens(foldLegalName(row.Name))
	if len(tokens) == 0 {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(hit.Name + " " + hit.Handle + " " + hit.HomepageURL))
	hitN := 0
	for _, tok := range tokens {
		if strings.Contains(text, tok) {
			hitN++
		}
	}
	need := 2
	if len(tokens) < 2 {
		need = 1
	}
	return hitN >= need && hitN*2 >= len(tokens)
}

func nameMatchTokens(folded string) []string {
	var out []string
	for _, tok := range strings.Fields(strings.ToLower(folded)) {
		if len(tok) < 3 || tok == "and" || tok == "the" || tok == "for" {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// HarvestNameDorks looks up social homepages for existing GLEIF companies
// by legal name (外贸常用：公司全名 + site:linkedin/facebook).
func (c *Client) HarvestNameDorks(ctx context.Context, opt HarvestOptions) (HarvestStats, error) {
	started := time.Now()
	if opt.DBPath == "" {
		opt.DBPath = DefaultMerchantDB
	}
	if opt.Workers <= 0 {
		opt.Workers = 8
	}
	limit := opt.QueryLimit
	if limit <= 0 {
		limit = 4000
	}
	dir, err := OpenDirectory(opt.DBPath)
	if err != nil {
		return HarvestStats{Took: time.Since(started), Err: err.Error()}, err
	}
	defer dir.Close()

	targets, err := dir.ListNameDorkTargets(ctx, limit)
	if err != nil {
		return HarvestStats{Took: time.Since(started), Err: err.Error()}, err
	}

	var (
		mu       sync.Mutex
		attached int
		profiles int
		byPlat   = map[string]int{}
		queries  atomic.Int64
		matched  atomic.Int64
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opt.Workers)
	for i := range targets {
		row := targets[i]
		g.Go(func() error {
			if gctx.Err() != nil {
				return nil
			}
			q := nameDorkQuery(row)
			qn := queries.Add(1)
			if qn%200 == 0 {
				logIngest("name-dork %d/%d attached=%d", qn, len(targets), matched.Load())
			}
			batch, _, err := c.searchDorkFirstPage(gctx, q, "")
			if err != nil || len(batch) == 0 {
				return nil
			}
			var got []Profile
			home := ""
			for _, h := range batch {
				if !nameHitMatches(row, h) {
					continue
				}
				got = append(got, Profile{
					ExtID:    row.ExtID,
					Platform: h.Platform,
					URL:      h.HomepageURL,
					Handle:   h.Handle,
					Source:   "name-dork",
				})
				if home == "" && isRealHomepage(h.HomepageURL) {
					home = h.HomepageURL
				}
			}
			if len(got) == 0 {
				return nil
			}
			m := Merchant{ExtID: row.ExtID, Source: "gleif", Homepage: home, Profiles: got}
			n, p, err := dir.attachExisting(gctx, []Merchant{m})
			if err != nil {
				return err
			}
			mu.Lock()
			attached += n
			profiles += p
			matched.Add(int64(n))
			for _, pr := range got {
				byPlat[pr.Platform]++
			}
			mu.Unlock()
			return nil
		})
	}
	waitErr := g.Wait()
	st := HarvestStats{
		Keywords: len(targets),
		Queries:  int(queries.Load()),
		Hits:     attached,
		Inserted: attached,
		Profiles: profiles,
		ByPlat:   byPlat,
		Took:     time.Since(started),
	}
	if waitErr != nil {
		st.Err = waitErr.Error()
	}
	_ = dir.RecordRun(ctx, "name-dork", started, attached, st.String())
	logIngest("name-dork targets=%d attached=%d profiles=%d", len(targets), attached, profiles)
	return st, waitErr
}
