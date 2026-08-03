package gmaps

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
	"github.com/gosom/scrapemate"
	"github.com/mcnijman/go-emailaddress"

	"github.com/gosom/google-maps-scraper/exiter"
)

const (
	emailJobTimeout       = 18 * time.Second
	emailFollowBudget     = 20 * time.Second
	emailMaxFollowPages   = 10
	emailMaxResponseBytes = 512 << 10
	emailBrowserUA        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
)

var (
	obfuscatedEmailRe = regexp.MustCompile(`(?i)([a-z0-9._%+\-]+)\s*(?:\[at\]|\(at\)|\{at\}|\sat\s)\s*([a-z0-9.\-]+)\s*(?:\[dot\]|\(dot\)|\{dot\}|\sdot\s)\s*([a-z]{2,})`)
	cloudflareEmailRe = regexp.MustCompile(`data-cfemail=["']([0-9a-fA-F]+)["']`)
	jsonLDEmailRe     = regexp.MustCompile(`(?i)"email"\s*:\s*"([^"]+@[^"]+)"`)
	contactPathHints  = []string{
		"/contact", "/contact-us", "/contactus", "/about", "/about-us", "/aboutus",
		"/privacy", "/privacy-policy", "/impressum", "/imprint", "/support",
		"/get-in-touch", "/reach-us", "/kontakt",
		// 印尼/东南亚常见联系页
		"/kontak", "/hubungi-kami", "/hubungi", "/contact-me",
		"/contacto", "/contatti", "/nous-contacter",
	}
	contactLinkRe = regexp.MustCompile(`(?i)contact|about|privacy|impressum|imprint|support|get-in-touch|kontakt|kontak|hubungi|legal|team`)
	emailJunkSubstrings = []string{
		"sentry.io", "example.com", "domain.com", "email.com", "yourdomain",
		"localhost", "schema.org", "w3.org", "googleapis", "gstatic.com",
		"wixpress.com", "cloudflare", "github.com", "jquery", "sentry",
		"webpack", "facebook.com", "google.com", "apple.com", "microsoft.com",
		"yelp.com", "tripadvisor", "squarespace", "godaddy", "latofonts",
		"indiantypefoundry", "impallari@", "mysite.com", "test.com",
		"user@domain", "name@email", "johndoe", "john.doe", "example@",
		"abc@xyz", "email@email", "test@test", "foo@bar",
		"yoursite.com", "anthropic.com", "gdprlocal.com", "linktr.ee",
		"wix.com", "sentry.io", "noreply@", "no-reply@",
		"addresshere.com", "support@support.com", "blackbox.ai",
		"enteryour@", "your@email",
	}
	emailJunkSuffixes = []string{
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".css", ".js", ".map",
	}
)

type EmailExtractJobOptions func(*EmailExtractJob)

// EmailExtractJob fetches a merchant website and extracts public emails.
type EmailExtractJob struct {
	scrapemate.Job

	Entry                   *Entry
	ExitMonitor             exiter.Exiter
	WriterManagedCompletion bool
}

// NewEmailJob creates an email extraction job for the merchant website.
func NewEmailJob(parentID string, entry *Entry, opts ...EmailExtractJobOptions) *EmailExtractJob {
	// 与 PlaceJob 同级（Medium）：避免被地点详情饿死，联系方式覆盖率崩盘。
	const defaultPrio = scrapemate.PriorityMedium

	job := EmailExtractJob{
		Job: scrapemate.Job{
			ID:         uuid.New().String(),
			ParentID:   parentID,
			Method:     "GET",
			URL:        normalizeGoogleURL(entry.WebSite),
			MaxRetries: 2,
			Priority:   defaultPrio,
			Timeout:    emailJobTimeout,
			Headers: map[string]string{
				"User-Agent":      emailBrowserUA,
				"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
				"Accept-Language": "en-US,en;q=0.9,id;q=0.8",
			},
		},
	}

	job.Entry = entry

	for _, opt := range opts {
		opt(&job)
	}

	return &job
}

// WithEmailJobExitMonitor attaches an exit monitor to the email job.
func WithEmailJobExitMonitor(exitMonitor exiter.Exiter) EmailExtractJobOptions {
	return func(j *EmailExtractJob) {
		j.ExitMonitor = exitMonitor
	}
}

