package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	secCompanyTickersURL = "https://www.sec.gov/files/company_tickers.json"
	secSubmissionsURL    = "https://data.sec.gov/submissions/CIK%s.json"
	secUserAgent         = "map-engine/sec (https://github.com/nihao555-hub/map)"
)

type secTicker struct {
	CIK    int    `json:"cik_str"`
	Ticker string `json:"ticker"`
	Title  string `json:"title"`
}

type secSubmission struct {
	Name            string `json:"name"`
	Website         string `json:"website"`
	InvestorWebsite string `json:"investorWebsite"`
}

func (c *Client) ingestSECTickers(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil || dir == nil {
		return IngestStats{Source: "sec", Took: time.Since(started), Note: "skipped"}
	}
	tickers, err := c.loadSECTickers(ctx)
	if err != nil {
		st := IngestStats{Source: "sec", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "sec", started, 0, st.Err)
		return st
	}
	targets, err := dir.loadBareGLEIFKeys(ctx)
	if err != nil {
		st := IngestStats{Source: "sec", Took: time.Since(started), Err: err.Error()}
		_ = dir.RecordRun(ctx, "sec", started, 0, st.Err)
		return st
	}
	type slot struct {
		t        secTicker
		conflict bool
	}
	byKey := map[string]*slot{}
	for _, t := range tickers {
		key := nameCountryKey(t.Title, "US")
		if key == "" {
			continue
		}
		if cur, ok := byKey[key]; ok {
			cur.conflict = true
			continue
		}
		t := t
		byKey[key] = &slot{t: t}
	}
	type match struct {
		extID string
		t     secTicker
	}
	var matches []match
	for key, extID := range targets {
		if !strings.HasPrefix(key, "US|") {
			continue
		}
		slot, ok := byKey[key]
		if !ok || slot.conflict {
			continue
		}
		matches = append(matches, match{extID: extID, t: slot.t})
	}
	const maxSECWebsiteFetches = 4000
	if len(matches) > maxSECWebsiteFetches {
		matches = matches[:maxSECWebsiteFetches]
	}
	var rows []Merchant
	failed := 0
	for i, m := range matches {
		if err := ctx.Err(); err != nil {
			break
		}
		home, err := c.fetchSECWebsite(ctx, m.t.CIK)
		if err != nil || !isRealHomepage(home) {
			if err != nil {
				failed++
			}
			time.Sleep(120 * time.Millisecond)
			continue
		}
		rows = append(rows, Merchant{
			ExtID:    m.extID,
			Source:   "gleif",
			Homepage: home,
			Profiles: []Profile{{
				ExtID:    m.extID,
				Platform: PlatformWebsite,
				URL:      home,
				Source:   "sec",
			}},
		})
		if (i+1)%200 == 0 {
			logIngest("SEC websites %d/%d attached=%d", i+1, len(matches), len(rows))
		}
		time.Sleep(120 * time.Millisecond)
	}
	matched, profiles, err := dir.attachExisting(ctx, rows)
	st := IngestStats{
		Source: "sec",
		Rows:   matched,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("tickers=%d unique-us=%d websites=%d profiles=%d failed=%d", len(tickers), len(matches), len(rows), profiles, failed),
	}
	if err != nil {
		st.Err = err.Error()
	}
	_ = dir.RecordRun(ctx, "sec", started, matched, st.Note)
	logIngest("SEC attached %d GLEIF websites", matched)
	return st
}

func (c *Client) loadSECTickers(ctx context.Context) ([]secTicker, error) {
	path := secTickerCachePath()
	if _, err := os.Stat(path); err != nil {
		if err := downloadCachedURL(ctx, c.httpClient(), secCompanyTickersURL, path, 1024); err != nil {
			return nil, err
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseSECTickers(raw)
}

func parseSECTickers(raw []byte) ([]secTicker, error) {
	var doc map[string]secTicker
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make([]secTicker, 0, len(doc))
	for _, t := range doc {
		if t.CIK <= 0 || strings.TrimSpace(t.Title) == "" {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (c *Client) fetchSECWebsite(ctx context.Context, cik int) (string, error) {
	if c == nil || cik <= 0 {
		return "", fmt.Errorf("missing cik")
	}
	padded := fmt.Sprintf("%010d", cik)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(secSubmissionsURL, padded), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", secUserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("sec %s: %s", padded, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return secWebsiteFromJSON(body), nil
}

func secWebsiteFromJSON(raw []byte) string {
	var doc secSubmission
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	home := firstNonEmpty(doc.Website, doc.InvestorWebsite)
	home = strings.TrimSpace(home)
	if home == "" {
		return ""
	}
	if !strings.Contains(home, "://") {
		home = "https://" + strings.TrimPrefix(home, "//")
	}
	if !isRealHomepage(home) {
		return ""
	}
	return home
}

func secTickerCachePath() string {
	return filepath.Join(os.TempDir(), "merchant-ingest", "sec-company-tickers.json")
}
