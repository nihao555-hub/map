package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 免 key 公开源：RDAP/who-dat、crt.sh、Wayback CDX、Wikipedia、DNS TXT、DuckDuckGo 摘要。

var (
	wikiOfficerRe = regexp.MustCompile(`(?i)(?:founded by|founder[s]?|ceo|chief executive|direktur utama|pendiri|pemilik|owner)\s*(?:is|was|:|：)?\s*([A-Z][A-Za-z.'’\-]+(?:\s+[A-Z][A-Za-z.'’\-]+){1,3})`)
	tagStripRe    = regexp.MustCompile(`<[^>]+>`)
)

// runPublicEnrichment 并行拉取免 key 公开 API（与 CLI/付费源叠加）。
func runPublicEnrichment(ctx context.Context, intel *PlaceIntel, place Place, mu *sync.Mutex) {
	budget, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()

	domain := intel.Domain
	title := place.Title
	website := place.Website
	var wg sync.WaitGroup

	if domain != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hit, emails, err := lookupRDAPWhoDat(budget, domain)
			if err != nil || (hit == nil && len(emails) == 0) {
				return
			}
			mu.Lock()
			applyRDAP(intel, hit, emails)
			mu.Unlock()
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			emails, hosts, err := lookupCRTSH(budget, domain)
			if err != nil {
				return
			}
			mu.Lock()
			applyCRTSH(intel, emails, hosts)
			mu.Unlock()
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			emails, err := lookupDNSTXTEmails(budget, domain)
			if err != nil {
				return
			}
			mu.Lock()
			if len(emails) > 0 {
				intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emails, domain))
				intel.Sources = mergeUnique(intel.Sources, []string{"dns-txt"})
				intel.Provider = strings.Trim(intel.Provider+"+dns", "+")
			}
			mu.Unlock()
		}()
	}

	if website != "" || domain != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seed := website
			if seed == "" {
				seed = "https://" + domain
			}
			emails, makers, urls, err := lookupWaybackTeamPages(budget, seed, domain)
			if err != nil {
				return
			}
			mu.Lock()
			applyWayback(intel, emails, makers, urls)
			mu.Unlock()
		}()
	}

	if title != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			makers, extract, err := lookupWikipediaExtract(budget, title)
			if err != nil {
				return
			}
			mu.Lock()
			applyWikipedia(intel, makers, extract)
			mu.Unlock()
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			makers, err := lookupDuckDuckGoOfficers(budget, title)
			if err != nil {
				return
			}
			mu.Lock()
			if len(makers) > 0 {
				intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, makers)
				intel.Sources = mergeUnique(intel.Sources, []string{"duckduckgo"})
				intel.Provider = strings.Trim(intel.Provider+"+ddg", "+")
			}
			mu.Unlock()
		}()

		// LinkedIn X-Ray 已在 runOSINTEnrichment 开头同步执行（避免被 AHU Clash 锁饿死）。
		// CrossLinked 作为补充；若 X-Ray 已有 /in/ 则跳过，省代理与时间。
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			hasIn := false
			for _, d := range intel.DecisionMakers {
				if strings.Contains(strings.ToLower(d.LinkedIn), "linkedin.com/in/") {
					hasIn = true
					break
				}
			}
			mu.Unlock()
			if hasIn {
				return
			}
			people, err := lookupCrossLinkedEmployees(budget, title, domain)
			if err != nil || len(people) == 0 {
				return
			}
			mu.Lock()
			intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, people)
			intel.Sources = mergeUnique(intel.Sources, []string{"crosslinked"})
			intel.Provider = strings.Trim(intel.Provider+"+crosslinked", "+")
			mu.Unlock()
		}()

		// 海关定公司 → 领英定人（ImportYeti / 美国提单开放数据）
		wg.Add(1)
		go func() {
			defer wg.Done()
			runCustomsEnrichment(budget, intel, place, mu)
		}()
	}

	wg.Wait()
}