// WithEmailJobWriterManagedCompletion marks completion as writer-managed.
func WithEmailJobWriterManagedCompletion() EmailExtractJobOptions {
	return func(j *EmailExtractJob) {
		j.WriterManagedCompletion = true
	}
}

// Process extracts emails from the fetched homepage and, when needed, a few
// high-intent contact/about/privacy pages on the same host.
func (j *EmailExtractJob) Process(ctx context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
	}()

	defer func() {
		if j.ExitMonitor != nil && !j.WriterManagedCompletion {
			j.ExitMonitor.IncrPlacesCompleted(1)
		}
	}()

	log := scrapemate.GetLoggerFromContext(ctx)
	log.Info("Processing email job", "url", j.URL)

	emails := collectEmailsFromResponse(resp)
	whatsapp := ""
	var social SocialLinks
	if resp != nil {
		whatsapp = extractWhatsApp(resp.Body)
		social = extractSocialFromHTML(resp.Body)
	}

	baseURL := j.URL
	if resp != nil && resp.URL != "" {
		baseURL = resp.URL
	}

	// 为联系方式覆盖率：缺邮箱 / WhatsApp / 任一社媒时都跟进联系页（不再因已有邮箱短路）
	needFollow := len(emails) == 0 ||
		whatsapp == "" ||
		socialEmpty(social) ||
		(resp != nil && resp.Error != nil)
	if needFollow {
		followURLs := alternateEmailURLs(baseURL)
		if resp == nil || resp.Error == nil {
			followURLs = append(followURLs, discoverEmailFollowURLs(baseURL, resp)...)
		}
		followURLs = uniqueURLs(followURLs)
		if len(followURLs) > 0 {
			extraMails, extraWA, extraSocial := fetchContactsFromURLs(ctx, followURLs)
			emails = mergeEmails(emails, extraMails)
			if whatsapp == "" {
				whatsapp = extraWA
			}
			mergeSocialLinks(&social, extraSocial)
		}
	}

	j.Entry.Emails = filterEmails(emails)
	j.Entry.WhatsApp = whatsapp
	j.Entry.PromoteSocialFromMapsFields()
	j.Entry.mergeSocial(social)
	j.Entry.FillWhatsAppFromPhone()

	return j.Entry, nil, nil
}

func socialEmpty(s SocialLinks) bool {
	return s.Facebook == "" && s.Instagram == "" && s.LinkedIn == "" &&
		s.Twitter == "" && s.TikTok == "" && s.YouTube == "" &&
		s.Telegram == "" && s.Pinterest == ""
}

var (
	waMeRe     = regexp.MustCompile(`(?i)(?:https?://)?(?:wa\.me/|api\.whatsapp\.com/send\?[^"'>\s]*phone=)(\+?\d{8,15})`)
	waDigitsRe = regexp.MustCompile(`(?i)whatsapp[^0-9+]{0,24}(\+?\d[\d\s\-()]{7,18}\d)`)
)

func extractWhatsApp(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	text := string(body)
	// 解码常见 HTML/URL 转义，才能匹配 api.whatsapp.com%2Fsend%3Fphone%3D62…
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "%2F", "/")
	text = strings.ReplaceAll(text, "%2f", "/")
	text = strings.ReplaceAll(text, "%3F", "?")
	text = strings.ReplaceAll(text, "%3f", "?")
	text = strings.ReplaceAll(text, "%3D", "=")
	text = strings.ReplaceAll(text, "%3d", "=")
	text = strings.ReplaceAll(text, "%20", "")
	text = strings.ReplaceAll(text, "%2B", "+")
	text = strings.ReplaceAll(text, "%2b", "+")

	lower := strings.ToLower(text)
	// 群邀请链接不含个人手机号，避免把 invite code 数字当成 WA
	if strings.Contains(lower, "chat.whatsapp.com/") {
		text = regexp.MustCompile(`(?i)https?://chat\.whatsapp\.com/[^\s"'<>]+`).ReplaceAllString(text, "")
	}

	if m := waMeRe.FindStringSubmatch(text); len(m) >= 2 {
		return normalizeWhatsApp(m[1])
	}
	if m := waDigitsRe.FindStringSubmatch(text); len(m) >= 2 {
		return normalizeWhatsApp(m[1])
	}

	return ""
}

