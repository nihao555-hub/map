package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 背调增强源：Hunter / katana / GitHub commits / GLEIF / Wikidata / AHU（可选代理）。

var (
	personNameRe = regexp.MustCompile(`(?i)\b([A-ZÁÉÍÓÚÄËÏÖÜÑ][\p{L}'’\-]{1,40}(?:\s+[A-ZÁÉÍÓÚÄËÏÖÜÑ][\p{L}'’\-]{1,40}){1,3})\b`)
	titleNearRe  = regexp.MustCompile(`(?i)\b(ceo|cfo|cto|coo|founder|co-founder|owner|director|direktur(\s+utama)?|komisaris(\s+utama)?|president|partner|pendiri|pemilik|chief\s+\w+|head of\s+\w+|managing director|board member)\b`)
	teamURLRe    = regexp.MustCompile(`(?i)/(about|about-us|team|our-team|people|leadership|management|staff|contact|contact-us|kontak|tentang|tentang-kami|profil|struktur|karir|company|imprint)(/|$|\?)`)
)

func hunterAPIKey() string {
	for _, k := range []string{"HUNTER_API_KEY", "HUNTER_KEY"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func ahuProxyURL() string {
	for _, k := range []string{"AHU_PROXY", "AHU_PROXY_URL", "RESIDENTIAL_PROXY", "HTTPS_PROXY", "HTTP_PROXY"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func githubToken() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func katanaBin() string {
	if p := strings.TrimSpace(os.Getenv("KATANA_BIN")); p != "" {
		return p
	}
	if p := filepath.Join(os.Getenv("HOME"), "go", "bin", "katana"); fileExists(p) {
		return p
	}
	if p, err := exec.LookPath("katana"); err == nil {
		return p
	}
	return ""
}

func ahuScriptPath() string {
	if p := strings.TrimSpace(os.Getenv("AHU_SCRIPT")); p != "" {
		return p
	}
	return filepath.Join(repoRoot(), "tools", "ahu_lookup.py")
}

// runEnrichmentSources 并行跑高杠杆增强源（与 CLI OSINT 第一波重叠执行）。
func runEnrichmentSources(ctx context.Context, intel *PlaceIntel, place Place, st OSINTStatus, mu *sync.Mutex, addNote func(string)) {
	budget, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	domain := intel.Domain
	title := place.Title
	website := place.Website

	if domain != "" && st.Hunter {
		wg.Add(1)
		go func() {
			defer wg.Done()
			people, emails, err := lookupHunterDomain(budget, domain)
			if err != nil {
				addNote("Hunter：" + truncateRunes(err.Error(), 50))
				return
			}
			mu.Lock()
			applyHunter(intel, people, emails)
			mu.Unlock()
		}()
	}

	if website != "" && st.Katana {
		wg.Add(1)
		go func() {
			defer wg.Done()
			emails, socials, makers, urls, err := runKatanaTeamCrawl(budget, website)
			if err != nil {
				return
			}
			mu.Lock()
			applyKatana(intel, emails, socials, makers, urls)
			mu.Unlock()
		}()
	}

	if domain != "" && st.GitHubCommits {
		wg.Add(1)
		go func() {
			defer wg.Done()
			people, emails, err := lookupGitHubCommitAuthors(budget, domain)
			if err != nil {
				return
			}
			mu.Lock()
			applyGitHubAuthors(intel, people, emails)
			mu.Unlock()
		}()
	}

	if st.GLEIF && (title != "" || domain != "") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hit, err := lookupGLEIF(budget, title, domain)
			if err != nil || hit == nil {
				return
			}
			mu.Lock()
			if intel.CompanyRegistry == nil {
				intel.CompanyRegistry = hit
			} else if hit.RegistryURL != "" {
				intel.Sources = mergeUnique(intel.Sources, []string{hit.RegistryURL})
			}
			intel.Sources = mergeUnique(intel.Sources, []string{"gleif"})
			intel.Provider = strings.Trim(intel.Provider+"+gleif", "+")
			mu.Unlock()
		}()
	}

	if st.Wikidata && title != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			makers, org, err := lookupWikidataPeople(budget, title)
			if err != nil {
				return
			}
			mu.Lock()
			applyWikidata(intel, makers, org)
			mu.Unlock()
		}()
	}

	if st.AHU && st.AHUProxyConfigured && title != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hit, makers, err := lookupAHU(budget, title)
			if err != nil {
				addNote("AHU：" + truncateRunes(err.Error(), 50))
				return
			}
			mu.Lock()
			applyAHU(intel, hit, makers)
			mu.Unlock()
		}()
	}

	wg.Wait()
}

