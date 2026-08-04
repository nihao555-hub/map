package web

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// 国内浏览器打不开 lh3.googleusercontent.com / LinkedIn / ContactOut 图片；
// 由本服务代取（走已配置的出站代理），前端只请求同源 /api/v1/media。

const (
	mediaProxyMaxBytes = 2 << 20 // 2 MiB
	mediaProxyTimeout  = 25 * time.Second
)

// mediaAllowedHosts 仅放行图片 CDN，防 SSRF。
var mediaAllowedHosts = map[string]bool{
	"lh3.googleusercontent.com":          true,
	"lh4.googleusercontent.com":          true,
	"lh5.googleusercontent.com":          true,
	"lh6.googleusercontent.com":          true,
	"streetviewpixels-pa.googleapis.com": true,
	"maps.googleapis.com":                true,
	"images.contactout.com":              true,
	"media.licdn.com":                    true,
	"static.licdn.com":                   true,
	"unavatar.io":                        true,
}

// ProxiedMediaURL 把外链图片改写为本站代理地址；非白名单原样返回。
func ProxiedMediaURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "/api/v1/media") {
		return raw
	}

	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return raw
	}

	host := strings.ToLower(u.Hostname())
	if !mediaHostAllowed(host) {
		return raw
	}

	return "/api/v1/media?u=" + url.QueryEscape(raw)
}

func mediaHostAllowed(host string) bool {
	if mediaAllowedHosts[host] {
		return true
	}
	return strings.HasSuffix(host, ".googleusercontent.com") ||
		strings.HasSuffix(host, ".licdn.com") ||
		host == "unavatar.io" || strings.HasSuffix(host, ".unavatar.io")
}

// apiMediaProxy GET /api/v1/media?u=<url>
func (s *Server) apiMediaProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	raw := strings.TrimSpace(r.URL.Query().Get("u"))
	if raw == "" {
		http.Error(w, "missing u", http.StatusBadRequest)
		return
	}

	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}

	host := strings.ToLower(target.Hostname())
	if !mediaHostAllowed(host) {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	if ip := net.ParseIP(host); ip != nil {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}

	client := &http.Client{Timeout: mediaProxyTimeout}
	if p := firstOutboundProxy(); p != "" {
		if pu, err := url.Parse(p); err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(pu)}
		}
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; gmaps-media-proxy/1.0)")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	req.Header.Set("Referer", "https://www.google.com/")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("media proxy fetch: %v", err)
		http.Error(w, "upstream fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		http.Error(w, fmt.Sprintf("upstream %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	ct := resp.Header.Get("Content-Type")
	lowCT := strings.ToLower(ct)
	if ct == "" || (!strings.HasPrefix(lowCT, "image/") && !strings.Contains(lowCT, "octet-stream")) {
		http.Error(w, "not an image", http.StatusBadGateway)
		return
	}
	if !strings.HasPrefix(lowCT, "image/") {
		ct = "image/jpeg"
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	if _, err := io.Copy(w, io.LimitReader(resp.Body, mediaProxyMaxBytes)); err != nil {
		return
	}
}

// rewritePlaceMedia 把商家图片字段改写为同源代理，国内浏览器才能显示。
func rewritePlaceMedia(p *Place) {
	if p == nil {
		return
	}
	p.Thumbnail = ProxiedMediaURL(p.Thumbnail)
	p.StreetViewURL = ProxiedMediaURL(p.StreetViewURL)
	if p.Images != "" {
		p.Images = rewriteMediaURLsInText(p.Images)
	}
}

// rewriteIntelMedia 背调头像同样走代理。
func rewriteIntelMedia(intel *PlaceIntel) {
	if intel == nil {
		return
	}
	for i := range intel.DecisionMakers {
		intel.DecisionMakers[i].Avatar = ProxiedMediaURL(intel.DecisionMakers[i].Avatar)
	}
}

// mediaURLInTextRe 匹配 JSON/文本里的绝对图片 URL。
var mediaURLInTextRe = regexp.MustCompile(`https?://[^\s"'\\]+`)

func rewriteMediaURLsInText(s string) string {
	return mediaURLInTextRe.ReplaceAllStringFunc(s, func(u string) string {
		// 去掉 JSON 里偶发的尾部标点
		u = strings.TrimRight(u, ".,);]")
		return ProxiedMediaURL(u)
	})
}

// firstOutboundProxy 读取可用于出站的代理（优先 MEDIA/AHU，能通 Google 图片）。
func firstOutboundProxy() string {
	for _, k := range []string{
		"MEDIA_PROXY", "AHU_PROXY", "AHU_PROXY_URL",
		"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY",
	} {
		if v := firstProxyLine(os.Getenv(k)); v != "" {
			return v
		}
	}
	// 部署时常把 SOCKS 写在 GMS_PROXIES（多行），取第一条可用
	for _, k := range []string{"GMS_PROXIES", "GMS_PROXY"} {
		if v := firstProxyLine(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func firstProxyLine(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}