// extractWhatsAppFromURL 从 wa.me / api.whatsapp.com 链接本身拆手机号。
func extractWhatsAppFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if wa := extractWhatsApp([]byte(raw)); wa != "" {
		return wa
	}
	// wa.me/+62%20811-... 或 wa.me/62811...
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "wa.me" || strings.HasSuffix(host, ".wa.me") {
		path := strings.Trim(u.Path, "/")
		if path != "" {
			return normalizeWhatsApp(path)
		}
	}
	if strings.Contains(host, "whatsapp.com") {
		if phone := u.Query().Get("phone"); phone != "" {
			return normalizeWhatsApp(phone)
		}
	}

	return ""
}

func normalizeWhatsApp(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}

	digits := b.String()
	// 过短/过长丢掉；8 位且像年份日期(20230830)的也丢掉
	if len(digits) < 10 || len(digits) > 15 {
		return ""
	}
	if len(digits) == 8 && (strings.HasPrefix(digits, "20") || strings.HasPrefix(digits, "19")) {
		return ""
	}
	// 印尼：只要移动号段 628…；拒绝明显非电话（群邀请杂数等）
	if strings.HasPrefix(digits, "62") && !strings.HasPrefix(digits, "628") {
		return ""
	}
	// 以 95/96/97/98/99 开头且很长的更像邀请码残留，不是手机号
	if len(digits) >= 14 && (strings.HasPrefix(digits, "95") || strings.HasPrefix(digits, "96") ||
		strings.HasPrefix(digits, "97") || strings.HasPrefix(digits, "98") || strings.HasPrefix(digits, "99")) {
		return ""
	}

	return "+" + digits
}

// alternateEmailURLs 在官网首页拉失败时仍值得一试的候选地址
func alternateEmailURLs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil
	}

	host := parsed.Host
	var out []string

	// http → https
	if strings.EqualFold(parsed.Scheme, "http") {
		u := *parsed
		u.Scheme = "https"
		out = append(out, u.String())
	}

	// 去 www / 加 www
	bare := strings.TrimPrefix(strings.ToLower(host), "www.")
	for _, h := range []string{bare, "www." + bare} {
		for _, path := range contactPathHints {
			out = append(out, "https://"+h+path)
		}
		out = append(out, "https://"+h+"/")
	}

	return out
}

func uniqueURLs(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))

	for _, u := range in {
		key := strings.TrimRight(strings.ToLower(strings.TrimSpace(u)), "/")
		if key == "" || seen[key] {
			continue
		}

		seen[key] = true
		out = append(out, u)
		if len(out) >= emailMaxFollowPages+4 {
			break
		}
	}

	return out
}

// ProcessOnFetchError keeps the place result even when the website fetch fails.
func (j *EmailExtractJob) ProcessOnFetchError() bool {
	return true
}

func collectEmailsFromResponse(resp *scrapemate.Response) []string {
	if resp == nil || resp.Error != nil {
		return nil
	}

	var emails []string

	if doc, ok := resp.Document.(*goquery.Document); ok {
		emails = mergeEmails(emails, docEmailExtractor(doc))
	}

	emails = mergeEmails(emails, regexEmailExtractor(resp.Body))
	emails = mergeEmails(emails, obfuscatedEmailExtractor(resp.Body))
	emails = mergeEmails(emails, cloudflareEmailExtractor(resp.Body))
	emails = mergeEmails(emails, jsonLDEmailExtractor(resp.Body))

	return emails
}

func jsonLDEmailExtractor(body []byte) []string {
	if len(body) == 0 {
		return nil
	}

	matches := jsonLDEmailRe.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]bool{}
	var emails []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		if email, err := getValidEmail(string(m[1])); err == nil && !seen[email] {
			emails = append(emails, email)
			seen[email] = true
		}
	}

	return emails
}

func docEmailExtractor(doc *goquery.Document) []string {
	seen := map[string]bool{}

	var emails []string

	doc.Find("a[href^='mailto:']").Each(func(_ int, s *goquery.Selection) {
		mailto, exists := s.Attr("href")
		if !exists {
			return
		}

		value := strings.TrimPrefix(mailto, "mailto:")
		if idx := strings.IndexAny(value, "?&"); idx >= 0 {
			value = value[:idx]
		}

		if email, err := getValidEmail(value); err == nil {
			if !seen[email] {
				emails = append(emails, email)
				seen[email] = true
			}
		}
	})

	return emails
}