func lookupRDAPWhoDat(ctx context.Context, domain string) (*CompanyHit, []string, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil, nil, fmt.Errorf("empty domain")
	}
	// who-dat：归一化 WHOIS/RDAP
	raw, err := httpGetJSON(ctx, "https://who-dat.as93.net/"+url.PathEscape(domain), 10*time.Second)
	if err != nil {
		return lookupRDAPOrg(ctx, domain)
	}
	var parsed struct {
		Domain    string `json:"domain"`
		Registrar struct {
			Name string `json:"name"`
		} `json:"registrar"`
		Contacts struct {
			Registrant struct {
				Name         string `json:"name"`
				Organization string `json:"organization"`
				Email        string `json:"email"`
				Redacted     bool   `json:"redacted"`
			} `json:"registrant"`
		} `json:"contacts"`
		Dates struct {
			Created string `json:"created"`
		} `json:"dates"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return lookupRDAPOrg(ctx, domain)
	}
	var emails []string
	if em := strings.TrimSpace(parsed.Contacts.Registrant.Email); em != "" {
		emails = append(emails, em)
	}
	org := strings.TrimSpace(firstNonEmpty(parsed.Contacts.Registrant.Organization, parsed.Contacts.Registrant.Name))
	hit := &CompanyHit{
		Name:          firstNonEmpty(org, domain),
		CompanyType:   "domain",
		CurrentStatus: "registered",
		Incorporation: parsed.Dates.Created,
		RegistryURL:   "https://rdap.org/domain/" + domain,
		Source:        "rdap/who-dat",
	}
	if parsed.Registrar.Name != "" {
		hit.CurrentStatus = "registrar:" + parsed.Registrar.Name
	}
	if org != "" {
		hit.CompanyType = "registrant"
	}
	return hit, uniqueStrings(emails), nil
}

func lookupRDAPOrg(ctx context.Context, domain string) (*CompanyHit, []string, error) {
	raw, err := httpGetJSON(ctx, "https://rdap.org/domain/"+url.PathEscape(domain), 10*time.Second)
	if err != nil {
		return nil, nil, err
	}
	emails := emailFindRe.FindAllString(string(raw), -1)
	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	hit := &CompanyHit{
		Name:        domain,
		RegistryURL: "https://rdap.org/domain/" + domain,
		Source:      "rdap.org",
	}
	if org := firstStringDeep(parsed, "vcardArray", "fn", "org"); org != "" {
		hit.Name = org
	}
	return hit, uniqueStrings(emails), nil
}

func applyRDAP(intel *PlaceIntel, hit *CompanyHit, emails []string) {
	intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emails, intel.Domain))
	if hit != nil {
		if intel.CompanyRegistry == nil {
			intel.CompanyRegistry = hit
		}
		intel.OrgStructure = append(intel.OrgStructure, OrgUnit{
			Name: firstNonEmpty(hit.Name, intel.Domain), Role: "domain-registrant", Evidence: hit.Source,
		})
		intel.Sources = mergeUnique(intel.Sources, []string{hit.RegistryURL, hit.Source})
	}
	intel.Provider = strings.Trim(intel.Provider+"+rdap", "+")
}

func lookupCRTSH(ctx context.Context, domain string) ([]string, []string, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	u := "https://crt.sh/?q=" + url.QueryEscape("%."+domain) + "&output=json"
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "gmaps-intel/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("crt.sh status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, nil, err
	}
	// crt.sh 有时返回多份 JSON 对象拼接；尽量宽松解析
	var rows []map[string]any
	if json.Unmarshal(raw, &rows) != nil {
		// 单对象或脏数据：仍抽邮箱/主机名
		emails := filterPublicEmails(emailFindRe.FindAllString(string(raw), -1), domain)
		hosts := uniqueStrings(regexp.MustCompile(`(?i)[a-z0-9._-]+\.`+regexp.QuoteMeta(domain)).FindAllString(string(raw), -1))
		return emails, hosts, nil
	}
	var hosts []string
	for _, r := range rows {
		if n, _ := r["name_value"].(string); n != "" {
			for _, line := range strings.Split(n, "\n") {
				line = strings.TrimSpace(strings.ToLower(line))
				if strings.Contains(line, domain) {
					hosts = append(hosts, line)
				}
			}
		}
		if n, _ := r["common_name"].(string); n != "" {
			hosts = append(hosts, strings.ToLower(strings.TrimSpace(n)))
		}
	}
	emails := filterPublicEmails(emailFindRe.FindAllString(string(raw), -1), domain)
	return emails, uniqueStrings(hosts), nil
}

func applyCRTSH(intel *PlaceIntel, emails, hosts []string) {
	intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emails, intel.Domain))
	if len(hosts) > 0 {
		intel.Technologies = mergeUnique(intel.Technologies, []string{fmt.Sprintf("crtsh_hosts:%d", len(hosts))})
		intel.Sources = mergeUnique(intel.Sources, []string{"crt.sh"})
		intel.Provider = strings.Trim(intel.Provider+"+crtsh", "+")
	}
}

func lookupDNSTXTEmails(ctx context.Context, domain string) ([]string, error) {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resolver := &net.Resolver{}
	txts, err := resolver.LookupTXT(rctx, domain)
	if err != nil {
		return nil, err
	}
	blob := strings.Join(txts, "\n")
	return filterPublicEmails(emailFindRe.FindAllString(blob, -1), domain), nil
}

func lookupWaybackTeamPages(ctx context.Context, website, domain string) ([]string, []DecisionMaker, []string, error) {
	host := domain
	if host == "" {
		host = hostDomain(website)
	}
	if host == "" {
		return nil, nil, nil, fmt.Errorf("no host")
	}
	cdx := "https://web.archive.org/cdx/search/cdx?url=" + url.QueryEscape(host+"/*") +
		"&output=json&fl=original,timestamp&collapse=urlkey&limit=40&filter=statuscode:200"
	raw, err := httpGetJSON(ctx, cdx, 12*time.Second)
	if err != nil {
		return nil, nil, nil, err
	}
	var rows [][]string
	if json.Unmarshal(raw, &rows) != nil || len(rows) < 2 {
		return nil, nil, nil, fmt.Errorf("cdx empty")
	}
	var candidates []string
	seen := map[string]bool{}
	for _, row := range rows[1:] { // skip header
		if len(row) < 2 {
			continue
		}
		orig, ts := row[0], row[1]
		if !teamURLRe.MatchString(orig) && !strings.Contains(strings.ToLower(orig), "about") &&
			!strings.Contains(strings.ToLower(orig), "team") && !strings.Contains(strings.ToLower(orig), "contact") {
			continue
		}
		arch := "https://web.archive.org/web/" + ts + "id_/" + orig
		if seen[arch] {
			continue
		}
		seen[arch] = true
		candidates = append(candidates, arch)
		if len(candidates) >= 6 {
			break
		}
	}
	var emails []string
	var makers []DecisionMaker
	var used []string
	for _, u := range candidates {
		body, _, ferr := fetchIntelPage(ctx, u)
		if ferr != nil || len(body) < 80 {
			continue
		}
		used = append(used, u)
		text := stripTags(string(body))
		emails = mergeUnique(emails, filterPublicEmails(emailFindRe.FindAllString(string(body), -1), domain))
		for _, m := range extractPeopleFromText(text, u) {
			m.Source = "wayback"
			makers = append(makers, m)
		}
		if len(used) >= 3 {
			break
		}
	}
	return emails, dedupeDecisionMakers(makers), used, nil
}

func applyWayback(intel *PlaceIntel, emails []string, makers []DecisionMaker, urls []string) {
	intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emails, intel.Domain))
	intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, makers)
	if len(urls) > 0 || len(emails) > 0 || len(makers) > 0 {
		intel.Sources = mergeUnique(intel.Sources, append([]string{"wayback"}, urls...))
		intel.Provider = strings.Trim(intel.Provider+"+wayback", "+")
	}
}

func lookupWikipediaExtract(ctx context.Context, title string) ([]DecisionMaker, string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, "", fmt.Errorf("empty")
	}
	var makers []DecisionMaker
	var extracts []string
	for _, lang := range []string{"en", "id"} {
		// search
		sq := "https://" + lang + ".wikipedia.org/w/api.php?action=query&list=search&srsearch=" +
			url.QueryEscape(title) + "&format=json&srlimit=1"
		sraw, err := httpGetJSON(ctx, sq, 8*time.Second)
		if err != nil {
			continue
		}
		var search struct {
			Query struct {
				Search []struct {
					Title string `json:"title"`
				} `json:"search"`
			} `json:"query"`
		}
		if json.Unmarshal(sraw, &search) != nil || len(search.Query.Search) == 0 {
			continue
		}
		pageTitle := search.Query.Search[0].Title
		eq := "https://" + lang + ".wikipedia.org/w/api.php?action=query&prop=extracts&exintro=1&explaintext=1&titles=" +
			url.QueryEscape(pageTitle) + "&format=json"
		eraw, err := httpGetJSON(ctx, eq, 8*time.Second)
		if err != nil {
			continue
		}
		var edoc map[string]any
		if json.Unmarshal(eraw, &edoc) != nil {
			continue
		}
		query, _ := edoc["query"].(map[string]any)
		pages, _ := query["pages"].(map[string]any)
		src := "https://" + lang + ".wikipedia.org/wiki/" + strings.ReplaceAll(url.PathEscape(pageTitle), "%20", "_")
		for _, p := range pages {
			pm, _ := p.(map[string]any)
			extract, _ := pm["extract"].(string)
			if extract == "" {
				continue
			}
			extracts = append(extracts, extract)
			for _, m := range wikiOfficerRe.FindAllStringSubmatch(extract, -1) {
				name := strings.TrimSpace(m[1])
				if !isLikelyPersonName(name) {
					continue
				}
				makers = append(makers, DecisionMaker{
					Name: name, Title: "Officer (Wikipedia)", Source: "wikipedia",
					Evidence: truncateRunes(m[0], 120) + " @ " + src, Confidence: "medium",
				})
			}
			for _, dm := range extractPeopleFromText(extract, src) {
				dm.Source = "wikipedia"
				makers = append(makers, dm)
			}
		}
	}
	return dedupeDecisionMakers(makers), strings.Join(extracts, "\n"), nil
}

func applyWikipedia(intel *PlaceIntel, makers []DecisionMaker, extract string) {
	intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, makers)
	if extract != "" {
		intel.Sources = mergeUnique(intel.Sources, []string{"wikipedia"})
		if intel.Summary == "" && len([]rune(extract)) > 40 {
			intel.Summary = truncateRunes(extract, 280)
		}
		intel.Provider = strings.Trim(intel.Provider+"+wikipedia", "+")
	}
}

func lookupDuckDuckGoOfficers(ctx context.Context, title string) ([]DecisionMaker, error) {
	q := fmt.Sprintf(`"%s" (CEO OR founder OR "direktur utama" OR pendiri OR pemilik OR purchasing OR buyer OR procurement)`, title)
	u := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(q)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; gmaps-intel/1.0)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 400<<10))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ddg status %d", resp.StatusCode)
	}
	html := string(raw)
	text := tagStripRe.ReplaceAllString(html, " ")
	text = strings.Join(strings.Fields(text), " ")
	var makers []DecisionMaker
	for _, m := range wikiOfficerRe.FindAllStringSubmatch(text, -1) {
		name := strings.TrimSpace(m[1])
		if !isLikelyPersonName(name) {
			continue
		}
		makers = append(makers, DecisionMaker{
			Name: name, Title: "Officer", Source: "duckduckgo",
			Evidence: truncateRunes(m[0], 140), Confidence: "low",
		})
	}
	for _, dm := range extractPeopleFromText(text, "duckduckgo:"+title) {
		dm.Source = "duckduckgo"
		dm.Confidence = "low"
		makers = append(makers, dm)
	}
	out := dedupeDecisionMakers(makers)
	if len(out) > 5 {
		out = out[:5]
	}
	return out, nil
}

var (
	ddgLinkedInInRe = regexp.MustCompile(`(?i)(?:https?://(?:[a-z]+\.)?linkedin\.com/in/[a-z0-9\-_%]+|uddg=[^&]*linkedin\.com%2Fin%2F[a-z0-9\-_%]+)`)
	ddgLinkedInCoRe = regexp.MustCompile(`(?i)(?:https?://(?:[a-z]+\.)?linkedin\.com/company/[a-z0-9\-_%]+|uddg=[^&]*linkedin\.com%2Fcompany%2F[a-z0-9\-_%]+)`)
	ddgResultARe    = regexp.MustCompile(`(?is)<a[^>]+class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	liTitleSplitRe  = regexp.MustCompile(`(?i)\s*[-|–—]\s*|\s*\|\s*| · | • `)
	liSlugHexRe     = regexp.MustCompile(`^[0-9a-f]{5,}$`)
	liSlugDigitsRe  = regexp.MustCompile(`^\d+$`)
)

// lookupLinkedInPeople 用外贸常用 Google/DDG X-Ray 公式搜 LinkedIn 个人/公司页。
// 公式参考：site:linkedin.com/in "公司" (Purchasing Manager OR Buyer OR Procurement OR Direktur OR Owner OR CEO)
func lookupLinkedInPeople(ctx context.Context, title, domain string) ([]DecisionMaker, string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, "", fmt.Errorf("empty title")
	}
	brand := companyBrandToken(title)
	queries := []string{
		// 外贸大神常用：公司 + 采购/老板头衔 + LinkedIn
		fmt.Sprintf(`site:linkedin.com/in "%s" ("Purchasing Manager" OR "Procurement Manager" OR Buyer OR "Import Manager" OR "Supply Chain" OR Direktur OR Director OR Owner OR Founder OR CEO OR "General Manager")`, title),
		fmt.Sprintf(`site:linkedin.com/company "%s"`, title),
	}
	if brand != "" && !strings.EqualFold(brand, title) {
		queries = append(queries,
			fmt.Sprintf(`site:linkedin.com/in "%s" ("Purchasing Manager" OR Buyer OR Procurement OR Direktur OR Owner OR Founder OR CEO)`, brand),
			fmt.Sprintf(`site:linkedin.com/company "%s"`, brand),
			// 非 LinkedIn 补充：公开页提及采购负责人
			fmt.Sprintf(`"%s" ("Purchasing Manager" OR "Procurement" OR Buyer OR Direktur OR "Import Manager") (email OR contact OR LinkedIn OR "@")`, brand),
		)
	}
	if domain != "" {
		queries = append(queries, fmt.Sprintf(`site:linkedin.com/in "%s" (Purchasing OR Buyer OR Direktur OR Owner OR CEO)`, domain))
	}
	// 印尼市场加本地头衔
	lowTitle := strings.ToLower(title)
	if strings.Contains(lowTitle, "indonesia") || strings.Contains(lowTitle, "pt ") || strings.HasPrefix(lowTitle, "pt") {
		qBrand := brand
		if qBrand == "" {
			qBrand = title
		}
		queries = append(queries, fmt.Sprintf(`site:linkedin.com/in "%s" Indonesia (Direktur OR "General Manager" OR Purchasing OR Procurement OR Buyer OR Pemilik OR Owner)`, qBrand))
	}

	var makers []DecisionMaker
	coURL := ""
	seenIn := map[string]bool{}
	backend := ""

	// 搜索出口有成本（Clash 切节点）；优先前几条高杠杆公式
	if len(queries) > 4 {
		queries = queries[:4]
	}

	for _, q := range queries {
		page, src, err := fetchSearchHTMLForLinkedIn(ctx, q)
		if err != nil || page == "" {
			continue
		}
		if backend == "" {
			backend = src
		}
		for _, hit := range extractLinkedInHitsFromHTML(page) {
			switch hit.Kind {
			case "company":
				if coURL == "" {
					coURL = hit.URL
				}
			case "in":
				key := strings.ToLower(hit.URL)
				if hit.URL == "" || seenIn[key] {
					continue
				}
				seenIn[key] = true
				name, role := parseLinkedInResultTitle(hit.Label, title)
				if name == "" {
					name = linkedInSlugToName(hit.URL)
				}
				if name == "" || !IsValidPersonName(name) {
					continue
				}
				conf := "medium"
				if role != "" {
					conf = "high"
				}
				makers = append(makers, DecisionMaker{
					Name: name, Title: firstNonEmpty(role, "LinkedIn profile"),
					LinkedIn: hit.URL, Source: "linkedin:xray",
					Evidence: truncateRunes(firstNonEmpty(hit.Label, hit.URL), 140), Confidence: conf,
				})
			}
		}
		if len(makers) >= 6 {
			break
		}
	}
	out := dedupeDecisionMakers(makers)
	if len(out) > 8 {
		out = out[:8]
	}
	_ = backend
	return out, coURL, nil
}

// companyBrandToken 从 "PT Deugro Indonesia" 抽出品牌词 Deugro，供搜索公式使用。
func companyBrandToken(title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}
	// 去法人前缀/后缀
	re := regexp.MustCompile(`(?i)\b(PT\.?|CV\.?|TBK\.?|Ltd\.?|Limited|Inc\.?|Corp\.?|LLC|GmbH|Sdn\.?\s*Bhd\.?|Pte\.?|Co\.?|Company|Group|Indonesia|Jakarta)\b`)
	cleaned := strings.TrimSpace(re.ReplaceAllString(t, " "))
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return t
	}
	parts := strings.Fields(cleaned)
	// 取最长实义词，通常是品牌
	best := parts[0]
	for _, p := range parts {
		if len(p) > len(best) {
			best = p
		}
	}
	if len(best) < 3 {
		return cleaned
	}
	return best
}

func fetchDDGHTML(ctx context.Context, q string) (string, error) {
	u := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(q)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// DDG 常被风控；Brave 作为外贸检索兜底
		return fetchBraveHTML(ctx, q)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 400<<10))
	body := string(raw)
	if resp.StatusCode >= 400 || strings.Contains(body, "error-lite@duckduckgo") || !strings.Contains(body, "result__a") {
		if alt, err2 := fetchBraveHTML(ctx, q); err2 == nil && alt != "" {
			return alt, nil
		}
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("ddg status %d", resp.StatusCode)
		}
	}
	return body, nil
}

func decodeDDGHref(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	// DDG redirect: /l/?uddg=https%3A%2F%2F...
	if strings.Contains(href, "uddg=") {
		if u, err := url.Parse(href); err == nil {
			if v := u.Query().Get("uddg"); v != "" {
				if dec, err := url.QueryUnescape(v); err == nil {
					return dec
				}
				return v
			}
		}
		if i := strings.Index(href, "uddg="); i >= 0 {
			part := href[i+5:]
			if amp := strings.Index(part, "&"); amp >= 0 {
				part = part[:amp]
			}
			if dec, err := url.QueryUnescape(part); err == nil {
				return dec
			}
			return part
		}
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	return href
}

func cleanLinkedInURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.Split(raw, "?")[0]
	raw = strings.TrimRight(raw, "/")
	low := strings.ToLower(raw)
	if !strings.Contains(low, "linkedin.com/in/") && !strings.Contains(low, "linkedin.com/company/") {
		return ""
	}
	if strings.HasPrefix(low, "http") {
		return raw
	}
	if strings.HasPrefix(low, "linkedin.com") {
		return "https://www." + raw
	}
	return raw
}

func linkedInSlugToName(profileURL string) string {
	low := strings.ToLower(profileURL)
	idx := strings.Index(low, "/in/")
	if idx < 0 {
		return ""
	}
	slug := profileURL[idx+4:]
	if i := strings.IndexAny(slug, "/?#"); i >= 0 {
		slug = slug[:i]
	}
	slug, _ = url.PathUnescape(slug)
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	// john-tan-a1b2c3 → John Tan（丢掉末尾看似 ID 的片段）
	parts := strings.Split(slug, "-")
	var words []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if liSlugHexRe.MatchString(strings.ToLower(p)) {
			continue
		}
		if liSlugDigitsRe.MatchString(p) {
			continue
		}
		words = append(words, strings.ToUpper(p[:1])+strings.ToLower(p[1:]))
	}
	if len(words) < 2 {
		return ""
	}
	name := strings.Join(words, " ")
	if !isLikelyPersonName(name) {
		return ""
	}
	return name
}

func parseLinkedInResultTitle(label, company string) (name, role string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", ""
	}
	// 常见：Name - Title - Company | LinkedIn
	label = strings.ReplaceAll(label, "| LinkedIn", "")
	label = strings.ReplaceAll(label, "- LinkedIn", "")
	label = strings.TrimSpace(label)
	parts := liTitleSplitRe.Split(label, -1)
	var cleaned []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.EqualFold(p, "LinkedIn") {
			continue
		}
		cleaned = append(cleaned, p)
	}
	if len(cleaned) == 0 {
		return "", ""
	}
	cand := cleaned[0]
	if isLikelyPersonName(cand) {
		name = cand
	}
	roleKeys := []string{"purchas", "procure", "buyer", "direktur", "director", "owner", "founder", "ceo", "import", "export", "manager", "head"}
	for i, p := range cleaned {
		if i == 0 && name != "" {
			continue
		}
		low := strings.ToLower(p)
		if company != "" && strings.Contains(strings.ToLower(p), strings.ToLower(company)) {
			continue
		}
		for _, k := range roleKeys {
			if strings.Contains(low, k) {
				role = p
				break
			}
		}
		if role != "" {
			break
		}
	}
	return name, role
}

func firstStringDeep(v any, keys ...string) string {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range keys {
			if s, ok := t[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		for _, k := range keys {
			if child, ok := t[k]; ok {
				if s := firstStringDeep(child, keys...); s != "" {
					return s
				}
			}
		}
		for _, child := range t {
			if s := firstStringDeep(child, keys...); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range t {
			if s := firstStringDeep(child, keys...); s != "" {
				return s
			}
		}
	case string:
		return strings.TrimSpace(t)
	}
	return ""
}
