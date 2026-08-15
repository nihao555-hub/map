package engine

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultHTTPTimeout     = 45 * time.Second
	sidecarProbeWait       = 800 * time.Millisecond
	maxBodyBytes           = 2 << 20
	defaultTikTokURL       = "http://127.0.0.1:8091"
	defaultF2URL           = "http://127.0.0.1:8092"
	defaultKirchnerURL     = "https://www.kirchnerdata.com"
	defaultWikidataSPARQL  = "https://query.wikidata.org/sparql"
	defaultComtradeURL     = "https://comtradeapi.un.org/public/v1/preview"
	defaultUSITCURL        = "https://hts.usitc.gov/reststop/search"
	defaultAUMAFairURL     = "https://www.auma.de/en/find-your-fair/"
	defaultFairCalendarURL = "https://raw.githubusercontent.com/LensmorOfficial/trade-show-calendar/main/data/trade_shows.json"
	browserUA              = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"

	ddgCooldown   = 12 * time.Second
	bingCooldown  = 10 * time.Second
	braveCooldown = 15 * time.Second
	maxCooldown   = 45 * time.Second
)

// Client talks to public web indexes and cloned high-star OSS sidecars.
// It does not sign or scrape Douyin/TikTok APIs itself.
type Client struct {
	HTTP          *http.Client
	TikTokURL     string
	F2URL         string
	TikHubToken   string
	DisablePublic bool
	// SkipExpand turns off sister-profile expansion (unit tests with shared mock HTML).
	SkipExpand bool
	// AIBaseURL is an OpenAI-compatible local endpoint (Ollama default :11434/v1).
	AIBaseURL string
	AIModel   string
	// CustomsBaseURL is Kirchner's public US bill-of-lading API (no key).
	CustomsBaseURL string
	// WikidataURL is the SPARQL endpoint used by 展会获客.
	WikidataURL string
	// ComtradeURL is UN Comtrade public preview (uncomtrade/comtradeapicall, no key).
	ComtradeURL string
	// USITCURL is the USITC HTS keyword search (product → HS, no key).
	USITCURL string
	// ImportYetiURL is the live ImportYeti search host (US bills of lading).
	ImportYetiURL string
	// ImportYetiAPIURL is the official data.importyeti.com API host.
	ImportYetiAPIURL string
	// ImportYetiAPIKey is optional; without it the public /api/search is used.
	ImportYetiAPIKey string
	// ImportYetiCookie is an optional session cookie for the public search.
	ImportYetiCookie string
	// FairCalendarURL is a maintained JSON calendar of major world fairs.
	FairCalendarURL string
	// FairMapURL is an optional live JSON fair map (empty by default).
	FairMapURL string
	// EventsEyeURL is EventsEye's live worldwide trade-show directory (~12k fairs).
	EventsEyeURL string
	// AUMAFairURL is AUMA FairFinder, a live trade-fair calendar.
	AUMAFairURL string
	braveUntil  atomic.Int64
	ddgUntil    atomic.Int64
	bingUntil   atomic.Int64
}