func lookupHunterDomain(ctx context.Context, domain string) ([]DecisionMaker, []string, error) {
	key := hunterAPIKey()
	if key == "" {
		return nil, nil, fmt.Errorf("no HUNTER_API_KEY")
	}
	domain = strings.TrimSpace(strings.ToLower(domain))
	u := fmt.Sprintf("https://api.hunter.io/v2/domain-search?domain=%s&api_key=%s&limit=20",
		url.QueryEscape(domain), url.QueryEscape(key))
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var parsed struct {
		Data struct {
			Emails []struct {
				Value      string `json:"value"`
				FirstName  string `json:"first_name"`
				LastName   string `json:"last_name"`
				Position   string `json:"position"`
				Confidence int    `json:"confidence"`
				LinkedIn   string `json:"linkedin"`
			} `json:"emails"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, nil, err
	}
	var people []DecisionMaker
	var emails []string
	for _, e := range parsed.Data.Emails {
		em := strings.ToLower(strings.TrimSpace(e.Value))
		if em == "" {
			continue
		}
		emails = append(emails, em)
		name := strings.TrimSpace(e.FirstName + " " + e.LastName)
		if name == "" {
			continue
		}
		conf := "medium"
		if e.Confidence >= 80 {
			conf = "high"
		} else if e.Confidence < 50 {
			conf = "low"
		}
		people = append(people, DecisionMaker{
			Name:       name,
			Title:      strings.TrimSpace(e.Position),
			Email:      em,
			LinkedIn:   strings.TrimSpace(e.LinkedIn),
			Source:     "hunter.io",
			Evidence:   fmt.Sprintf("hunter domain-search confidence=%d", e.Confidence),
			Confidence: conf,
		})
	}
	return people, uniqueStrings(emails), nil
}

func applyHunter(intel *PlaceIntel, people []DecisionMaker, emails []string) {
	intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emails, intel.Domain))
	intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, people)
	intel.Sources = mergeUnique(intel.Sources, []string{"hunter.io"})
	intel.Provider = strings.Trim(intel.Provider+"+hunter", "+")
}

// runKatanaTeamCrawl 用 katana 发现 about/team/contact URL，再抓取抽取邮箱/人名。
func runKatanaTeamCrawl(ctx context.Context, website string) (emails []string, socials map[string]string, makers []DecisionMaker, urls []string, err error) {
	bin := katanaBin()
	if bin == "" {
		return nil, nil, nil, nil, fmt.Errorf("katana missing")
	}
	website = strings.TrimSpace(website)
	if website == "" {
		return nil, nil, nil, nil, fmt.Errorf("empty website")
	}

	ctx, cancel := context.WithTimeout(ctx, 16*time.Second)
	defer cancel()
	var out bytes.Buffer
	_ = runCmdGroup(ctx, &out, nil, bin,
		"-u", website,
		"-d", "2",
		"-c", "12",
		"-rl", "30",
		"-silent",
		"-nc",
		"-timeout", "8",
	)
	seen := map[string]bool{}
	var candidates []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "http") {
			continue
		}
		if !teamURLRe.MatchString(line) {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		candidates = append(candidates, line)
		if len(candidates) >= 8 {
			break
		}
	}
	socials = map[string]string{}
	for _, u := range candidates {
		body, _, ferr := fetchIntelPage(ctx, u)
		if ferr != nil || len(body) < 80 {
			continue
		}
		urls = append(urls, u)
		text := stripTags(string(body))
		emails = mergeUnique(emails, filterPublicEmails(emailFindRe.FindAllString(string(body), -1), hostDomain(website)))
		for _, li := range linkedinRe.FindAllString(string(body), -1) {
			if socials["linkedin"] == "" {
				socials["linkedin"] = strings.Split(li, "?")[0]
			}
		}
		makers = append(makers, extractPeopleFromText(text, u)...)
		if len(urls) >= 4 {
			break
		}
	}
	return uniqueStrings(emails), socials, dedupeDecisionMakers(makers), urls, nil
}

func applyKatana(intel *PlaceIntel, emails []string, socials map[string]string, makers []DecisionMaker, urls []string) {
	intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emails, intel.Domain))
	if intel.Socials == nil {
		intel.Socials = map[string]string{}
	}
	for k, v := range socials {
		if intel.Socials[k] == "" && v != "" {
			intel.Socials[k] = v
		}
	}
	intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, makers)
	intel.Sources = mergeUnique(intel.Sources, append([]string{"katana"}, urls...))
	intel.Provider = strings.Trim(intel.Provider+"+katana", "+")
}

func hostDomain(website string) string {
	u, err := url.Parse(website)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func extractPeopleFromText(text, sourceURL string) []DecisionMaker {
	if text == "" {
		return nil
	}
	// 在头衔附近窗口抓人名
	var out []DecisionMaker
	low := text
	idxs := titleNearRe.FindAllStringIndex(low, -1)
	for _, loc := range idxs {
		start := loc[0] - 80
		if start < 0 {
			start = 0
		}
		end := loc[1] + 120
		if end > len(low) {
			end = len(low)
		}
		window := low[start:end]
		title := strings.TrimSpace(titleNearRe.FindString(window))
		for _, m := range personNameRe.FindAllString(window, -1) {
			name := strings.TrimSpace(m)
			if !isLikelyPersonName(name) {
				continue
			}
			out = append(out, DecisionMaker{
				Name:       name,
				Title:      title,
				Source:     "katana-page",
				Evidence:   "name near title on " + sourceURL,
				Confidence: "medium",
			})
		}
	}
	return dedupeDecisionMakers(out)
}

func isLikelyPersonName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	parts := strings.Fields(name)
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	low := strings.ToLower(name)
	deny := []string{
		"pt ", "cv ", "tbk", "indonesia", "jakarta", "limited", "company", "group",
		"contact us", "about us", "privacy policy", "terms of", "cookie", "javascript",
		"read more", "sign in", "log in", "follow us", "all rights", "wordpress",
		"google maps", "click here", "learn more", "get started",
	}
	for _, d := range deny {
		if strings.Contains(low, d) {
			return false
		}
	}
	// 邮箱 local / 职能词不当人名
	roleLocals := []string{"info", "admin", "sales", "marketing", "support", "contact", "hello", "cs", "customer"}
	if len(parts) == 1 {
		return false
	}
	for _, p := range parts {
		pl := strings.ToLower(p)
		for _, r := range roleLocals {
			if pl == r {
				return false
			}
		}
		if len([]rune(p)) < 2 {
			return false
		}
	}
	return true
}

func lookupGitHubCommitAuthors(ctx context.Context, domain string) ([]DecisionMaker, []string, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil, nil, fmt.Errorf("empty domain")
	}
	u := "https://api.github.com/search/commits?q=" + url.QueryEscape("author-email:@"+domain) + "&per_page=20"
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gmaps-intel/1.0")
	if tok := githubToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("github status %d", resp.StatusCode)
	}
	var parsed struct {
		Items []struct {
			Commit struct {
				Author struct {
					Name  string `json:"name"`
					Email string `json:"email"`
				} `json:"author"`
			} `json:"commit"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, nil, err
	}
	var people []DecisionMaker
	var emails []string
	for _, it := range parsed.Items {
		em := strings.ToLower(strings.TrimSpace(it.Commit.Author.Email))
		name := strings.TrimSpace(it.Commit.Author.Name)
		if em != "" {
			emails = append(emails, em)
		}
		if !isLikelyPersonName(name) || em == "" {
			continue
		}
		people = append(people, DecisionMaker{
			Name: name, Email: em, Title: "Contributor",
			Source: "github-commits", Evidence: "public commit author-email:@" + domain,
			Confidence: "medium",
		})
	}
	return dedupeDecisionMakers(people), uniqueStrings(emails), nil
}

func applyGitHubAuthors(intel *PlaceIntel, people []DecisionMaker, emails []string) {
	intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emails, intel.Domain))
	intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, people)
	if len(people) > 0 || len(emails) > 0 {
		intel.Sources = mergeUnique(intel.Sources, []string{"github-commits"})
		intel.Provider = strings.Trim(intel.Provider+"+github", "+")
	}
}

