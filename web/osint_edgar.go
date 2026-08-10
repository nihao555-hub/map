package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Firmographics 免费公开财报/企业规模（优先 SEC EDGAR；Wikidata 可补充员工）。
type Firmographics struct {
	LegalName    string `json:"legal_name,omitempty"`
	Ticker       string `json:"ticker,omitempty"`
	CIK          string `json:"cik,omitempty"`
	Exchange     string `json:"exchange,omitempty"`
	RevenueUSD   int64  `json:"revenue_usd,omitempty"`
	RevenueYear  string `json:"revenue_year,omitempty"`
	Employees    int    `json:"employees,omitempty"`
	EmployeesAsOf string `json:"employees_as_of,omitempty"`
	Source       string `json:"source,omitempty"`
	FilingURL    string `json:"filing_url,omitempty"`
}

type secTickerRow struct {
	CIK    int    `json:"cik_str"`
	Ticker string `json:"ticker"`
	Title  string `json:"title"`
}

var (
	secTickerMu   sync.Mutex
	secTickers    []secTickerRow
)

func secUserAgent() string {
	if v := strings.TrimSpace(os.Getenv("SEC_USER_AGENT")); v != "" {
		return v
	}
	// SEC 拒绝无联系邮箱的 UA（会 403）；须含可联系地址。
	return "google-maps-scraper intel (https://github.com/gosom/google-maps-scraper; research@example.com)"
}

func loadSECTickers(ctx context.Context) ([]secTickerRow, error) {
	secTickerMu.Lock()
	defer secTickerMu.Unlock()
	if len(secTickers) > 0 {
		return secTickers, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.sec.gov/files/company_tickers.json", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", secUserAgent())
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("sec tickers status %d", resp.StatusCode)
	}
	var parsed map[string]secTickerRow
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	out := make([]secTickerRow, 0, len(parsed))
	for _, row := range parsed {
		out = append(out, row)
	}
	secTickers = out
	return secTickers, nil
}

func resolveSECCIK(ctx context.Context, title string) (cik int, ticker, legal string, ok bool) {
	title = strings.TrimSpace(title)
	if title == "" || !IdentifiableCompanyName(title) {
		return 0, "", "", false
	}
	rows, err := loadSECTickers(ctx)
	if err == nil && len(rows) > 0 {
		core := coreBusinessTokens(title)
		if len(core) == 0 {
			core = distinctiveNameTokens(title)
		}
		brand := strings.ToLower(companyBrandToken(title))
		var best *secTickerRow
		bestScore := 0
		titleLow := strings.ToLower(title)
		for i := range rows {
			row := &rows[i]
			legalLow := strings.ToLower(row.Title)
			score := 0
			if legalLow == titleLow || strings.HasPrefix(legalLow, titleLow+",") {
				score = 100
			}
			if brand != "" && len(brand) >= 4 && (strings.HasPrefix(legalLow, brand+" ") || legalLow == brand ||
				strings.HasPrefix(legalLow, brand+",") || strings.Contains(legalLow, " "+brand+" ")) {
				score += 40
			}
			shared := 0
			for _, tok := range core {
				if strings.Contains(legalLow, tok) {
					shared++
				}
			}
			score += shared * 15
			if len(core) >= 2 && shared < 2 && score < 100 {
				continue
			}
			if len(core) == 1 && shared < 1 {
				continue
			}
			if score < 40 {
				continue
			}
			if best == nil || score > bestScore {
				cp := *row
				best = &cp
				bestScore = score
			}
		}
		if best != nil {
			if ExternalRecordMatchesBusiness(best.Title, title) ||
				ExternalRecordMatchesBusiness(strings.Split(best.Title, ",")[0], title) ||
				(brand != "" && strings.Contains(strings.ToLower(best.Title), brand)) {
				return best.CIK, best.Ticker, best.Title, true
			}
		}
	}
	return resolveSECCIKViaSearch(ctx, title)
}