// OptionsFromEnv wires sidecar base URLs.
//
//	ENGINE_TIKTOK_SIDECAR_URL  davidteather/TikTok-Api (default http://127.0.0.1:8091)
//	ENGINE_F2_SIDECAR_URL      Johnserf-Seed/f2        (default http://127.0.0.1:8092)
//	ENGINE_AI_BASE_URL         optional OpenAI-compatible local LLM (default http://127.0.0.1:11434/v1)
//	ENGINE_AI_MODEL            optional model name (default qwen2.5:7b)
//	ENGINE_KIRCHNER_URL        Kirchner public US BOL API (default https://www.kirchnerdata.com)
//	ENGINE_WIKIDATA_SPARQL     Wikidata SPARQL (default https://query.wikidata.org/sparql)
//	ENGINE_COMTRADE_URL        UN Comtrade public preview (default https://comtradeapi.un.org/public/v1/preview)
//	ENGINE_USITC_URL           USITC HTS search (default https://hts.usitc.gov/reststop/search)
//	ENGINE_IMPORTYETI_URL      ImportYeti live search (default https://www.importyeti.com)
//	ENGINE_IMPORTYETI_API_URL  official ImportYeti API (default https://data.importyeti.com)
//	ENGINE_IMPORTYETI_API_KEY  optional official API key
//	ENGINE_FAIR_CALENDAR_URL   JSON calendar (default LensmorOfficial/trade-show-calendar)
//	ENGINE_FAIR_MAP_URL        optional JSON fair map, off by default
//	ENGINE_EVENTSEYE_URL       EventsEye directory (default https://www.eventseye.com)
//	ENGINE_AUMA_FAIR_URL       AUMA FairFinder (default https://www.auma.de/en/find-your-fair/)
//	TIKHUB_API_TOKEN           optional paid API when Douyin keyword search is needed
func OptionsFromEnv() *Client {
	timeout := defaultHTTPTimeout
	if raw := strings.TrimSpace(os.Getenv("ENGINE_HTTP_TIMEOUT")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			timeout = d
		}
	}

	tiktok := strings.TrimRight(strings.TrimSpace(os.Getenv("ENGINE_TIKTOK_SIDECAR_URL")), "/")
	if tiktok == "" {
		tiktok = defaultTikTokURL
	}

	f2 := strings.TrimRight(strings.TrimSpace(os.Getenv("ENGINE_F2_SIDECAR_URL")), "/")
	if f2 == "" {
		f2 = defaultF2URL
	}

	kirchner := strings.TrimRight(strings.TrimSpace(os.Getenv("ENGINE_KIRCHNER_URL")), "/")
	if kirchner == "" {
		kirchner = defaultKirchnerURL
	}

	wikidata := strings.TrimRight(strings.TrimSpace(os.Getenv("ENGINE_WIKIDATA_SPARQL")), "/")
	if wikidata == "" {
		wikidata = defaultWikidataSPARQL
	}

	return &Client{
		HTTP:             newBrowserHTTPClient(timeout),
		TikTokURL:        tiktok,
		F2URL:            f2,
		TikHubToken:      strings.TrimSpace(firstNonEmpty(os.Getenv("TIKHUB_API_TOKEN"), os.Getenv("TIKHUB_API_KEY"))),
		AIBaseURL:        strings.TrimSpace(os.Getenv("ENGINE_AI_BASE_URL")),
		AIModel:          strings.TrimSpace(os.Getenv("ENGINE_AI_MODEL")),
		CustomsBaseURL:   kirchner,
		WikidataURL:      wikidata,
		ComtradeURL:      envServiceURL("ENGINE_COMTRADE_URL", defaultComtradeURL),
		USITCURL:         envServiceURL("ENGINE_USITC_URL", defaultUSITCURL),
		ImportYetiURL:    envServiceURL("ENGINE_IMPORTYETI_URL", defaultImportYetiURL),
		ImportYetiAPIURL: envServiceURL("ENGINE_IMPORTYETI_API_URL", defaultImportYetiAPIURL),
		ImportYetiAPIKey: strings.TrimSpace(os.Getenv("ENGINE_IMPORTYETI_API_KEY")),
		ImportYetiCookie: strings.TrimSpace(os.Getenv("ENGINE_IMPORTYETI_COOKIE")),
		FairCalendarURL:  envServiceURL("ENGINE_FAIR_CALENDAR_URL", defaultFairCalendarURL),
		FairMapURL:       envServiceURL("ENGINE_FAIR_MAP_URL", ""),
		EventsEyeURL:     envServiceURL("ENGINE_EVENTSEYE_URL", defaultEventsEyeURL),
		AUMAFairURL:      envServiceURL("ENGINE_AUMA_FAIR_URL", defaultAUMAFairURL),
	}
}

func envServiceURL(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if strings.EqualFold(v, "off") || v == "-" {
		return ""
	}
	if v == "" {
		v = fallback
	}
	return strings.TrimRight(v, "/")
}

func newBrowserHTTPClient(timeout time.Duration) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Timeout:   timeout,
		Transport: browserTransport(),
		Jar:       jar,
	}
}

func browserTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ForceAttemptHTTP2 = false
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{}
	} else {
		t.TLSClientConfig = t.TLSClientConfig.Clone()
	}
	t.TLSClientConfig.NextProtos = []string{"http/1.1"}
	return t
}

func cooldownActive(until *atomic.Int64) bool {
	u := until.Load()
	return u > 0 && time.Now().UnixNano() < u
}

func setCooldown(until *atomic.Int64, d time.Duration) {
	if d <= 0 {
		d = time.Minute
	}
	if d > maxCooldown {
		d = maxCooldown
	}
	until.Store(time.Now().Add(d).UnixNano())
}

func (c *Client) markBraveLimited() {
	if c != nil {
		setCooldown(&c.braveUntil, braveCooldown)
	}
}

