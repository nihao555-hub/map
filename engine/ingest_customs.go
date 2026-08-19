package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CustomsIngestOptions controls bulk ingest of public customs/trade feeds.
type CustomsIngestOptions struct {
	DBPath       string
	Year         int
	SkipUS       bool
	SkipUK       bool
	SkipComtrade bool
	USKeywords   []string
	USLimit      int
	ComtradeHS   []string
}

// DefaultCustomsIngestOptions ingests all public feeds we can bulk-download legally.
func DefaultCustomsIngestOptions() CustomsIngestOptions {
	return CustomsIngestOptions{
		DBPath:     ResolveCustomsDB(""),
		Year:       time.Now().UTC().Year(),
		USKeywords: customsHarvestKeywords(),
		USLimit:    200,
		ComtradeHS: []string{"TOTAL"},
	}
}

func customsHarvestKeywords() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[strings.ToLower(s)] {
			return
		}
		seen[strings.ToLower(s)] = true
		out = append(out, s)
	}
	for _, kw := range DefaultHarvestKeywords {
		add(kw)
		if en := yellowPageKeywordEN[kw]; en != "" {
			add(en)
		}
	}
	for _, hs := range []string{
		"9403", "9401", "8504", "8516", "8471", "6109", "6403", "3926", "7308", "8541",
	} {
		add(hs)
	}
	return out
}

