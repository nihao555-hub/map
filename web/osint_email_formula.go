package web

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// 外贸地图获客「联系方式」公式（仅依赖 Maps 可得字段：公司名 / 官网域名 / 电话）。
// 实测结论（20 组）：
//  1) 官网深挖 mailto/JSON-LD/联系页 — 精确邮箱主力
//  2) Brave 公开检索 "@domain" — 可补官网未披露的真实邮箱（须滤 Hunter 占位样例）
//  3) HasMX 且仍无邮箱时 — 外贸角色渠道 sales/export/purchase（低置信，不当具名决策人）
//  4) 决策人姓名 + 域名排列组合 — 仅当公开检索精确命中才采纳（禁止盲猜）
//  5) Maps 电话 / WhatsApp — 挂到所有决策人作为可触达渠道
// SMTP:25 在本环境不可用，故不做 RCPT 探测。

var (
	mailtoEmailRe           = regexp.MustCompile(`(?i)mailto:([a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})`)
	obfuscatedAtRe          = regexp.MustCompile(`(?i)\b([a-z0-9._%+\-]{2,40})\s*(?:\[at\]|\(at\)|\s+at\s+)\s*([a-z0-9.\-]+\.[a-z]{2,})\b`)
	jsonLDBlockRe           = regexp.MustCompile(`(?is)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	placeholderEmailLocalRe = regexp.MustCompile(`(?i)^(first(\.|_)?last|first\.middle(last)?|first|last|flast|f\.?last|firstl|j\.?doe|jdoe|jane|john|doe(_jane|_j)?|jane_doe|janedoe|johndoe|john\.doe|j\.?smith|jsmith|johnsmith|name|user|email|test|sample|someone|fullname|yourname|firstname|lastname|n/a|na)(\d*)$`)
	junkEmailLocalRe        = regexp.MustCompile(`(?i)^(and|or|the|for|with|from|only|your|my|this|that|http|https|www|png|jpg|gif|svg|css|js)$`)
)

// tradeRoleLocals 仅在域名有 MX、且公开源无任何可用邮箱时，写入 ExtraEmails 作触达猜测。
// 绝不升成具名决策人；数量刻意压到 2，避免 UI 刷出一排「渠道联系人」。
var tradeRoleLocals = []string{
	"purchase", "sales",
}

// runContactFormulaPass 在 OSINT/决策人初步汇聚后执行，补齐可触达联系方式与架构节点。
func runContactFormulaPass(ctx context.Context, intel *PlaceIntel, place Place) {
	if intel == nil {
		return
	}
	domain := strings.TrimSpace(strings.ToLower(intel.Domain))
	title := strings.TrimSpace(place.Title)

	if domain != "" {
		if published, src := lookupPublishedEmailsBrave(ctx, domain, title); len(published) > 0 {
			intel.ExtraEmails = mergeUnique(intel.ExtraEmails, published)
			intel.Sources = mergeUnique(intel.Sources, []string{src})
			intel.Provider = strings.Trim(intel.Provider+"+brave-mail", "+")
		}
		if intel.HasMX && countUsableEmails(intel.ExtraEmails, domain) == 0 {
			roles := seedTradeRoleEmails(domain)
			if len(roles) > 0 {
				intel.ExtraEmails = mergeUnique(intel.ExtraEmails, roles)
				intel.Sources = mergeUnique(intel.Sources, []string{"email-role-mx"})
				intel.Provider = strings.Trim(intel.Provider+"+role-mail", "+")
			}
		}
	}

	if domain != "" && len(intel.DecisionMakers) > 0 {
		fillDecisionMakerEmails(ctx, intel, domain)
	}

	intel.DecisionMakers = attachMapsContactsToMakers(intel.DecisionMakers, place)
	intel.OrgStructure = mergeOrgUnits(intel.OrgStructure, orgUnitsFromDecisionMakers(intel.DecisionMakers, place.Title))
	if len(intel.Phones) == 0 && (intel.Socials == nil || intel.Socials["whatsapp"] == "") {
		if phones, wa := lookupPublishedPhonesBrave(ctx, place.Title, intel.Domain); len(phones) > 0 || wa != "" {
			intel.Phones = mergeUnique(intel.Phones, phones)
			if wa != "" {
				if intel.Socials == nil {
					intel.Socials = map[string]string{}
				}
				intel.Socials["whatsapp"] = wa
				intel.Phones = mergeUnique(intel.Phones, []string{wa})
			}
			intel.Sources = mergeUnique(intel.Sources, []string{"brave:phone"})
			intel.Provider = strings.Trim(intel.Provider+"+brave-phone", "+")
		}
	}
}

func countUsableEmails(emails []string, domain string) int {
	n := 0
	for _, e := range filterPlaceholderEmails(emails) {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || !strings.Contains(e, "@") {
			continue
		}
		if domain == "" || strings.HasSuffix(e, "@"+domain) || strings.Contains(e, domain) {
			n++
		}
	}
	return n
}

func seedTradeRoleEmails(domain string) []string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil
	}
	out := make([]string, 0, 2)
	for i, local := range tradeRoleLocals {
		if i >= 2 {
			break
		}
		out = append(out, local+"@"+domain)
	}
	return out
}

// extractEmailsFromHTML 官网深挖：明文 + mailto + [at] 混淆 + JSON-LD。
func extractEmailsFromHTML(html, domain string) []string {
	if strings.TrimSpace(html) == "" {
		return nil
	}
	found := emailFindRe.FindAllString(html, -1)
	for _, m := range mailtoEmailRe.FindAllStringSubmatch(html, -1) {
		if len(m) > 1 {
			found = append(found, m[1])
		}
	}
	for _, m := range obfuscatedAtRe.FindAllStringSubmatch(html, -1) {
		if len(m) > 2 {
			found = append(found, m[1]+"@"+m[2])
		}
	}
	for _, block := range jsonLDBlockRe.FindAllStringSubmatch(html, -1) {
		if len(block) > 1 {
			found = append(found, emailFindRe.FindAllString(block[1], -1)...)
		}
	}
	found = filterPlaceholderEmails(filterPublicEmails(found, domain))
	if domain == "" {
		return found
	}
	// 优先保留目标域名 / 同品牌域名，避免正文里的第三方邮箱污染
	base := strings.Split(domain, ".")[0]
	var preferred []string
	for _, e := range found {
		host := e
		if i := strings.Index(e, "@"); i >= 0 {
			host = e[i+1:]
		}
		if host == domain || strings.HasSuffix(host, "."+domain) ||
			(base != "" && len(base) >= 4 && strings.Contains(host, base)) {
			preferred = append(preferred, e)
		}
	}
	if len(preferred) > 0 {
		return uniqueStrings(preferred)
	}
	return found
}

func filterPlaceholderEmails(in []string) []string {
	var out []string
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(strings.Trim(e, `\"'`)))
		e = strings.TrimSuffix(e, `\`)
		if e == "" || !strings.Contains(e, "@") {
			continue
		}
		if strings.HasPrefix(e, ".") || strings.Contains(e, "..") {
			continue
		}
		local, host := e, ""
		if i := strings.Index(e, "@"); i > 0 {
			local = e[:i]
			host = e[i+1:]
		}
		if placeholderEmailLocalRe.MatchString(local) || junkEmailLocalRe.MatchString(local) {
			continue
		}
		if strings.Contains(local, "first") && strings.Contains(local, "last") {
			continue
		}
		if strings.Contains(local, "first.middle") {
			continue
		}
		if host == "irs.gov" || host == "example.com" || strings.HasSuffix(host, ".gov") && !strings.Contains(host, "custom") {
			// 页面正文误抓的政府/样例域名
			continue
		}
		out = append(out, e)
	}
	return uniqueStrings(out)
}

// lookupPublishedEmailsBrave 外贸常用：用公开搜索挖 "@domain" 已披露邮箱。
func lookupPublishedEmailsBrave(ctx context.Context, domain, title string) ([]string, string) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil, ""
	}
	queries := []string{
		fmt.Sprintf(`"@%s" (email OR contact OR sales OR export OR purchase OR enquiry)`, domain),
	}
	if brand := companyBrandToken(title); brand != "" {
		queries = append(queries, fmt.Sprintf(`"%s" ("@%s" OR email OR contact) (sales OR export OR info OR purchase)`, brand, domain))
	}
	var found []string
	for _, q := range queries {
		html, err := fetchBraveHTML(ctx, q)
		if err != nil || html == "" {
			continue
		}
		found = append(found, extractDomainEmailsFromSearchHTML(html, domain)...)
	}
	found = filterPlaceholderEmails(found)
	if len(found) > 12 {
		found = found[:12]
	}
	if len(found) == 0 {
		return nil, ""
	}
	return found, "brave:email"
}

// lookupPublishedPhonesBrave 无 Maps/官网电话时，用公开检索补电话/WhatsApp。
func lookupPublishedPhonesBrave(ctx context.Context, title, domain string) ([]string, string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, ""
	}
	brand := companyBrandToken(title)
	queries := []string{
		fmt.Sprintf(`"%s" (phone OR WhatsApp OR "wa.me" OR Tel OR Telephone OR Hubungi)`, title),
	}
	if brand != "" && !strings.EqualFold(brand, title) {
		queries = append(queries, fmt.Sprintf(`"%s" (phone OR WhatsApp OR "wa.me" OR "+62" OR "+60")`, brand))
	}
	if domain != "" {
		queries = append(queries, fmt.Sprintf(`site:%s (phone OR WhatsApp OR "wa.me" OR tel:)`, domain))
	}
	var phones []string
	wa := ""
	for _, q := range queries {
		html, err := fetchBraveHTML(ctx, q)
		if err != nil || html == "" {
			continue
		}
		p, w, _ := harvestContactChannelsFromHTML(html)
		phones = mergeUnique(phones, p)
		if wa == "" && w != "" {
			wa = w
		}
	}
	phones = filterPhones(phones)
	if len(phones) > 5 {
		phones = phones[:5]
	}
	return phones, wa
}

func extractDomainEmailsFromSearchHTML(html, domain string) []string {
	var out []string
	for _, e := range emailFindRe.FindAllString(html, -1) {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		host := e
		if i := strings.Index(e, "@"); i >= 0 {
			host = e[i+1:]
		}
		if host == domain || strings.HasSuffix(host, "."+domain) {
			out = append(out, e)
			continue
		}
		// 同品牌旁路域名（如 gramedia.id vs gramedia.com）
		base := strings.Split(domain, ".")[0]
		if base != "" && len(base) >= 4 && strings.Contains(host, base) {
			out = append(out, e)
		}
	}
	return out
}

func fetchBraveHTML(ctx context.Context, q string) (string, error) {
	u := "https://search.brave.com/search?q=" + url.QueryEscape(q)
	var (
		body    string
		lastErr error
	)
	err := withSearchEgress(ctx, func(node string) error {
		raw, code, err := httpGetSearch(ctx, u, 14*time.Second)
		if err != nil {
			lastErr = err
			return err
		}
		if searchHTMLLooksBlocked(raw, code) {
			lastErr = fmt.Errorf("brave status %d via %s", code, node)
			return lastErr
		}
		body = raw
		return nil
	})
	if body != "" {
		return body, nil
	}
	// 无代理 / Clash 不可用时直连兜底
	raw, code, err2 := httpGetDirect(ctx, u, 12*time.Second)
	if err2 == nil && code < 400 && !searchHTMLLooksBlocked(raw, code) {
		return raw, nil
	}
	if err != nil {
		return "", err
	}
	if lastErr != nil {
		return "", lastErr
	}
	if err2 != nil {
		return "", err2
	}
	return "", fmt.Errorf("brave status %d", code)
}

func fillDecisionMakerEmails(ctx context.Context, intel *PlaceIntel, domain string) {
	pattern := detectEmailPattern(intel.ExtraEmails, domain)
	budget, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()

	for i := range intel.DecisionMakers {
		d := &intel.DecisionMakers[i]
		if strings.TrimSpace(d.Email) != "" {
			continue
		}
		first, last := splitPersonName(d.Name)
		if first == "" {
			continue
		}
		candidates := permutePersonEmails(first, last, domain)
		if pattern != "" {
			if e := applyEmailPattern(pattern, first, last, domain); e != "" {
				candidates = append([]string{e}, candidates...)
			}
		}
		candidates = uniqueStrings(candidates)
		if len(candidates) > 6 {
			candidates = candidates[:6]
		}
		confirmed := confirmEmailsOnBrave(budget, candidates)
		if len(confirmed) == 0 {
			continue
		}
		d.Email = confirmed[0]
		d.Source = strings.Trim(d.Source+"+email-formula", "+")
		d.Evidence = truncateRunes(firstNonEmpty(d.Evidence, "public web hit for guessed mailbox"), 140)
		if d.Confidence == "" || d.Confidence == "low" {
			d.Confidence = "medium"
		}
		intel.ExtraEmails = mergeUnique(intel.ExtraEmails, confirmed[:1])
	}
}

func splitPersonName(name string) (first, last string) {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "Dr.")
	name = strings.TrimPrefix(name, "Mr.")
	name = strings.TrimPrefix(name, "Mrs.")
	name = strings.TrimPrefix(name, "Ms.")
	name = strings.Join(strings.Fields(name), " ")
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return strings.ToLower(parts[0]), ""
	}
	return strings.ToLower(parts[0]), strings.ToLower(parts[len(parts)-1])
}

func permutePersonEmails(first, last, domain string) []string {
	first = sanitizeEmailLocalToken(first)
	last = sanitizeEmailLocalToken(last)
	domain = strings.TrimSpace(strings.ToLower(domain))
	if first == "" || domain == "" {
		return nil
	}
	var locals []string
	if last != "" {
		locals = []string{
			first + "." + last,
			first + last,
			string(first[0]) + last,
			first + "_" + last,
			first + "-" + last,
			first,
		}
	} else {
		locals = []string{first}
	}
	out := make([]string, 0, len(locals))
	for _, local := range locals {
		if local == "" || placeholderEmailLocalRe.MatchString(local) {
			continue
		}
		out = append(out, local+"@"+domain)
	}
	return out
}

func sanitizeEmailLocalToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, s)
	return s
}

func detectEmailPattern(emails []string, domain string) string {
	domain = strings.TrimSpace(strings.ToLower(domain))
	counts := map[string]int{}
	for _, e := range filterPlaceholderEmails(emails) {
		e = strings.ToLower(strings.TrimSpace(e))
		if !strings.HasSuffix(e, "@"+domain) {
			continue
		}
		local := emailLocal(e)
		if isGenericOfficeEmailLocal(local) || !isPersonLikeEmailLocal(local) {
			continue
		}
		switch {
		case strings.Contains(local, "."):
			counts["first.last"]++
		case strings.Contains(local, "_"):
			counts["first_last"]++
		case strings.Contains(local, "-"):
			counts["first-last"]++
		default:
			if len(local) >= 2 {
				counts["firstlast_or_flast"]++
			}
		}
	}
	best, bestN := "", 0
	for k, n := range counts {
		if n > bestN {
			best, bestN = k, n
		}
	}
	if bestN < 1 {
		return ""
	}
	return best
}

func applyEmailPattern(pattern, first, last, domain string) string {
	first = sanitizeEmailLocalToken(first)
	last = sanitizeEmailLocalToken(last)
	if first == "" || domain == "" {
		return ""
	}
	var local string
	switch pattern {
	case "first.last":
		if last == "" {
			return ""
		}
		local = first + "." + last
	case "first_last":
		if last == "" {
			return ""
		}
		local = first + "_" + last
	case "first-last":
		if last == "" {
			return ""
		}
		local = first + "-" + last
	case "firstlast_or_flast":
		if last == "" {
			return first + "@" + domain
		}
		local = first + last
	default:
		return ""
	}
	return local + "@" + domain
}

func confirmEmailsOnBrave(ctx context.Context, emails []string) []string {
	var out []string
	for _, e := range emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || isPlaceholderEmail(e) {
			continue
		}
		html, err := fetchBraveHTML(ctx, `"`+e+`"`)
		if err != nil || html == "" {
			continue
		}
		low := strings.ToLower(html)
		if !strings.Contains(low, e) {
			continue
		}
		// 排除纯邮箱格式说明页噪音：若只有 first/last 样例上下文则跳过
		if looksLikePatternDemoPage(low, e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func isPlaceholderEmail(e string) bool {
	e = strings.ToLower(strings.TrimSpace(e))
	if i := strings.Index(e, "@"); i > 0 {
		return placeholderEmailLocalRe.MatchString(e[:i])
	}
	return true
}

func looksLikePatternDemoPage(html, email string) bool {
	// Hunter / 格式生成器页面常见簇
	demoHints := []string{
		"email format", "email pattern", "first.last@", "flast@", "most common email",
		"email permutat", "guess email", "email format finder",
	}
	hits := 0
	for _, h := range demoHints {
		if strings.Contains(html, h) {
			hits++
		}
	}
	if hits >= 2 {
		return true
	}
	// 同页出现多个经典占位邮箱 → 模板页
	placeholders := 0
	for _, p := range []string{"first.last@", "jdoe@", "john.doe@", "jane.doe@", "flast@"} {
		if strings.Contains(html, p) {
			placeholders++
		}
	}
	return placeholders >= 2 && !strings.Contains(html, "mailto:"+email)
}

func attachMapsContactsToMakers(makers []DecisionMaker, place Place) []DecisionMaker {
	phone := firstNonEmpty(place.WhatsApp, place.Phone)
	wa := place.WhatsApp
	if phone == "" && wa == "" {
		return makers
	}
	for i := range makers {
		if makers[i].Phone == "" {
			makers[i].Phone = phone
		}
		if makers[i].WhatsApp == "" && wa != "" {
			makers[i].WhatsApp = wa
		}
	}
	return makers
}

// orgUnitsFromDecisionMakers 只为「有真名的决策人」生成岗位节点。
//
// 以前对每个联系人条目都建一个以店名为名的节点，实测 354 家里造出 84 个
// 「店名 = executive，证据 = Pemilik」的自指节点，架构 Tab 全是占位。
func orgUnitsFromDecisionMakers(makers []DecisionMaker, company string) []OrgUnit {
	company = strings.TrimSpace(company)
	buckets := map[string]string{} // role -> 真名

	for _, d := range makers {
		if !QualifiesAsDecisionMaker(d, company) {
			continue
		}

		role := classifyOrgRole(firstNonEmpty(d.Title, d.Headline))
		if role == "" {
			continue
		}

		if _, ok := buckets[role]; !ok {
			buckets[role] = d.Name
		}
	}

	var out []OrgUnit

	for role, name := range buckets {
		out = append(out, OrgUnit{
			Name:     name,
			Role:     role,
			Evidence: "named decision maker at " + company,
		})
	}

	return out
}

func classifyOrgRole(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return ""
	}
	switch {
	case strings.Contains(t, "purchas"), strings.Contains(t, "procurement"),
		strings.Contains(t, "buyer"), strings.Contains(t, "sourcing"),
		strings.Contains(t, "import"):
		return "procurement"
	case strings.Contains(t, "ceo"), strings.Contains(t, "founder"),
		strings.Contains(t, "owner"), strings.Contains(t, "direktur"),
		strings.Contains(t, "director"), strings.Contains(t, "president"),
		strings.Contains(t, "pemilik"), strings.Contains(t, "pendiri"):
		return "executive"
	case strings.Contains(t, "sales"), strings.Contains(t, "export"),
		strings.Contains(t, "commercial"), strings.Contains(t, "business development"):
		return "sales-export"
	case strings.Contains(t, "supply chain"), strings.Contains(t, "operations"),
		strings.Contains(t, "logistics"):
		return "operations"
	case strings.Contains(t, "marketing"):
		return "marketing"
	default:
		return ""
	}
}

func mergeOrgUnits(base, extra []OrgUnit) []OrgUnit {
	if len(extra) == 0 {
		return base
	}
	seen := map[string]bool{}
	for _, u := range base {
		key := strings.ToLower(u.Role + "|" + u.Name)
		seen[key] = true
	}
	for _, u := range extra {
		key := strings.ToLower(u.Role + "|" + u.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		base = append(base, u)
	}
	return base
}