var secCIKRe = regexp.MustCompile(`(?i)CIK=0*([0-9]+)`)

func resolveSECCIKViaSearch(ctx context.Context, title string) (cik int, ticker, legal string, ok bool) {
	brand := companyBrandToken(title)
	q := firstNonEmpty(brand, title)
	u := "https://www.sec.gov/cgi-bin/browse-edgar?company=" + url.QueryEscape(q) +
		"&owner=exclude&action=getcompany&output=atom"
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, "", "", false
	}
	req.Header.Set("User-Agent", secUserAgent())
	req.Header.Set("Accept", "application/atom+xml,application/xml,text/xml,*/*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", "", false
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return 0, "", "", false
	}
	body := string(raw)
	legal = extractXMLTag(body, "conformed-name")
	cikStr := extractXMLTag(body, "cik")
	if cikStr == "" {
		if m := secCIKRe.FindStringSubmatch(body); len(m) > 1 {
			cikStr = m[1]
		}
	}
	if cikStr == "" {
		return 0, "", "", false
	}
	n, err := strconv.Atoi(strings.TrimLeft(cikStr, "0"))
	if err != nil || n <= 0 {
		n, _ = strconv.Atoi(cikStr)
	}
	if n <= 0 {
		return 0, "", "", false
	}
	formerNames := extractSECFormerNames(body)
	brandLow := strings.ToLower(brand)
	matched := ExternalRecordMatchesBusiness(legal, title)
	if !matched {
		for _, fn := range formerNames {
			if ExternalRecordMatchesBusiness(fn, title) ||
				(brandLow != "" && strings.Contains(strings.ToLower(fn), brandLow)) {
				matched = true
				break
			}
		}
	}
	if !matched && brandLow != "" {
		// company-info 块内出现品牌（含曾用名 Allbirds, Inc.）
		info := body
		if i := strings.Index(strings.ToLower(body), "<company-info>"); i >= 0 {
			info = body[i:]
			if j := strings.Index(strings.ToLower(info), "</company-info>"); j > 0 {
				info = info[:j]
			}
		}
		matched = strings.Contains(strings.ToLower(info), brandLow)
	}
	if !matched {
		return 0, "", "", false
	}
	return n, "", firstNonEmpty(legal, title), true
}

func extractSECFormerNames(body string) []string {
	re := regexp.MustCompile(`(?is)<formerly-names[^>]*>[\s\S]*?</formerly-names>`)
	block := re.FindString(body)
	if block == "" {
		return nil
	}
	nameRe := regexp.MustCompile(`(?is)<name>([^<]+)</name>`)
	var out []string
	for _, m := range nameRe.FindAllStringSubmatch(block, -1) {
		if len(m) > 1 {
			out = append(out, strings.TrimSpace(html.UnescapeString(m[1])))
		}
	}
	return out
}

func extractXMLTag(body, tag string) string {
	re := regexp.MustCompile(`(?is)<` + tag + `[^>]*>([^<]+)</` + tag + `>`)
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(m[1]))
}