func lookupGLEIF(ctx context.Context, name, domain string) (*CompanyHit, error) {
	q := strings.TrimSpace(name)
	if q == "" {
		q = domain
	}
	if q == "" {
		return nil, fmt.Errorf("empty query")
	}
	u := "https://api.gleif.org/api/v1/lei-records?filter[entity.legalName]=" + url.QueryEscape(q) + "&page[size]=5"
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("User-Agent", "gmaps-intel/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gleif status %d", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Entity struct {
					LegalName struct {
						Name string `json:"name"`
					} `json:"legalName"`
					Status       string `json:"status"`
					LegalAddress struct {
						Country string `json:"country"`
					} `json:"legalAddress"`
					Category string `json:"category"`
				} `json:"entity"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 {
		return nil, nil
	}
	best := parsed.Data[0]
	nameLow := strings.ToLower(name)
	for _, d := range parsed.Data {
		n := strings.ToLower(d.Attributes.Entity.LegalName.Name)
		if nameLow != "" && (strings.Contains(n, nameLow) || strings.Contains(nameLow, n)) {
			best = d
			break
		}
		if d.Attributes.Entity.LegalAddress.Country == "ID" {
			best = d
		}
	}
	return &CompanyHit{
		Name:          best.Attributes.Entity.LegalName.Name,
		CompanyNumber: best.ID,
		Jurisdiction:  best.Attributes.Entity.LegalAddress.Country,
		CompanyType:   best.Attributes.Entity.Category,
		CurrentStatus: best.Attributes.Entity.Status,
		RegistryURL:   "https://search.gleif.org/#/record/" + best.ID,
		Source:        "gleif",
	}, nil
}

func lookupWikidataPeople(ctx context.Context, title string) ([]DecisionMaker, *OrgUnit, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, nil, fmt.Errorf("empty title")
	}
	// 去掉常见后缀提高命中
	q := title
	for _, suf := range []string{" ID", " Indonesia", " Coffee", " Group"} {
		if strings.HasSuffix(q, suf) && len(q) > len(suf)+2 {
			// keep full first; search both
			break
		}
	}
	searchURL := "https://www.wikidata.org/w/api.php?action=wbsearchentities&search=" +
		url.QueryEscape(q) + "&language=en&uselang=en&format=json&limit=3"
	raw, err := httpGetJSON(ctx, searchURL, 8*time.Second)
	if err != nil {
		return nil, nil, err
	}
	var search struct {
		Search []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"search"`
	}
	if json.Unmarshal(raw, &search) != nil || len(search.Search) == 0 {
		return nil, nil, nil
	}
	qid := search.Search[0].ID
	label := search.Search[0].Label
	entURL := "https://www.wikidata.org/wiki/Special:EntityData/" + qid + ".json"
	entRaw, err := httpGetJSON(ctx, entURL, 10*time.Second)
	if err != nil {
		return nil, nil, err
	}
	var entDoc map[string]any
	if json.Unmarshal(entRaw, &entDoc) != nil {
		return nil, nil, fmt.Errorf("wikidata entity json")
	}
	entities, _ := entDoc["entities"].(map[string]any)
	ent, _ := entities[qid].(map[string]any)
	if ent == nil {
		return nil, nil, nil
	}
	claims, _ := ent["claims"].(map[string]any)
	roleProps := []struct {
		PID  string
		Role string
	}{
		{"P112", "Founder"},
		{"P169", "Chief executive officer"},
		{"P488", "Chairperson"},
		{"P1037", "Director / manager"},
		{"P3320", "Board member"},
	}
	var makers []DecisionMaker
	for _, rp := range roleProps {
		arr, _ := claims[rp.PID].([]any)
		for _, c := range arr {
			cm, _ := c.(map[string]any)
			mainsnak, _ := cm["mainsnak"].(map[string]any)
			dv, _ := mainsnak["datavalue"].(map[string]any)
			val, _ := dv["value"].(map[string]any)
			pid, _ := val["id"].(string)
			if pid == "" {
				continue
			}
			name, nerr := wikidataLabel(ctx, pid)
			if nerr != nil || !isLikelyPersonName(name) {
				if name == "" {
					continue
				}
				// 单名创始人也保留（印尼/中文场景）
				if len(strings.Fields(name)) < 1 {
					continue
				}
			}
			if name == "" {
				continue
			}
			makers = append(makers, DecisionMaker{
				Name: name, Title: rp.Role, Source: "wikidata",
				Evidence:   fmt.Sprintf("%s %s → %s", label, rp.PID, pid),
				Confidence: "high",
			})
		}
	}
	org := &OrgUnit{Name: label, Role: "wikidata entity", Evidence: "https://www.wikidata.org/wiki/" + qid}
	return dedupeDecisionMakers(makers), org, nil
}