func regexEmailExtractor(body []byte) []string {
	seen := map[string]bool{}

	var emails []string

	addresses := emailaddress.Find(body, false)
	for i := range addresses {
		mail := addresses[i].String()
		if !seen[mail] {
			emails = append(emails, mail)
			seen[mail] = true
		}
	}

	return emails
}

func obfuscatedEmailExtractor(body []byte) []string {
	matches := obfuscatedEmailRe.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]bool{}

	var emails []string

	for _, m := range matches {
		if len(m) < 4 {
			continue
		}

		raw := string(m[1]) + "@" + string(m[2]) + "." + string(m[3])
		if email, err := getValidEmail(raw); err == nil && !seen[email] {
			emails = append(emails, email)
			seen[email] = true
		}
	}

	return emails
}

func cloudflareEmailExtractor(body []byte) []string {
	matches := cloudflareEmailRe.FindAllSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := map[string]bool{}

	var emails []string

	for _, m := range matches {
		decoded := decodeCloudflareEmail(string(m[1]))
		if email, err := getValidEmail(decoded); err == nil && !seen[email] {
			emails = append(emails, email)
			seen[email] = true
		}
	}

	return emails
}

func decodeCloudflareEmail(encoded string) string {
	raw, err := hex.DecodeString(encoded)
	if err != nil || len(raw) < 2 {
		return ""
	}

	key := raw[0]
	out := make([]byte, len(raw)-1)

	for i := 1; i < len(raw); i++ {
		out[i-1] = raw[i] ^ key
	}

	return string(out)
}

func getValidEmail(s string) (string, error) {
	email, err := emailaddress.Parse(strings.TrimSpace(s))
	if err != nil {
		return "", err
	}

	return strings.ToLower(email.String()), nil
}

func filterEmails(emails []string) []string {
	if len(emails) == 0 {
		return nil
	}

	seen := map[string]bool{}

	var out []string

	for _, raw := range emails {
		email, err := getValidEmail(raw)
		if err != nil {
			continue
		}

		if seen[email] || isJunkEmail(email) {
			continue
		}

		seen[email] = true
		out = append(out, email)
	}

	return out
}

func isJunkEmail(email string) bool {
	lower := strings.ToLower(email)

	for _, suffix := range emailJunkSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}

	for _, junk := range emailJunkSubstrings {
		if strings.Contains(lower, junk) {
			return true
		}
	}

	at := strings.LastIndex(lower, "@")
	if at <= 0 || at == len(lower)-1 {
		return true
	}

	local := lower[:at]
	domain := lower[at+1:]

	// 过长本地部分 / 含大量无意义字符：常见于追踪像素伪造邮箱
	if len(local) >= 32 {
		compact := strings.Map(func(r rune) rune {
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
				return r
			}
			return -1
		}, local)
		if len(compact) >= 24 {
			return true
		}
	}
	if len(local) >= 40 {
		return true
	}

	// 域名过怪（无点、或 TLD 不像常规邮箱）
	if !strings.Contains(domain, ".") || strings.HasSuffix(domain, ".nacsi") ||
		strings.Contains(domain, "w-whpeg") {
		return true
	}

	return false
}

func isHexish(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}

	return true
}

func mergeEmails(dst, src []string) []string {
	if len(src) == 0 {
		return dst
	}

	seen := map[string]bool{}
	for _, e := range dst {
		seen[strings.ToLower(e)] = true
	}

	for _, e := range src {
		key := strings.ToLower(e)
		if seen[key] {
			continue
		}

		seen[key] = true
		dst = append(dst, e)
	}

	return dst
}