func lookupEDGARFirmographics(ctx context.Context, title string) (*Firmographics, error) {
	cik, ticker, legal, ok := resolveSECCIK(ctx, title)
	if !ok {
		return nil, fmt.Errorf("edgar: no CIK for %q", title)
	}
	cikPad := fmt.Sprintf("%010d", cik)
	factsURL := "https://data.sec.gov/api/xbrl/companyfacts/CIK" + cikPad + ".json"
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, factsURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", secUserAgent())
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("edgar facts status %d", resp.StatusCode)
	}
	var doc struct {
		EntityName string `json:"entityName"`
		Facts      map[string]map[string]struct {
			Units map[string][]struct {
				End   string  `json:"end"`
				Val   float64 `json:"val"`
				FY    int     `json:"fy"`
				FP    string  `json:"fp"`
				Form  string  `json:"form"`
				Filed string  `json:"filed"`
			} `json:"units"`
		} `json:"facts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := &Firmographics{
		LegalName: firstNonEmpty(doc.EntityName, legal),
		Ticker:    ticker,
		CIK:       strconv.Itoa(cik),
		Source:    "sec-edgar",
		FilingURL: "https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&CIK=" + cikPad + "&type=10-K&owner=exclude&count=40",
	}
	revenueKeys := []string{
		"RevenueFromContractWithCustomerExcludingAssessedTax",
		"RevenueFromContractWithCustomerIncludingAssessedTax",
		"SalesRevenueNet",
		"Revenues",
		"RevenueFromContractWithCustomer",
	}
	for _, section := range []string{"us-gaap", "ifrs-full"} {
		facts := doc.Facts[section]
		if facts == nil {
			continue
		}
		for _, key := range revenueKeys {
			concept, ok := facts[key]
			if !ok {
				continue
			}
			usd := concept.Units["USD"]
			if len(usd) == 0 {
				continue
			}
			bestEnd := ""
			var bestVal float64
			bestFY := 0
			for _, row := range usd {
				if row.FP != "FY" && !strings.HasPrefix(row.Form, "10-K") {
					continue
				}
				if row.End >= bestEnd {
					bestEnd = row.End
					bestVal = row.Val
					bestFY = row.FY
				}
			}
			if bestEnd != "" {
				out.RevenueUSD = int64(bestVal)
				if bestFY > 0 {
					out.RevenueYear = strconv.Itoa(bestFY)
				} else if len(bestEnd) >= 4 {
					out.RevenueYear = bestEnd[:4]
				}
				break
			}
		}
		if out.RevenueUSD > 0 {
			break
		}
	}
	// 员工数极少出现在 XBRL；若有 EntityNumberOfEmployees / NumberOfEmployees 则取。
	for _, section := range []string{"dei", "us-gaap"} {
		facts := doc.Facts[section]
		if facts == nil {
			continue
		}
		for _, key := range []string{"EntityNumberOfEmployees", "NumberOfEmployees"} {
			concept, ok := facts[key]
			if !ok {
				continue
			}
			for _, unit := range concept.Units {
				bestEnd := ""
				bestVal := 0.0
				for _, row := range unit {
					if row.End >= bestEnd {
						bestEnd = row.End
						bestVal = row.Val
					}
				}
				if bestVal > 0 {
					out.Employees = int(bestVal)
					out.EmployeesAsOf = bestEnd
				}
			}
		}
	}
	if out.RevenueUSD == 0 && out.Employees == 0 && out.LegalName == "" {
		return nil, fmt.Errorf("edgar: empty facts")
	}
	return out, nil
}

func applyFirmographics(intel *PlaceIntel, f *Firmographics) {
	if intel == nil || f == nil {
		return
	}
	if intel.Firmographics == nil {
		intel.Firmographics = f
	} else {
		dst := intel.Firmographics
		if dst.LegalName == "" {
			dst.LegalName = f.LegalName
		}
		if dst.Ticker == "" {
			dst.Ticker = f.Ticker
		}
		if dst.CIK == "" {
			dst.CIK = f.CIK
		}
		if dst.RevenueUSD == 0 {
			dst.RevenueUSD = f.RevenueUSD
			dst.RevenueYear = f.RevenueYear
		}
		if dst.Employees == 0 {
			dst.Employees = f.Employees
			dst.EmployeesAsOf = f.EmployeesAsOf
		}
		if dst.FilingURL == "" {
			dst.FilingURL = f.FilingURL
		}
		if dst.Source == "" {
			dst.Source = f.Source
		}
	}
	intel.Sources = mergeUnique(intel.Sources, []string{"sec-edgar"})
	if f.FilingURL != "" {
		intel.Sources = mergeUnique(intel.Sources, []string{f.FilingURL})
	}
	intel.Provider = strings.Trim(intel.Provider+"+edgar", "+")
}
