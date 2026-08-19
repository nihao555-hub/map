package engine

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const ukTradeInfoBase = "https://www.uktradeinfo.com"

var ukTradeZipRe = regexp.MustCompile(`href="(/media/[^"]+/(?:importers|exporters|bdsimp|bdsexp)(\d{4})\.zip)"`)

// ukTradeBulkURLs scrapes the latest HMRC bulk download links.
func ukTradeBulkURLs(ctx context.Context, client *http.Client) (map[string]string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ukTradeInfoBase+"/trade-data/latest-bulk-data-sets", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, m := range ukTradeZipRe.FindAllStringSubmatch(string(raw), -1) {
		if len(m) < 3 {
			continue
		}
		path := strings.ToLower(m[1])
		period := m[2]
		switch {
		case strings.Contains(path, "importers"):
			out["importers:"+period] = ukTradeInfoBase + m[1]
		case strings.Contains(path, "exporters"):
			out["exporters:"+period] = ukTradeInfoBase + m[1]
		case strings.Contains(path, "bdsimp"):
			out["bdsimp:"+period] = ukTradeInfoBase + m[1]
		case strings.Contains(path, "bdsexp"):
			out["bdsexp:"+period] = ukTradeInfoBase + m[1]
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("uktradeinfo: no bulk zip links found")
	}
	return out, nil
}

func downloadZipEntry(ctx context.Context, client *http.Client, zipURL string) ([]byte, string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zipURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("uktradeinfo: %s status %d", zipURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return nil, "", err
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, "", err
	}
	if len(zr.File) == 0 {
		return nil, "", fmt.Errorf("uktradeinfo: empty zip %s", zipURL)
	}
	f := zr.File[0]
	rc, err := f.Open()
	if err != nil {
		return nil, "", err
	}
	defer rc.Close()
	txt, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", err
	}
	return txt, f.Name, nil
}

func parseUKCompanyFile(body []byte, role string) ([]CustomsCompany, []CustomsCompanyProduct) {
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var companies []CustomsCompany
	var products []CustomsCompanyProduct
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 10 {
			continue
		}
		period := strings.TrimSpace(parts[0])
		name := cleanUKCompanyField(parts[2])
		if name == "" || period == "" {
			continue
		}
		addr := cleanUKCompanyField(strings.Join(parts[3:8], ", "))
		postcode := cleanUKCompanyField(parts[8])
		ext := customsCompanyExtID("GB", role, period, name, postcode)
		companies = append(companies, CustomsCompany{
			ExtID:    ext,
			Country:  "GB",
			Name:     name,
			Address:  addr,
			Postcode: postcode,
			Role:     role,
			Source:   "uktradeinfo",
			Period:   period,
		})
		for _, hs := range parts[9:] {
			hs = strings.TrimSpace(hs)
			if len(hs) < 4 || !isDigits(hs) {
				continue
			}
			products = append(products, CustomsCompanyProduct{
				ExtID:   ext,
				HSCode:  hs,
				Country: "GB",
				Source:  "uktradeinfo",
				Period:  period,
			})
		}
	}
	return companies, products
}

func cleanUKCompanyField(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "'\"")
	s = strings.ReplaceAll(s, "''", "'")
	return strings.TrimSpace(s)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseUKBDSLines(body []byte, flow string) []CustomsTradeLine {
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
	var out []CustomsTradeLine
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 85 {
			continue
		}
		hs := strings.TrimSpace(line[13:21])
		if hs == "" || strings.Contains(hs, "-") {
			continue
		}
		out = append(out, CustomsTradeLine{
			ReporterCountry: "GB",
			Flow:            strings.TrimSpace(line[81:84]),
			Period:          strings.TrimSpace(line[0:6]),
			HSCode:          hs,
			PartnerCountry:  strings.TrimSpace(line[29:31]),
			OriginCountry:   strings.TrimSpace(line[40:42]),
			PortCode:        strings.TrimSpace(line[34:37]),
			StatValue:       parseUKStatInt(line[44:56]),
			NetMass:         parseUKStatInt(line[56:68]),
			RecordType:      strings.TrimSpace(line[6:7]),
			Source:          "uktradeinfo-bds-" + flow,
		})
	}
	return out
}

func parseUKStatInt(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, _ := strconv.ParseInt(raw, 10, 64)
	return n
}

func (c *Client) ingestUKTradeInfo(ctx context.Context, store *CustomsStore) (IngestStats, error) {
	started := time.Now()
	if c == nil || store == nil {
		return IngestStats{Source: "uktradeinfo", Took: time.Since(started), Note: "skipped"}, nil
	}
	urls, err := ukTradeBulkURLs(ctx, c.httpClient())
	if err != nil {
		st := IngestStats{Source: "uktradeinfo", Took: time.Since(started), Err: err.Error()}
		_ = store.RecordRun(ctx, "uktradeinfo", started, 0, st.Err)
		return st, err
	}
	var (
		companies []CustomsCompany
		products  []CustomsCompanyProduct
		lines     []CustomsTradeLine
	)
	for kind, u := range urls {
		txt, name, err := downloadZipEntry(ctx, c.httpClient(), u)
		if err != nil {
			logIngest("uktradeinfo %s: %v", kind, err)
			continue
		}
		logIngest("uktradeinfo downloaded %s (%s, %d bytes)", kind, name, len(txt))
		switch {
		case strings.HasPrefix(kind, "importers:"):
			co, pr := parseUKCompanyFile(txt, "importer")
			companies = append(companies, co...)
			products = append(products, pr...)
		case strings.HasPrefix(kind, "exporters:"):
			co, pr := parseUKCompanyFile(txt, "exporter")
			companies = append(companies, co...)
			products = append(products, pr...)
		case strings.HasPrefix(kind, "bdsimp:"):
			lines = append(lines, parseUKBDSLines(txt, "import")...)
		case strings.HasPrefix(kind, "bdsexp:"):
			lines = append(lines, parseUKBDSLines(txt, "export")...)
		}
	}
	nc, err := store.UpsertCompanies(ctx, companies)
	if err != nil {
		st := IngestStats{Source: "uktradeinfo", Took: time.Since(started), Err: err.Error()}
		_ = store.RecordRun(ctx, "uktradeinfo", started, 0, st.Err)
		return st, err
	}
	np, err := store.UpsertCompanyProducts(ctx, products)
	if err != nil {
		st := IngestStats{Source: "uktradeinfo", Took: time.Since(started), Err: err.Error()}
		_ = store.RecordRun(ctx, "uktradeinfo", started, nc, st.Err)
		return st, err
	}
	nl, err := store.UpsertTradeLines(ctx, lines)
	if err != nil {
		st := IngestStats{Source: "uktradeinfo", Took: time.Since(started), Err: err.Error()}
		_ = store.RecordRun(ctx, "uktradeinfo", started, nc+np, st.Err)
		return st, err
	}
	total := nc + np + nl
	note := fmt.Sprintf("companies=%d products=%d trade_lines=%d periods=%d", nc, np, nl, len(urls))
	st := IngestStats{Source: "uktradeinfo", Rows: total, Took: time.Since(started), Note: note}
	_ = store.RecordRun(ctx, "uktradeinfo", started, total, note)
	logIngest("uktradeinfo done %s", note)
	return st, nil
}