// IngestCustomsPublic loads every public customs feed we can bulk persist.
func (c *Client) IngestCustomsPublic(ctx context.Context, opt CustomsIngestOptions) ([]IngestStats, error) {
	if opt.DBPath == "" {
		opt.DBPath = ResolveCustomsDB("")
	}
	if opt.Year <= 0 {
		opt.Year = time.Now().UTC().Year()
	}
	store, err := OpenCustomsStore(opt.DBPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	var stats []IngestStats
	if !opt.SkipUK {
		st, err := c.ingestUKTradeInfo(ctx, store)
		if err != nil && st.Err == "" {
			st.Err = err.Error()
		}
		stats = append(stats, st)
	}
	if !opt.SkipComtrade {
		stats = append(stats, c.ingestComtradeBulk(ctx, store, opt)...)
	}
	if !opt.SkipUS {
		st, err := c.ingestKirchnerBulk(ctx, store, opt)
		if err != nil && st.Err == "" {
			st.Err = err.Error()
		}
		stats = append(stats, st)
	}
	return stats, nil
}

func (c *Client) ingestKirchnerBulk(ctx context.Context, store *CustomsStore, opt CustomsIngestOptions) (IngestStats, error) {
	started := time.Now()
	source := "kirchner-us-bol"
	if c == nil || store == nil || strings.TrimSpace(c.CustomsBaseURL) == "" {
		return IngestStats{Source: source, Took: time.Since(started), Note: "skipped"}, nil
	}
	keywords := opt.USKeywords
	if len(keywords) == 0 {
		keywords = customsHarvestKeywords()
	}
	limit := opt.USLimit
	if limit <= 0 {
		limit = 200
	}
	if limit > 200 {
		limit = 200
	}
	year := opt.Year
	var (
		companies []CustomsCompany
		products  []CustomsCompanyProduct
		ships     []CustomsShipmentRow
		stats     []CustomsCompanyStat
	)
	seen := map[string]struct{}{}
	for _, term := range keywords {
		if ctx.Err() != nil {
			break
		}
		yearUsed := year
		match := "product_desc"
		if looksLikeHS(term) || hs4Digits(term) != "" {
			match = "hs_code"
			if hs := hs4Digits(term); hs != "" {
				term = hs
			}
		}
		payload, err := c.fetchLeadFinder(ctx, term, match, yearUsed, limit, true)
		if err != nil && yearUsed == time.Now().UTC().Year() {
			yearUsed = year - 1
			payload, err = c.fetchLeadFinder(ctx, term, match, yearUsed, limit, true)
		}
		if err != nil {
			logIngest("kirchner bulk %q: %v", term, err)
			continue
		}
		period := strconv.Itoa(yearUsed)
		for _, row := range payload.Importers {
			name := strings.TrimSpace(row.Name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			ext := customsCompanyExtID("US", "importer", period, name, "")
			companies = append(companies, CustomsCompany{
				ExtID: ext, Country: "US", Name: name, Role: "importer",
				Source: "kirchner", Period: period,
			})
			stats = append(stats, CustomsCompanyStat{
				ExtID: ext, Year: yearUsed, TotalShipments: row.TotalShipments,
				MatchingShipments: row.MatchingShipments, Source: "kirchner",
			})
		}
		for _, raw := range payload.Profiles {
			name := strings.TrimSpace(asString(raw["name"]))
			if name == "" {
				continue
			}
			ext := customsCompanyExtID("US", "importer", period, name, "")
			if addr := asString(raw["address"]); addr != "" {
				for i := range companies {
					if companies[i].ExtID == ext {
						companies[i].Address = addr
						break
					}
				}
			}
			for _, p := range mapCustomsProducts(raw["top_products"], 20) {
				if p.Code == "" {
					continue
				}
				products = append(products, CustomsCompanyProduct{
					ExtID: ext, HSCode: p.Code, Country: "US", Source: "kirchner", Period: period,
				})
			}
			for _, sh := range mapCustomsShipments(raw["latest_shipments"], 12) {
				ships = append(ships, CustomsShipmentRow{
					ExtID: ext, ShipDate: sh.Date, Shipper: sh.Shipper, Consignee: firstNonEmpty(sh.Consignee, name),
					Product: sh.Product, HSCode: sh.HSCode, Origin: sh.Country, Vessel: sh.Vessel,
					Source: "kirchner", Year: yearUsed,
				})
			}
		}
		if len(seen)%500 == 0 && len(seen) > 0 {
			logIngest("kirchner bulk companies=%d term=%q", len(seen), term)
		}
	}
	nc, err := store.UpsertCompanies(ctx, companies)
	if err != nil {
		st := IngestStats{Source: source, Took: time.Since(started), Err: err.Error()}
		_ = store.RecordRun(ctx, source, started, 0, st.Err)
		return st, err
	}
	np, _ := store.UpsertCompanyProducts(ctx, products)
	ns, _ := store.UpsertShipments(ctx, ships)
	nst, _ := store.UpsertCompanyStats(ctx, stats)
	total := nc + np + ns + nst
	note := fmt.Sprintf("year=%d keywords=%d companies=%d products=%d shipments=%d stats=%d", year, len(keywords), nc, np, ns, nst)
	st := IngestStats{Source: source, Rows: total, Took: time.Since(started), Note: note}
	_ = store.RecordRun(ctx, source, started, total, note)
	logIngest("kirchner bulk done %s", note)
	return st, nil
}

var comtradeBulkReporters = []struct {
	Code string
	ISO  string
	Name string
}{
	{"842", "USA", "United States"},
	{"826", "GBR", "United Kingdom"},
	{"156", "CHN", "China"},
	{"276", "DEU", "Germany"},
	{"392", "JPN", "Japan"},
	{"410", "KOR", "Korea"},
	{"699", "IND", "India"},
	{"704", "VNM", "Vietnam"},
	{"764", "THA", "Thailand"},
	{"458", "MYS", "Malaysia"},
	{"360", "IDN", "Indonesia"},
	{"702", "SGP", "Singapore"},
	{"608", "PHL", "Philippines"},
	{"251", "FRA", "France"},
	{"380", "ITA", "Italy"},
	{"724", "ESP", "Spain"},
	{"528", "NLD", "Netherlands"},
	{"076", "BRA", "Brazil"},
	{"484", "MEX", "Mexico"},
	{"124", "CAN", "Canada"},
	{"036", "AUS", "Australia"},
	{"784", "ARE", "United Arab Emirates"},
	{"682", "SAU", "Saudi Arabia"},
	{"792", "TUR", "Turkey"},
	{"643", "RUS", "Russia"},
}

func (c *Client) ingestComtradeBulk(ctx context.Context, store *CustomsStore, opt CustomsIngestOptions) []IngestStats {
	started := time.Now()
	source := "comtrade"
	if c == nil || store == nil || strings.TrimSpace(c.ComtradeURL) == "" {
		return []IngestStats{{Source: source, Took: time.Since(started), Note: "skipped"}}
	}
	hsCodes := opt.ComtradeHS
	if len(hsCodes) == 0 {
		hsCodes = []string{"TOTAL"}
	}
	year := opt.Year
	period := comtradePeriod(year)
	var stats []IngestStats
	totalRows := 0
	for _, rep := range comtradeBulkReporters {
		if ctx.Err() != nil {
			break
		}
		for _, hs := range hsCodes {
			for _, flow := range []string{"M", "X"} {
				rows, err := c.fetchComtradeBulk(ctx, hs, period, rep.Code, flow)
				if err != nil {
					logIngest("comtrade %s %s %s: %v", rep.ISO, hs, flow, err)
					continue
				}
				n, err := store.UpsertComtradeFlows(ctx, rows, rep.Code, rep.ISO, rep.Name, flow, strconv.Itoa(period), hs, source)
				if err != nil {
					logIngest("comtrade store %s: %v", rep.ISO, err)
					continue
				}
				totalRows += n
			}
		}
	}
	note := fmt.Sprintf("reporters=%d hs=%v period=%d flows=%d", len(comtradeBulkReporters), hsCodes, period, totalRows)
	st := IngestStats{Source: source, Rows: totalRows, Took: time.Since(started), Note: note}
	_ = store.RecordRun(ctx, source, started, totalRows, note)
	stats = append(stats, st)
	logIngest("comtrade bulk done %s", note)
	return stats
}

func (c *Client) fetchComtradeBulk(ctx context.Context, hs4 string, year int, reporter, flow string) ([]comtradeRecord, error) {
	if c == nil || strings.TrimSpace(c.ComtradeURL) == "" || reporter == "" {
		return nil, nil
	}
	u, err := buildComtradeURL(c.ComtradeURL, reporter, year, hs4, flow, 500)
	if err != nil {
		return nil, err
	}
	raw, err := c.get(ctx, u, map[string]string{
		"Accept":     "application/json",
		"User-Agent": "map-engine/comtrade-bulk (https://github.com/nihao555-hub/map)",
	})
	if err != nil {
		return nil, err
	}
	var doc comtradePreview
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make([]comtradeRecord, 0, len(doc.Data))
	for _, row := range doc.Data {
		if row.PartnerCode == 0 || row.PrimaryValue <= 0 {
			continue
		}
		desc := strings.ToLower(row.PartnerDesc)
		if strings.Contains(desc, "world") || strings.Contains(desc, "areas") {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func buildComtradeURL(base, reporter string, year int, hs, flow string, maxRecords int) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/C/A/HS")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("reporterCode", reporter)
	q.Set("period", strconv.Itoa(year))
	q.Set("cmdCode", hs)
	q.Set("flowCode", flow)
	q.Set("maxRecords", strconv.Itoa(maxRecords))
	q.Set("includeDesc", "true")
	u.RawQuery = q.Encode()
	return u.String(), nil
}