func wikidataLabel(ctx context.Context, qid string) (string, error) {
	raw, err := httpGetJSON(ctx, "https://www.wikidata.org/wiki/Special:EntityData/"+qid+".json", 8*time.Second)
	if err != nil {
		return "", err
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return "", fmt.Errorf("json")
	}
	entities, _ := doc["entities"].(map[string]any)
	ent, _ := entities[qid].(map[string]any)
	labels, _ := ent["labels"].(map[string]any)
	for _, lang := range []string{"en", "id", "nl", "de"} {
		if lb, ok := labels[lang].(map[string]any); ok {
			if v, _ := lb["value"].(string); v != "" {
				return v, nil
			}
		}
	}
	return "", nil
}

func applyWikidata(intel *PlaceIntel, makers []DecisionMaker, org *OrgUnit) {
	intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, makers)
	if org != nil && org.Name != "" {
		intel.OrgStructure = append(intel.OrgStructure, *org)
		intel.Sources = mergeUnique(intel.Sources, []string{org.Evidence})
	}
	if len(makers) > 0 {
		intel.Sources = mergeUnique(intel.Sources, []string{"wikidata"})
		intel.Provider = strings.Trim(intel.Provider+"+wikidata", "+")
	}
}

// lookupAHU 调用 tools/ahu_lookup.py（需 AHU_PROXY + playwright/camoufox）。
func lookupAHU(ctx context.Context, company string) (*CompanyHit, []DecisionMaker, error) {
	script := ahuScriptPath()
	if !fileExists(script) {
		return nil, nil, fmt.Errorf("ahu script missing")
	}
	proxy := ahuProxyURL()
	if proxy == "" {
		return nil, nil, fmt.Errorf("AHU_PROXY not set (datacenter IPs blocked)")
	}
	py := osintExtraPython()
	if !fileExists(py) {
		py = "python3"
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var stdout, stderr strings.Builder
	cmd := exec.CommandContext(ctx, py, script, "--query", company, "--proxy", proxy) //nolint:gosec
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("%v: %s", err, truncateRunes(stderr.String(), 80))
	}
	var parsed struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		Result struct {
			CompanyName    string `json:"company_name"`
			RegistrationNo string `json:"registration_no"`
			LegalForm      string `json:"legal_form"`
			LegalStatus    string `json:"legal_status"`
			Domicile       string `json:"domicile"`
			Directors      []struct {
				Nama    string `json:"nama"`
				Jabatan string `json:"jabatan"`
			} `json:"directors"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &parsed); err != nil {
		return nil, nil, fmt.Errorf("ahu json: %w", err)
	}
	if !parsed.OK {
		return nil, nil, fmt.Errorf("%s", firstNonEmpty(parsed.Error, "ahu failed"))
	}
	hit := &CompanyHit{
		Name:          parsed.Result.CompanyName,
		CompanyNumber: parsed.Result.RegistrationNo,
		Jurisdiction:  "id",
		CompanyType:   parsed.Result.LegalForm,
		CurrentStatus: parsed.Result.LegalStatus,
		RegistryURL:   "https://ahu.go.id/pencarian/perseroan-terbatas",
		Source:        "ahu.go.id",
	}
	var makers []DecisionMaker
	for _, d := range parsed.Result.Directors {
		name := strings.TrimSpace(d.Nama)
		if name == "" {
			continue
		}
		makers = append(makers, DecisionMaker{
			Name: name, Title: firstNonEmpty(d.Jabatan, "Direksi"),
			Source: "ahu.go.id", Evidence: "AHU company directors",
			Confidence: "high",
		})
	}
	return hit, makers, nil
}

func applyAHU(intel *PlaceIntel, hit *CompanyHit, makers []DecisionMaker) {
	if hit != nil {
		intel.CompanyRegistry = hit
		intel.Sources = mergeUnique(intel.Sources, []string{hit.RegistryURL, "ahu.go.id"})
	}
	intel.DecisionMakers = mergeDecisionMakers(intel.DecisionMakers, makers)
	if len(makers) > 0 || hit != nil {
		intel.Provider = strings.Trim(intel.Provider+"+ahu", "+")
	}
}

func httpGetJSON(ctx context.Context, rawURL string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gmaps-intel/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func mergeDecisionMakers(dst, src []DecisionMaker) []DecisionMaker {
	return dedupeDecisionMakers(append(append([]DecisionMaker{}, dst...), src...))
}

func dedupeDecisionMakers(in []DecisionMaker) []DecisionMaker {
	seen := map[string]bool{}
	var out []DecisionMaker
	for _, d := range in {
		d.Name = strings.TrimSpace(d.Name)
		d.Email = strings.TrimSpace(strings.ToLower(d.Email))
		key := strings.ToLower(d.Name) + "|" + d.Email
		if d.Name == "" && d.Email == "" {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}

// countNamedPeople 统计像真人名的决策人（非邮箱 local / 占位文案）。
func countNamedPeople(makers []DecisionMaker) int {
	n := 0
	for _, d := range makers {
		if looksLikeRealPerson(d) {
			n++
		}
	}
	return n
}

func looksLikeRealPerson(d DecisionMaker) bool {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return false
	}
	if strings.Contains(name, "核实") || strings.Contains(name, "（") {
		return false
	}
	if strings.Contains(name, "@") {
		return false
	}
	// 邮箱 local 被当成 Name 的情况
	if d.Email != "" {
		local := d.Email
		if i := strings.Index(d.Email, "@"); i > 0 {
			local = d.Email[:i]
		}
		if strings.EqualFold(name, local) {
			return false
		}
	}
	if isLikelyPersonName(name) {
		return true
	}
	// Wikidata/AHU 高置信单段名
	if (d.Source == "wikidata" || d.Source == "ahu.go.id" || d.Source == "hunter.io") &&
		len([]rune(name)) >= 3 && !strings.Contains(strings.ToLower(name), "contact") {
		return true
	}
	return false
}
