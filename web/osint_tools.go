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

// OSINT 工具路径（相对仓库根或绝对路径，可用环境变量覆盖）
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
	// 常见：进程 cwd 为仓库根；否则尝试可执行文件旁
	wd, err := os.Getwd()
	if err == nil {
		if _, err2 := os.Stat(filepath.Join(wd, "tools")); err2 == nil {
			return wd
		}
		// web 测试可能在子目录
		if _, err2 := os.Stat(filepath.Join(wd, "..", "tools")); err2 == nil {
			return filepath.Clean(filepath.Join(wd, ".."))
		}
	}
	return "/workspace"
}

// OSINTToolsAvailable 探测 theHarvester / SpiderFoot 是否已安装。
func OSINTToolsAvailable() (harvester, spiderfoot bool) {
	bin, args := theHarvesterCmd()
	thDir := filepath.Join(repoRoot(), "tools", "theHarvester")
	if st, err := os.Stat(thDir); err == nil && st.IsDir() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, append(args, "-h")...)
		harvester = cmd.Run() == nil
	}
	py, sf := spiderfootPython()
	if _, err := os.Stat(py); err == nil {
		if _, err2 := os.Stat(sf); err2 == nil {
			spiderfoot = true
		}
	}
	return harvester, spiderfoot
}

type harvesterOut struct {
	Emails []string `json:"emails"`
	Hosts  []string `json:"hosts"`
	People []string `json:"people"`
	Interesting []string `json:"interesting_urls"`
}

// runTheHarvester 对域名跑免费源，返回邮箱/主机等。
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
	_ = cmd.Run() // 即使部分源失败也可能写出 JSON

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
	// 有的版本只把邮箱嵌在 hosts 文本里
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

// runSpiderfootLite 精简模块扫描：DNS/WHOIS/证书/公司名/人名/邮箱/OpenCorporates。
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
	// 输出可能是 JSON 数组，或带噪声前缀
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
			Name:       p,
			Title:      "Name from OSINT",
			Source:     "theHarvester",
			Evidence:   "theHarvester people result",
			Confidence: "low",
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
					Name:       name,
					Source:     "spiderfoot:" + e.Module,
					Evidence:   e.Type,
					Confidence: "low",
				})
			}
		case "Company Name":
			intel.OrgStructure = append(intel.OrgStructure, OrgUnit{
				Name:     strings.TrimSpace(e.Data),
				Role:     "company",
				Evidence: "spiderfoot:" + e.Module,
			})
		case "Domain Whois":
			if m := whoisOrgRe.FindStringSubmatch(e.Data); len(m) > 1 {
				whoisOrgs = append(whoisOrgs, strings.TrimSpace(m[1]))
			}
			intel.Technologies = mergeUnique(intel.Technologies, []string{"WHOIS"})
		case "Internet Name", "Domain Name", "IP Address":
			// footprint only
		case "Raw DNS Data", "DNS TXT", "DNS MX":
			intel.ExtraEmails = mergeUnique(intel.ExtraEmails, filterPublicEmails(emailFindRe.FindAllString(e.Data, -1), intel.Domain))
		}
		if strings.Contains(strings.ToLower(e.Module), "opencorporates") && intel.CompanyRegistry == nil {
			intel.CompanyRegistry = &CompanyHit{
				Name:   e.Data,
				Source: "spiderfoot-opencorporates",
			}
		}
	}
	for _, org := range uniqueStrings(whoisOrgs) {
		intel.OrgStructure = append(intel.OrgStructure, OrgUnit{
			Name: org, Role: "registrant", Evidence: "WHOIS",
		})
	}
	intel.Provider = strings.Trim(intel.Provider+"+spiderfoot", "+")
}
