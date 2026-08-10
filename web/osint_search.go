package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 搜索出口：DDG/Brave 机房直连常 403/429；经 Clash 日本/美国节点可拿到 LinkedIn /in/。
// AHU 仍需新加坡节点，因此搜索前后会临时切换 PROXY 组并加锁，避免并发踩踏。

var (
	clashSearchMu      sync.Mutex
	cachedSearchNode   string
	cachedSearchNodeAt time.Time
	searchLinkedInRe   = regexp.MustCompile(`(?i)https?://(?:[a-z]+\.)?linkedin\.com/in/[a-z0-9\-_%]+`)
	searchLinkedInCoRe = regexp.MustCompile(`(?i)https?://(?:[a-z]+\.)?linkedin\.com/company/[a-z0-9\-_%]+`)
	searchAnchorRe     = regexp.MustCompile(`(?is)<a[^>]+href="([^"]*linkedin\.com/in/[^"]+)"[^>]*>(.*?)</a>`)
	braveTitleCleanRe  = regexp.MustCompile(`(?i)^\s*linkedin\b.*$`)
)

const searchNodeCacheTTL = 8 * time.Minute

func searchProxyURL() string {
	for _, k := range []string{
		"SEARCH_PROXY", "CROSSLINKED_PROXY", "AHU_PROXY", "AHU_PROXY_URL",
		"MEDIA_PROXY", "HTTPS_PROXY", "HTTP_PROXY",
	} {
		if v := firstProxyLine(os.Getenv(k)); v != "" {
			low := strings.ToLower(v)
			if strings.HasPrefix(low, "socks5://") || strings.HasPrefix(low, "socks5h://") ||
				strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
				return v
			}
			if strings.Contains(v, ":") && !strings.Contains(v, "/") {
				return "http://" + v
			}
			return v
		}
	}
	return ""
}

func clashAPIBase() string {
	if v := strings.TrimSpace(os.Getenv("CLASH_API")); v != "" {
		return strings.TrimRight(v, "/")
	}
	// 本机 mihomo 默认
	return "http://127.0.0.1:19090"
}

func clashProxyGroup() string {
	if v := strings.TrimSpace(os.Getenv("CLASH_PROXY_GROUP")); v != "" {
		return v
	}
	return "PROXY"
}

func clashDefaultNode() string {
	if v := strings.TrimSpace(os.Getenv("CLASH_DEFAULT_NODE")); v != "" {
		return v
	}
	return "新加坡SG-HY2"
}

func clashSearchNodes() []string {
	if v := strings.TrimSpace(os.Getenv("CLASH_SEARCH_NODE")); v != "" {
		parts := strings.Split(v, ",")
		var out []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{
		"日本JP-HY2",
		"美国LA-优化-GPT",
		"德国-优化",
		"美国LA-优化2-GPT",
		"日本-优化",
		"香港HK-HY2",
		"加拿大-优化",
		"英国-优化-GPT",
		"新加坡-优化-Gemini-GPT",
	}
}

func clashCurrentNode(ctx context.Context) string {
	api := clashAPIBase()
	group := url.PathEscape(clashProxyGroup())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api+"/proxies/"+group, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ""
	}
	var parsed struct {
		Now string `json:"now"`
	}
	if json.NewDecoder(resp.Body).Decode(&parsed) != nil {
		return ""
	}
	return parsed.Now
}

func clashSelectNode(ctx context.Context, node string) error {
	node = strings.TrimSpace(node)
	if node == "" {
		return fmt.Errorf("empty clash node")
	}
	api := clashAPIBase()
	group := url.PathEscape(clashProxyGroup())
	body, _ := json.Marshal(map[string]string{"name": node})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, api+"/proxies/"+group, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("clash select %s: status %d", node, resp.StatusCode)
	}
	return nil
}

