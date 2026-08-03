package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// OSINTStatus 本地已安装的 OSINT 工具探测结果。
type OSINTStatus struct {
	TheHarvester bool `json:"theharvester"`
	SpiderFoot   bool `json:"spiderfoot"`
	Holehe       bool `json:"holehe"`
	Maigret      bool `json:"maigret"`
	Blackbird    bool `json:"blackbird"`
	Photon       bool `json:"photon"`
	Amass        bool `json:"amass"`
	OpenCorporatesAPI bool `json:"opencorporates_api"`
}

func osintExtraPython() string {
	if p := strings.TrimSpace(os.Getenv("OSINT_EXTRA_PYTHON")); p != "" {
		return p
	}
	return filepath.Join(repoRoot(), "tools", "osint-extra-venv", "bin", "python")
}

func theHarvesterCmd() (string, []string) {
	if p := strings.TrimSpace(os.Getenv("THEHARVESTER_BIN")); p != "" {
		return p, nil
	}
	root := repoRoot()
	thDir := filepath.Join(root, "tools", "theHarvester")
	uv := "uv"
	if p := strings.TrimSpace(os.Getenv("UV_BIN")); p != "" {
		uv = p
	}
	return uv, []string{"run", "--directory", thDir, "theHarvester"}
}

func spiderfootPython() (py, sf string) {
	if p := strings.TrimSpace(os.Getenv("SPIDERFOOT_PYTHON")); p != "" {
		py = p
	} else {
		py = filepath.Join(repoRoot(), "tools", "spiderfoot-venv", "bin", "python")
	}
	if p := strings.TrimSpace(os.Getenv("SPIDERFOOT_SF")); p != "" {
		sf = p
	} else {
		sf = filepath.Join(repoRoot(), "tools", "spiderfoot", "sf.py")
	}
	return py, sf
}

func repoRoot() string {
	if p := strings.TrimSpace(os.Getenv("GMAPS_REPO_ROOT")); p != "" {
		return p
	}
	wd, err := os.Getwd()
	if err == nil {
		if _, err2 := os.Stat(filepath.Join(wd, "tools")); err2 == nil {
			return wd
		}
		if _, err2 := os.Stat(filepath.Join(wd, "..", "tools")); err2 == nil {
			return filepath.Clean(filepath.Join(wd, ".."))
		}
	}
	return "/workspace"
}

