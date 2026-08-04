package web

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// CrossLinked：直接调用本地克隆的 https://github.com/m8sec/CrossLinked
// 路径：tools/CrossLinked + tools/crosslinked-venv（见 tools/install_osint.sh）

func crosslinkedBin() string {
	if p := strings.TrimSpace(os.Getenv("CROSSLINKED_BIN")); p != "" {
		return p
	}
	root := repoRoot()
	cand := filepath.Join(root, "tools", "crosslinked-venv", "bin", "crosslinked")
	if fileExists(cand) {
		return cand
	}
	// 退回：用 venv python 跑仓库入口
	py := filepath.Join(root, "tools", "crosslinked-venv", "bin", "python")
	script := filepath.Join(root, "tools", "CrossLinked", "crosslinked.py")
	if fileExists(py) && fileExists(script) {
		return py + "\x00" + script // 特殊：双段，runCrossLinkedCLI 识别
	}
	return ""
}

// CrossLinkedAvailable 本地是否已安装 CrossLinked 项目。
func CrossLinkedAvailable() bool {
	return crosslinkedBin() != "" && fileExists(filepath.Join(repoRoot(), "tools", "CrossLinked", "crosslinked.py"))
}

// lookupCrossLinkedEmployees 调用本地 CrossLinked CLI，解析 names.csv。
func lookupCrossLinkedEmployees(ctx context.Context, company, domain string) ([]DecisionMaker, error) {
	company = strings.TrimSpace(company)
	if company == "" {
		return nil, fmt.Errorf("empty company")
	}
	if !CrossLinkedAvailable() {
		return nil, fmt.Errorf("CrossLinked not installed (bash tools/install_osint.sh)")
	}

	// CrossLinked 文档：公司名用 LinkedIn 上的写法，不要只用域名
	target := company
	if brand := companyBrandToken(company); brand != "" && len(brand) >= 3 {
		// 先跑全名；若为空再试品牌（见下方二次调用）
		_ = brand
	}

	people, err := runCrossLinkedCLI(ctx, target, domain)
	if err != nil {
		return nil, err
	}
	if len(people) == 0 {
		if brand := companyBrandToken(company); brand != "" && !strings.EqualFold(brand, company) {
			more, err2 := runCrossLinkedCLI(ctx, brand, domain)
			if err2 == nil {
				people = append(people, more...)
			}
		}
	}
	out := dedupeDecisionMakers(people)
	if len(out) > 12 {
		out = out[:12]
	}
	return out, nil
}

func runCrossLinkedCLI(ctx context.Context, company, domain string) ([]DecisionMaker, error) {
	bin := crosslinkedBin()
	if bin == "" {
		return nil, fmt.Errorf("crosslinked binary missing")
	}

	tmpDir, err := os.MkdirTemp("", "crosslinked-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	outBase := filepath.Join(tmpDir, "names")
	nformat := "{first}.{last}"
	if d := strings.TrimSpace(domain); d != "" {
		nformat = "{first}.{last}@" + d
	}

	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	args := []string{
		"-f", nformat,
		"-t", "20",
		"-j", "0.8",
		"-o", outBase,
		"--search", "bing,google",
		company,
	}
	if proxy := crosslinkedProxyArg(); proxy != "" {
		args = append([]string{"--proxy", proxy}, args...)
	}

	var cmdName string
	var cmdArgs []string
	if strings.Contains(bin, "\x00") {
		parts := strings.SplitN(bin, "\x00", 2)
		cmdName, cmdArgs = parts[0], append([]string{parts[1]}, args...)
	} else {
		cmdName, cmdArgs = bin, args
	}

	var stdout, stderr strings.Builder
	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...) //nolint:gosec
	cmd.Dir = tmpDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	csvPath := outBase + ".csv"
	people, perr := parseCrossLinkedCSV(csvPath)
	if perr != nil {
		msg := strings.TrimSpace(stderr.String() + "\n" + stdout.String())
		if msg == "" {
			msg = perr.Error()
		}
		return nil, fmt.Errorf("crosslinked: %s", truncateRunes(msg, 160))
	}
	return people, nil
}

func crosslinkedProxyArg() string {
	// CrossLinked --proxy 原样塞进 requests proxies；socks5h://user:pass@host:port 需 PySocks
	for _, k := range []string{"CROSSLINKED_PROXY", "AHU_PROXY", "MEDIA_PROXY", "HTTPS_PROXY", "HTTP_PROXY"} {
		raw := firstProxyLine(os.Getenv(k))
		if raw == "" {
			continue
		}
		raw = strings.TrimSpace(raw)
		low := strings.ToLower(raw)
		if strings.HasPrefix(low, "socks5://") || strings.HasPrefix(low, "socks5h://") ||
			strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
			return raw
		}
		// host:port → 当作 http 代理
		if strings.Contains(raw, ":") && !strings.Contains(raw, "/") {
			return "http://" + raw
		}
	}
	return ""
}

// parseCrossLinkedCSV 解析 CrossLinked 写出的 names.csv：
// Datetime,Search,Name,Title,URL,rawText
func parseCrossLinkedCSV(path string) ([]DecisionMaker, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty csv")
	}

	var makers []DecisionMaker
	seen := map[string]bool{}
	for i, row := range rows {
		if i == 0 && len(row) > 0 && strings.EqualFold(strings.TrimSpace(row[0]), "Datetime") {
			continue
		}
		// 至少 Name 列
		name, title, li, raw := "", "", "", ""
		switch {
		case len(row) >= 6:
			name, title, li, raw = row[2], row[3], row[4], row[5]
		case len(row) >= 3:
			name = row[2]
			if len(row) > 3 {
				title = row[3]
			}
			if len(row) > 4 {
				li = row[4]
			}
		default:
			continue
		}
		name = titleCasePersonName(strings.TrimSpace(name))
		title = strings.TrimSpace(title)
		if strings.EqualFold(title, "N/A") {
			title = ""
		}
		li = cleanLinkedInURL(strings.TrimSpace(li))
		if name == "" {
			continue
		}
		if !IsValidPersonName(name) {
			continue
		}
		key := strings.ToLower(name + "|" + li)
		if seen[key] {
			continue
		}
		seen[key] = true
		conf := "medium"
		if title != "" {
			conf = "high"
		}
		makers = append(makers, DecisionMaker{
			Name:       name,
			Title:      firstNonEmpty(title, "LinkedIn profile"),
			LinkedIn:   li,
			Source:     "crosslinked",
			Evidence:   truncateRunes(firstNonEmpty(raw, "CrossLinked CLI"), 140),
			Confidence: conf,
		})
	}
	return makers, nil
}

func titleCasePersonName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	parts := strings.Fields(strings.ToLower(name))
	for i, p := range parts {
		runes := []rune(p)
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}