// withSearchEgress 在搜索用节点上执行 fn（有 Clash API 时临时切换，结束后还原）。
func withSearchEgress(ctx context.Context, fn func(node string) error) error {
	proxy := searchProxyURL()
	if proxy == "" {
		return fn("")
	}

	clashSearchMu.Lock()
	defer clashSearchMu.Unlock()

	restore := clashDefaultNode()

	var lastErr error
	tried := map[string]bool{}
	nodes := make([]string, 0, 6)
	if cachedSearchNode != "" && time.Since(cachedSearchNodeAt) < searchNodeCacheTTL {
		nodes = append(nodes, cachedSearchNode)
	}
	nodes = append(nodes, clashSearchNodes()...)
	maxTries := 3
	tries := 0
	for _, node := range nodes {
		if node == "" || tried[node] {
			continue
		}
		if tries >= maxTries {
			break
		}
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) < 4*time.Second {
			break
		}
		tried[node] = true
		tries++
		_ = clashSelectNode(ctx, node)
		select {
		case <-ctx.Done():
			_ = clashSelectNode(context.Background(), restore)
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
		if err := fn(node); err != nil {
			lastErr = err
			if cachedSearchNode == node {
				cachedSearchNode = ""
			}
			continue
		}
		cachedSearchNode = node
		cachedSearchNodeAt = time.Now()
		_ = clashSelectNode(context.Background(), restore)
		return nil
	}
	_ = clashSelectNode(context.Background(), restore)
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no working search egress node")
}

func searchHTTPClient(timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: timeout}
	if p := searchProxyURL(); p != "" {
		if pu, err := url.Parse(p); err == nil {
			client.Transport = &http.Transport{
				Proxy:                 http.ProxyURL(pu),
				MaxIdleConns:          16,
				IdleConnTimeout:       30 * time.Second,
				TLSHandshakeTimeout:   8 * time.Second,
				ResponseHeaderTimeout: timeout,
				ForceAttemptHTTP2:     false, // 代理下 HTTP/2 易卡住
			}
		}
	}
	return client
}

func httpGetSearch(ctx context.Context, rawURL string, timeout time.Duration) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := searchHTTPClient(timeout).Do(req)
	if err == nil {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 700<<10))
		body := string(raw)
		if !searchHTMLLooksBlocked(body, resp.StatusCode) {
			return body, resp.StatusCode, nil
		}
		// 429/风控时再试 curl（代理下 Go TLS 有时比 curl 更容易被拦）
		if body, code, err2 := httpGetSearchCurl(ctx, rawURL, timeout, ua); err2 == nil && !searchHTMLLooksBlocked(body, code) {
			return body, code, nil
		}
		return body, resp.StatusCode, nil
	}
	if body, code, err2 := httpGetSearchCurl(ctx, rawURL, timeout, ua); err2 == nil {
		return body, code, nil
	}
	return "", 0, err
}

func httpGetSearchCurl(ctx context.Context, rawURL string, timeout time.Duration, ua string) (string, int, error) {
	sec := int(timeout.Seconds())
	if sec < 5 {
		sec = 5
	}
	args := []string{"-sL", "--max-time", fmt.Sprintf("%d", sec), "-A", ua,
		"-H", "Accept: text/html,application/xhtml+xml;q=0.9,*/*;q=0.8",
		"-H", "Accept-Language: en-US,en;q=0.9",
		"-w", "\n__HTTP_CODE__:%{http_code}",
	}
	if p := searchProxyURL(); p != "" {
		args = append(args, "--proxy", p)
	}
	args = append(args, rawURL)
	cmd := exec.CommandContext(ctx, "curl", args...) //nolint:gosec
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return "", 0, err
	}
	body := string(out)
	code := 0
	if i := strings.LastIndex(body, "\n__HTTP_CODE__:"); i >= 0 {
		fmt.Sscanf(body[i+len("\n__HTTP_CODE__:"):], "%d", &code)
		body = body[:i]
	}
	return body, code, nil
}

type linkedInSearchHit struct {
	URL   string
	Label string
	Kind  string // in | company
}