// ProbeOSINTTools 探测全部背调依赖是否可用。
func ProbeOSINTTools() OSINTStatus {
	st := OSINTStatus{OpenCorporatesAPI: true}
	bin, args := theHarvesterCmd()
	thDir := filepath.Join(repoRoot(), "tools", "theHarvester")
	if info, err := os.Stat(thDir); err == nil && info.IsDir() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		cmd := exec.CommandContext(ctx, bin, append(args, "-h")...)
		st.TheHarvester = cmd.Run() == nil
		cancel()
	}
	py, sf := spiderfootPython()
	if _, err := os.Stat(py); err == nil {
		if _, err2 := os.Stat(sf); err2 == nil {
			st.SpiderFoot = true
		}
	}
	extra := osintExtraPython()
	if _, err := os.Stat(extra); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		st.Holehe = exec.CommandContext(ctx, extra, "-c", "import holehe").Run() == nil
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 6*time.Second)
		st.Maigret = exec.CommandContext(ctx, extra, "-c", "import maigret").Run() == nil
		cancel()
		bb := filepath.Join(repoRoot(), "tools", "blackbird", "blackbird.py")
		if _, err := os.Stat(bb); err == nil {
			st.Blackbird = true
		}
		ph := filepath.Join(repoRoot(), "tools", "Photon", "photon.py")
		if _, err := os.Stat(ph); err == nil {
			st.Photon = true
		}
	}
	amass := "amass"
	if p := strings.TrimSpace(os.Getenv("AMASS_BIN")); p != "" {
		amass = p
	} else if p := filepath.Join(os.Getenv("HOME"), "go", "bin", "amass"); fileExists(p) {
		amass = p
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	st.Amass = exec.CommandContext(ctx, amass, "-version").Run() == nil ||
		exec.CommandContext(ctx, amass, "version").Run() == nil
	cancel()
	return st
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// OSINTToolsAvailable 兼容旧调用。
func OSINTToolsAvailable() (harvester, spiderfoot bool) {
	st := ProbeOSINTTools()
	return st.TheHarvester, st.SpiderFoot
}

type harvesterOut struct {
	Emails        []string `json:"emails"`
	Hosts         []string `json:"hosts"`
	People        []string `json:"people"`
	Interesting   []string `json:"interesting_urls"`
}

func runTheHarvester(ctx context.Context, domain string) (*harvesterOut, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" || !strings.Contains(domain, ".") {
		return nil, fmt.Errorf("invalid domain")
	}
	bin, args := theHarvesterCmd()
	tmpDir, err := os.MkdirTemp("", "th-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	outBase := filepath.Join(tmpDir, "out")

	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()

	full := append(append([]string{}, args...),
		"-d", domain,
		"-b", "crtsh,hackertarget",
		"-l", "40",
		"-f", outBase,
	)
	cmd := exec.CommandContext(ctx, bin, full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	_ = cmd.Run()

	raw, err := os.ReadFile(outBase + ".json")
	if err != nil {
		return nil, fmt.Errorf("theHarvester no json: %v (%s)", err, truncateRunes(stderr.String(), 180))
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	out := &harvesterOut{}
	out.Emails = asStringSlice(parsed["emails"])
	out.Hosts = asStringSlice(parsed["hosts"])
	out.People = asStringSlice(parsed["people"])
	out.Interesting = asStringSlice(parsed["interesting_urls"])
	blob := string(raw)
	out.Emails = mergeUnique(out.Emails, filterPublicEmails(emailFindRe.FindAllString(blob, -1), domain))
	return out, nil
}

type spiderEvent struct {
	Type   string `json:"type"`
	Data   string `json:"data"`
	Module string `json:"module"`
	Source string `json:"source"`
}

func runSpiderfootLite(ctx context.Context, domain string) ([]spiderEvent, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil, fmt.Errorf("invalid domain")
	}
	py, sf := spiderfootPython()
	modules := strings.Join([]string{
		"sfp_dnsresolve", "sfp_whois", "sfp_crt", "sfp_dnsraw",
		"sfp_company", "sfp_names", "sfp_email", "sfp_opencorporates",
	}, ",")

	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, py, sf,
		"-s", domain,
		"-m", modules,
		"-o", "json",
		"-q",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	raw := strings.TrimSpace(stdout.String())
	if raw == "" {
		return nil, fmt.Errorf("spiderfoot empty: %v %s", err, truncateRunes(stderr.String(), 160))
	}
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("spiderfoot bad json")
	}
	var events []spiderEvent
	if err2 := json.Unmarshal([]byte(raw[start:end+1]), &events); err2 != nil {
		return nil, err2
	}
	return events, nil
}

// runPhoton 爬官网浅层页面，抽邮箱/社媒链接。
func runPhoton(ctx context.Context, website string) (emails []string, socials map[string]string, err error) {
	website = strings.TrimSpace(website)
	if website == "" {
		return nil, nil, fmt.Errorf("empty website")
	}
	py := osintExtraPython()
	script := filepath.Join(repoRoot(), "tools", "Photon", "photon.py")
	if !fileExists(script) {
		return nil, nil, fmt.Errorf("photon missing")
	}
	tmpDir, err := os.MkdirTemp("", "photon-*")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, script,
		"-u", website,
		"-l", "1",
		"-t", "6",
		"-o", tmpDir,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	_ = cmd.Run()

	socials = map[string]string{}
	// Photon 输出：emails.txt / social.txt / intel.txt 等
	for _, name := range []string{"emails.txt", "intel.txt", "social.txt", "external.txt", "internal.txt"} {
		b, err := os.ReadFile(filepath.Join(tmpDir, name))
		if err != nil {
			continue
		}
		text := string(b)
		emails = mergeUnique(emails, emailFindRe.FindAllString(text, -1))
		for _, li := range linkedinRe.FindAllString(text, -1) {
			socials["linkedin"] = strings.Split(li, "?")[0]
		}
		for _, u := range regexp.MustCompile(`(?i)https?://(?:www\.)?(?:instagram|facebook|twitter|x|tiktok|youtube)\.com/[^\s"'<>]+`).FindAllString(text, -1) {
			low := strings.ToLower(u)
			switch {
			case strings.Contains(low, "instagram.com"):
				socials["instagram"] = u
			case strings.Contains(low, "facebook.com"):
				socials["facebook"] = u
			case strings.Contains(low, "twitter.com"), strings.Contains(low, "x.com/"):
				socials["twitter"] = u
			case strings.Contains(low, "tiktok.com"):
				socials["tiktok"] = u
			case strings.Contains(low, "youtube.com"):
				socials["youtube"] = u
			}
		}
	}
	return uniqueStrings(emails), socials, nil
}

// runHolehe 查邮箱注册过哪些站点（--only-used）。
func runHolehe(ctx context.Context, email string) ([]string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("bad email")
	}
	py := osintExtraPython()
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	// holehe CLI writes colored output; parse [+] lines
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(py), "holehe"), "--only-used", "--no-color", "-NP", email)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()
	var sites []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[+]") || strings.Contains(line, "[+]") {
			// e.g. [+] github.com
			line = strings.TrimSpace(strings.ReplaceAll(line, "[+]", ""))
			if line != "" && !strings.Contains(strings.ToLower(line), "email used") {
				sites = append(sites, line)
			}
		}
	}
	return uniqueStrings(sites), nil
}