func (c *Client) braveSkipped() bool {
	return c != nil && cooldownActive(&c.braveUntil)
}

func (c *Client) markDDGLimited() {
	if c != nil {
		setCooldown(&c.ddgUntil, ddgCooldown)
	}
}

func (c *Client) ddgSkipped() bool {
	return c != nil && cooldownActive(&c.ddgUntil)
}

func (c *Client) markBingLimited() {
	if c != nil {
		setCooldown(&c.bingUntil, bingCooldown)
	}
}

func (c *Client) bingSkipped() bool {
	return c != nil && cooldownActive(&c.bingUntil)
}

func (c *Client) markIndexLimited(name string) {
	switch name {
	case "duckduckgo":
		c.markDDGLimited()
	case "bing":
		c.markBingLimited()
	case "brave":
		c.markBraveLimited()
	}
}

func (c *Client) markHostLimited(host string, retryAfter time.Duration) {
	if c == nil {
		return
	}
	h := strings.ToLower(host)
	switch {
	case strings.Contains(h, "duckduckgo"):
		if retryAfter <= 0 {
			retryAfter = ddgCooldown
		}
		setCooldown(&c.ddgUntil, retryAfter)
	case strings.Contains(h, "brave"):
		if retryAfter <= 0 {
			retryAfter = braveCooldown
		}
		setCooldown(&c.braveUntil, retryAfter)
	case strings.Contains(h, "bing"):
		if retryAfter <= 0 {
			retryAfter = bingCooldown
		}
		setCooldown(&c.bingUntil, retryAfter)
	}
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}

	return newBrowserHTTPClient(defaultHTTPTimeout)
}

func (c *Client) get(ctx context.Context, rawURL string, extra map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "map-engine/oss-sidecar")
	for k, v := range extra {
		req.Header.Set(k, v)
	}

	return c.do(req)
}

func (c *Client) postJSON(ctx context.Context, rawURL string, payload any, extra map[string]string) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "map-engine/oss-sidecar")
	for k, v := range extra {
		req.Header.Set(k, v)
	}

	return c.do(req)
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, err
	}

	host := strings.ToLower(req.URL.Host)
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

	if resp.StatusCode == http.StatusAccepted && strings.Contains(host, "duckduckgo") {
		c.markHostLimited(host, ddgCooldown)
		return raw, fmt.Errorf("%s: status 202 challenge", host)
	}

	if resp.StatusCode == http.StatusTooManyRequests ||
		(resp.StatusCode == http.StatusForbidden && isPublicIndexHost(host)) {
		c.markHostLimited(host, retryAfter)
		return raw, fmt.Errorf("%s: status %d rate limited", host, resp.StatusCode)
	}

	if resp.StatusCode >= 400 {
		if isPublicIndexHost(host) && (resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusBadGateway) {
			c.markHostLimited(host, retryAfter)
		}
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}

		return raw, fmt.Errorf("%s: status %d %s", req.URL.Host, resp.StatusCode, msg)
	}

	return raw, nil
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

func isPublicIndexHost(host string) bool {
	h := strings.ToLower(host)
	return strings.Contains(h, "duckduckgo") || strings.Contains(h, "brave") || strings.Contains(h, "bing")
}

func (c *Client) sidecarAlive(ctx context.Context, base string) bool {
	if base == "" {
		return false
	}

	probe, cancel := context.WithTimeout(ctx, sidecarProbeWait)
	defer cancel()

	_, err := c.get(probe, strings.TrimRight(base, "/")+"/healthz", nil)

	return err == nil
}

func newBrowserRequest(ctx context.Context, method, rawURL, body string) (*http.Request, error) {
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}

	var req *http.Request
	var err error
	if rdr != nil {
		req, err = http.NewRequestWithContext(ctx, method, rawURL, rdr)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, rawURL, nil)
	}

	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", acceptLanguageFor(ctx))
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-CH-UA", `"Chromium";v="128", "Not;A=Brand";v="24", "Google Chrome";v="128"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)

	return req, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}

	return ""
}

func acceptLanguageFor(ctx context.Context) string {
	r := searchCountry(ctx)
	hl := LangForCountry(r.Code)
	switch hl {
	case "", "zh":
		return "zh-CN,zh;q=0.9,en;q=0.8"
	default:
		return hl + "," + hl + "-" + r.Code + ";q=0.9,en;q=0.8,zh;q=0.5"
	}
}