func discoverEmailFollowURLs(baseURL string, resp *scrapemate.Response) []string {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return nil
	}

	origin := parsed.Scheme + "://" + parsed.Host
	seen := map[string]bool{}

	var out []string

	add := func(raw string) {
		u, err := url.Parse(raw)
		if err != nil {
			return
		}

		if !u.IsAbs() {
			u = originURL(parsed).ResolveReference(u)
		}

		if !sameHost(parsed.Host, u.Host) {
			return
		}

		u.Fragment = ""
		u.RawQuery = ""
		key := strings.TrimRight(u.String(), "/")

		if seen[key] {
			return
		}

		seen[key] = true
		out = append(out, u.String())
	}

	for _, path := range contactPathHints {
		add(origin + path)
	}

	if resp != nil {
		if doc, ok := resp.Document.(*goquery.Document); ok {
			doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
				href, exists := s.Attr("href")
				if !exists || href == "" {
					return
				}

				text := strings.TrimSpace(s.Text())
				if contactLinkRe.MatchString(href) || contactLinkRe.MatchString(text) {
					add(href)
				}
			})
		}
	}

	// Cap follow list; homepage itself is already fetched.
	home := strings.TrimRight(originURL(parsed).String(), "/")
	filtered := make([]string, 0, emailMaxFollowPages)

	for _, u := range out {
		if strings.TrimRight(u, "/") == home {
			continue
		}

		filtered = append(filtered, u)
		if len(filtered) >= emailMaxFollowPages {
			break
		}
	}

	return filtered
}

func originURL(u *url.URL) *url.URL {
	return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}
}

func sameHost(a, b string) bool {
	return strings.EqualFold(strings.TrimPrefix(strings.ToLower(a), "www."), strings.TrimPrefix(strings.ToLower(b), "www."))
}

func fetchEmailsFromURLs(ctx context.Context, urls []string) []string {
	mails, _, _ := fetchContactsFromURLs(ctx, urls)

	return mails
}

// fetchContactsFromURLs 并行拉取联系页，提取邮箱、WhatsApp 与社媒链接
func fetchContactsFromURLs(ctx context.Context, urls []string) ([]string, string, SocialLinks) {
	var social SocialLinks
	if len(urls) == 0 {
		return nil, "", social
	}

	ctx, cancel := context.WithTimeout(ctx, emailFollowBudget)
	defer cancel()

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}

			return nil
		},
	}

	type pageHit struct {
		emails   []string
		whatsapp string
		social   SocialLinks
	}

	workers := 3
	if len(urls) < workers {
		workers = len(urls)
	}

	jobs := make(chan string, len(urls))
	hits := make(chan pageHit, len(urls))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for raw := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				body, err := fetchURLBody(ctx, client, raw)
				if err != nil || len(body) == 0 {
					continue
				}

				var h pageHit
				h.emails = mergeEmails(h.emails, regexEmailExtractor(body))
				h.emails = mergeEmails(h.emails, obfuscatedEmailExtractor(body))
				h.emails = mergeEmails(h.emails, cloudflareEmailExtractor(body))
				h.emails = mergeEmails(h.emails, jsonLDEmailExtractor(body))
				if doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body))); err == nil {
					h.emails = mergeEmails(h.emails, docEmailExtractor(doc))
				}
				h.whatsapp = extractWhatsApp(body)
				h.social = extractSocialFromHTML(body)
				hits <- h
			}
		}()
	}

	for _, u := range urls {
		jobs <- u
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(hits)
	}()

	var emails []string
	whatsapp := ""
	for h := range hits {
		emails = mergeEmails(emails, h.emails)
		if whatsapp == "" {
			whatsapp = h.whatsapp
		}
		mergeSocialLinks(&social, h.social)
	}

	return emails, whatsapp, social
}

func fetchURLBody(ctx context.Context, client *http.Client, raw string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", emailBrowserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,id;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, io.EOF
	}

	return io.ReadAll(io.LimitReader(resp.Body, emailMaxResponseBytes))
}

// normalizeGoogleURL extracts the actual target URL from Google redirect URLs.
// Google Maps sometimes returns URLs like "/url?q=http://example.com/&opi=..."
// for external website links.
func normalizeGoogleURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}

	if strings.HasPrefix(rawURL, "/url?q=") {
		fullURL := "https://www.google.com" + rawURL

		parsed, err := url.Parse(fullURL)
		if err != nil {
			return rawURL
		}

		if target := parsed.Query().Get("q"); target != "" {
			return target
		}
	}

	if strings.HasPrefix(rawURL, "/") {
		return "https://www.google.com" + rawURL
	}

	return rawURL
}