// runBlackbirdEmail 用 blackbird 按邮箱查社媒账号。
func runBlackbirdEmail(ctx context.Context, email string) ([]string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, fmt.Errorf("bad email")
	}
	py := osintExtraPython()
	script := filepath.Join(repoRoot(), "tools", "blackbird", "blackbird.py")
	if !fileExists(script) {
		return nil, fmt.Errorf("blackbird missing")
	}
	tmpDir, err := os.MkdirTemp("", "bb-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, script,
		"--email", email,
		"--json",
		"--no-nsfw",
		"--timeout", "8",
		"--max-concurrent-requests", "20",
	)
	cmd.Dir = tmpDir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()

	// blackbird 常把 json 写到 results/ 目录
	var hits []string
	_ = filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".json") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var parsed any
		if json.Unmarshal(b, &parsed) != nil {
			return nil
		}
		hits = append(hits, extractBlackbirdSites(parsed)...)
		return nil
	})
	// 回退：从 stdout 抓 URL
	if len(hits) == 0 {
		for _, u := range regexp.MustCompile(`https?://[^\s"'<>]+`).FindAllString(out.String(), -1) {
			hits = append(hits, u)
		}
	}
	return uniqueStrings(hits), nil
}

func extractBlackbirdSites(v any) []string {
	var out []string
	switch t := v.(type) {
	case map[string]any:
		if s, ok := t["site"].(string); ok {
			out = append(out, s)
		}
		if s, ok := t["url"].(string); ok {
			out = append(out, s)
		}
		for _, x := range t {
			out = append(out, extractBlackbirdSites(x)...)
		}
	case []any:
		for _, x := range t {
			out = append(out, extractBlackbirdSites(x)...)
		}
	}
	return out
}

// runAmassPassive 被动子域名枚举（短超时）。
func runAmassPassive(ctx context.Context, domain string) ([]string, error) {
	domain = strings.TrimSpace(strings.ToLower(domain))
	if domain == "" {
		return nil, fmt.Errorf("bad domain")
	}
	amass := "amass"
	if p := strings.TrimSpace(os.Getenv("AMASS_BIN")); p != "" {
		amass = p
	} else if p := filepath.Join(os.Getenv("HOME"), "go", "bin", "amass"); fileExists(p) {
		amass = p
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, amass, "enum", "-passive", "-d", domain, "-nocolor")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()
	var hosts []string
	for _, line := range strings.Split(out.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		// amass 行可能是 "host (FQDN) --> ..."
		if i := strings.Index(line, " "); i > 0 {
			line = line[:i]
		}
		if strings.Contains(line, domain) {
			hosts = append(hosts, line)
		}
	}
	return uniqueStrings(hosts), nil
}

// runMaigretLite 用邮箱本地部分当用户名做轻量社媒枚举（限站点数）。
func runMaigretLite(ctx context.Context, username string) ([]string, error) {
	username = strings.TrimSpace(username)
	username = regexp.MustCompile(`[^a-zA-Z0-9._-]`).ReplaceAllString(username, "")
	if len(username) < 3 {
		return nil, fmt.Errorf("bad username")
	}
	py := osintExtraPython()
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	tmpDir, err := os.MkdirTemp("", "maigret-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	cmd := exec.CommandContext(ctx, filepath.Join(filepath.Dir(py), "maigret"),
		username,
		"--timeout", "8",
		"-n", "20",
		"--no-recursion",
		"--no-extracting",
		"-fo", tmpDir,
		"--json", "simple",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run()
	var hits []string
	_ = filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}
		b, _ := os.ReadFile(path)
		var parsed map[string]any
		if json.Unmarshal(b, &parsed) != nil {
			return nil
		}
		for site, v := range parsed {
			if m, ok := v.(map[string]any); ok {
				if st, _ := m["status"].(map[string]any); st != nil {
					if s, _ := st["status"].(string); strings.EqualFold(s, "Claimed") || strings.EqualFold(s, "found") {
						hits = append(hits, site)
					}
				}
				if u, ok := m["url_user"].(string); ok {
					hits = append(hits, u)
				}
			}
		}
		return nil
	})
	return uniqueStrings(hits), nil
}

func asStringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		var out []string
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	default:
		return nil
	}
}

var whoisOrgRe = regexp.MustCompile(`(?i)(?:Registrant Organization|OrgName|organisation|organization):\s*(.+)`)

func applyHarvester(intel *PlaceIntel, h *harvesterOut) {
	if h == nil {
		return
	}
	intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(h.Emails, intel.Domain))
	if len(h.Hosts) > 0 {
		intel.Technologies = mergeUnique(intel.Technologies, []string{fmt.Sprintf("hosts:%d (theHarvester)", len(h.Hosts))})
		intel.Sources = mergeUnique(intel.Sources, []string{"theHarvester:crtsh,hackertarget"})
	}
	for _, p := range h.People {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		intel.DecisionMakers = append(intel.DecisionMakers, DecisionMaker{
			Name: p, Title: "Name from OSINT", Source: "theHarvester",
			Evidence: "theHarvester people result", Confidence: "low",
		})
	}
	intel.Provider = strings.Trim(intel.Provider+"+theHarvester", "+")
}

func applySpiderfoot(intel *PlaceIntel, events []spiderEvent) {
	if len(events) == 0 {
		return
	}
	intel.Sources = mergeUnique(intel.Sources, []string{"spiderfoot:lite"})
	var whoisOrgs []string
	for _, e := range events {
		switch e.Type {
		case "Email Address", "Email Address - Generic":
			intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails([]string{e.Data}, intel.Domain))
		case "Human Name", "Person":
			name := strings.TrimSpace(e.Data)
			if name != "" {
				intel.DecisionMakers = append(intel.DecisionMakers, DecisionMaker{
					Name: name, Source: "spiderfoot:" + e.Module, Evidence: e.Type, Confidence: "low",
				})
			}
		case "Company Name":
			intel.OrgStructure = append(intel.OrgStructure, OrgUnit{
				Name: strings.TrimSpace(e.Data), Role: "company", Evidence: "spiderfoot:" + e.Module,
			})
		case "Domain Whois":
			if m := whoisOrgRe.FindStringSubmatch(e.Data); len(m) > 1 {
				whoisOrgs = append(whoisOrgs, strings.TrimSpace(m[1]))
			}
			intel.Technologies = mergeUnique(intel.Technologies, []string{"WHOIS"})
		case "Raw DNS Data", "DNS TXT", "DNS MX":
			intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emailFindRe.FindAllString(e.Data, -1), intel.Domain))
		}
		if strings.Contains(strings.ToLower(e.Module), "opencorporates") && intel.CompanyRegistry == nil {
			intel.CompanyRegistry = &CompanyHit{Name: e.Data, Source: "spiderfoot-opencorporates"}
		}
	}
	for _, org := range uniqueStrings(whoisOrgs) {
		intel.OrgStructure = append(intel.OrgStructure, OrgUnit{Name: org, Role: "registrant", Evidence: "WHOIS"})
	}
	intel.Provider = strings.Trim(intel.Provider+"+spiderfoot", "+")
}

func applyPhoton(intel *PlaceIntel, emails []string, socials map[string]string) {
	intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emails, intel.Domain))
	if intel.Socials == nil {
		intel.Socials = map[string]string{}
	}
	for k, v := range socials {
		if intel.Socials[k] == "" && v != "" {
			intel.Socials[k] = v
		}
	}
	intel.Sources = mergeUnique(intel.Sources, []string{"photon"})
	intel.Provider = strings.Trim(intel.Provider+"+photon", "+")
}

func applyAccountHits(intel *PlaceIntel, label string, hits []string) {
	if len(hits) == 0 {
		return
	}
	intel.Technologies = mergeUnique(intel.Technologies, []string{fmt.Sprintf("%s_accounts:%d", label, len(hits))})
	if intel.Socials == nil {
		intel.Socials = map[string]string{}
	}
	for i, h := range hits {
		if i >= 12 {
			break
		}
		key := label + "_" + fmt.Sprintf("%d", i+1)
		intel.Socials[key] = h
	}
	intel.Sources = mergeUnique(intel.Sources, []string{label})
	intel.Provider = strings.Trim(intel.Provider+"+"+label, "+")
}

func applyAmass(intel *PlaceIntel, hosts []string) {
	if len(hosts) == 0 {
		return
	}
	intel.Technologies = mergeUnique(intel.Technologies, []string{fmt.Sprintf("amass_hosts:%d", len(hosts))})
	intel.Sources = mergeUnique(intel.Sources, []string{"amass:passive"})
	intel.Provider = strings.Trim(intel.Provider+"+amass", "+")
}