func extractLinkedInHitsFromHTML(page string) []linkedInSearchHit {
	if page == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []linkedInSearchHit

	add := func(raw, label, kind string) {
		clean := cleanLinkedInURL(html.UnescapeString(raw))
		if clean == "" {
			return
		}
		key := strings.ToLower(clean)
		if seen[key] {
			return
		}
		seen[key] = true
		label = strings.TrimSpace(tagStripRe.ReplaceAllString(html.UnescapeString(label), " "))
		label = strings.Join(strings.Fields(label), " ")
		if braveTitleCleanRe.MatchString(label) && !strings.Contains(label, " - ") && !strings.Contains(label, " | ") {
			label = ""
		}
		out = append(out, linkedInSearchHit{URL: clean, Label: label, Kind: kind})
	}

	for _, m := range searchAnchorRe.FindAllStringSubmatch(page, -1) {
		if len(m) >= 3 {
			add(m[1], m[2], "in")
		}
	}
	for _, m := range searchLinkedInRe.FindAllString(page, -1) {
		add(m, "", "in")
	}
	for _, m := range searchLinkedInCoRe.FindAllString(page, -1) {
		add(m, "", "company")
	}
	// DDG uddg=
	for _, m := range ddgLinkedInInRe.FindAllString(page, -1) {
		add(decodeDDGHref(m), "", "in")
	}
	for _, m := range ddgLinkedInCoRe.FindAllString(page, -1) {
		add(decodeDDGHref(m), "", "company")
	}
	return out
}

func searchHTMLLooksBlocked(body string, status int) bool {
	if status == 429 || status == 403 || status == 503 {
		return true
	}
	if status >= 400 {
		return true
	}
	low := strings.ToLower(body)
	if strings.Contains(low, "error-lite@duckduckgo") {
		return true
	}
	if strings.Contains(low, "sorry/index") || strings.Contains(low, "unusual traffic") {
		return true
	}
	// Brave/Bing 风控页通常很短或无结果结构
	if len(body) < 2000 {
		return true
	}
	return false
}

// fetchSearchHTMLForLinkedIn 多后端拿搜索页：先直连 DDG，失败再经 Clash 搜索节点打 Brave。
func fetchSearchHTMLForLinkedIn(ctx context.Context, q string) (string, string, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", "", fmt.Errorf("empty query")
	}

	// 1) DDG 直连（不占 Clash 锁；偶发可用）
	ddgURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(q)
	if body, code, err := httpGetDirect(ctx, ddgURL, 10*time.Second); err == nil &&
		!searchHTMLLooksBlocked(body, code) &&
		(strings.Contains(body, "result__a") || strings.Contains(strings.ToLower(body), "linkedin.com/in")) {
		return body, "ddg", nil
	}

	// 2) Brave / Bing via search proxy + Clash 节点轮换
	var (
		bestBody string
		bestSrc  string
		lastErr  error
	)
	err := withSearchEgress(ctx, func(node string) error {
		backends := []struct {
			name string
			u    string
		}{
			{"brave", "https://search.brave.com/search?q=" + url.QueryEscape(q)},
			{"bing", "https://www.bing.com/search?q=" + url.QueryEscape(q) + "&setlang=en-us&cc=US&ensearch=1"},
		}
		for _, b := range backends {
			body, code, err := httpGetSearch(ctx, b.u, 18*time.Second)
			if err != nil {
				lastErr = err
				continue
			}
			if searchHTMLLooksBlocked(body, code) {
				lastErr = fmt.Errorf("%s status %d via %s", b.name, code, node)
				continue
			}
			hits := extractLinkedInHitsFromHTML(body)
			nIn := 0
			for _, h := range hits {
				if h.Kind == "in" {
					nIn++
				}
			}
			if nIn > 0 {
				bestBody, bestSrc = body, b.name
				return nil // 成功，停止换节点
			}
			lastErr = fmt.Errorf("%s empty linkedin via %s", b.name, node)
		}
		return lastErr
	})
	if bestBody != "" {
		return bestBody, bestSrc, nil
	}
	if err != nil {
		return "", "", err
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("search backends empty")
}

func httpGetDirect(ctx context.Context, rawURL string, timeout time.Duration) (string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 500<<10))
	return string(raw), resp.StatusCode, nil
}
