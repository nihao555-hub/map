package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultHTTPTimeout = 45 * time.Second
	maxBodyBytes       = 2 << 20
	defaultTikTokURL   = "http://127.0.0.1:8091"
	defaultF2URL       = "http://127.0.0.1:8092"
)

// Client talks to cloned high-star OSS sidecars. It does not scrape platforms itself.
type Client struct {
	HTTP        *http.Client
	TikTokURL   string
	F2URL       string
	TikHubToken string
}

// OptionsFromEnv wires sidecar base URLs.
//
//	ENGINE_TIKTOK_SIDECAR_URL  davidteather/TikTok-Api (default http://127.0.0.1:8091)
//	ENGINE_F2_SIDECAR_URL      Johnserf-Seed/f2        (default http://127.0.0.1:8092)
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

	return &Client{
		HTTP:        &http.Client{Timeout: timeout},
		TikTokURL:   tiktok,
		F2URL:       f2,
		TikHubToken: strings.TrimSpace(firstNonEmpty(os.Getenv("TIKHUB_API_TOKEN"), os.Getenv("TIKHUB_API_KEY"))),
	}
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}

	return &http.Client{Timeout: defaultHTTPTimeout}
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

	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}

		return raw, fmt.Errorf("%s: status %d %s", req.URL.Host, resp.StatusCode, msg)
	}

	return raw, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}

	return ""
}
