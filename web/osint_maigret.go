package web

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Maigret：直接调用本地克隆 https://github.com/soxoj/maigret（tools/maigret + osint-extra-venv）。
// 只对门控通过的具名决策人跑，避免把邮箱本地部分当用户名刷出噪声。

var maigretUserCleanRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// enrichMaigretProfiles 对最多 maxPeople 位具名决策人跑轻量 Maigret，写入 Profiles。
func enrichMaigretProfiles(ctx context.Context, intel *PlaceIntel, st OSINTStatus) {
	if intel == nil || !st.Maigret {
		return
	}
	budget, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()

	const maxPeople = 2
	type job struct {
		idx  int
		user string
	}
	var jobs []job
	seenUser := map[string]bool{}
	for i, d := range intel.DecisionMakers {
		if len(jobs) >= maxPeople {
			break
		}
		if !QualifiesAsDecisionMaker(d, intel.Title) {
			continue
		}
		for _, u := range maigretUsernamesFor(d) {
			u = strings.ToLower(strings.TrimSpace(u))
			if len(u) < 3 || seenUser[u] {
				continue
			}
			seenUser[u] = true
			jobs = append(jobs, job{idx: i, user: u})
			break
		}
	}
	if len(jobs) == 0 {
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, j := range jobs {
		j := j
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits, err := runMaigretLite(budget, j.user)
			if err != nil || len(hits) == 0 {
				return
			}
			urls := filterMaigretProfileURLs(hits)
			if len(urls) == 0 {
				return
			}
			mu.Lock()
			intel.DecisionMakers[j.idx].Profiles = mergeUnique(intel.DecisionMakers[j.idx].Profiles, urls)
			if intel.DecisionMakers[j.idx].Evidence == "" {
				intel.DecisionMakers[j.idx].Evidence = "maigret username:" + j.user
			} else if !strings.Contains(strings.ToLower(intel.DecisionMakers[j.idx].Evidence), "maigret") {
				intel.DecisionMakers[j.idx].Evidence = truncateRunes(
					intel.DecisionMakers[j.idx].Evidence+"; maigret:"+j.user, 140)
			}
			intel.Sources = mergeUnique(intel.Sources, []string{"maigret"})
			if !strings.Contains(intel.Provider, "maigret") {
				intel.Provider = strings.Trim(intel.Provider+"+maigret", "+")
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
}

func maigretUsernamesFor(d DecisionMaker) []string {
	var out []string
	if li := strings.TrimSpace(d.LinkedIn); strings.Contains(strings.ToLower(li), "linkedin.com/in/") {
		if slug := linkedInSlugUsername(li); slug != "" {
			out = append(out, slug)
		}
	}
	name := strings.TrimSpace(d.Name)
	if IsValidPersonName(name) {
		parts := strings.Fields(name)
		joined := strings.ToLower(strings.Join(parts, ""))
		dotted := strings.ToLower(strings.Join(parts, "."))
		under := strings.ToLower(strings.Join(parts, "_"))
		out = append(out, joined, dotted, under)
		if len(parts) >= 2 {
			out = append(out, strings.ToLower(parts[0]+parts[len(parts)-1]))
		}
	}
	if em := strings.TrimSpace(d.Email); em != "" {
		local, _, ok := strings.Cut(em, "@")
		if ok {
			local = maigretUserCleanRe.ReplaceAllString(strings.ToLower(local), "")
			if len(local) >= 3 && !isGenericEmailLocal(local) {
				out = append(out, local)
			}
		}
	}
	return uniqueStrings(out)
}

func linkedInSlugUsername(profileURL string) string {
	low := strings.ToLower(profileURL)
	idx := strings.Index(low, "/in/")
	if idx < 0 {
		return ""
	}
	slug := profileURL[idx+4:]
	if i := strings.IndexAny(slug, "/?#"); i >= 0 {
		slug = slug[:i]
	}
	if dec, err := url.PathUnescape(slug); err == nil {
		slug = dec
	}
	slug = maigretUserCleanRe.ReplaceAllString(slug, "")
	if len(slug) < 3 {
		return ""
	}
	return slug
}

func isGenericEmailLocal(local string) bool {
	generic := []string{
		"info", "sales", "admin", "contact", "hello", "support", "office",
		"mail", "privacy", "noreply", "no-reply", "marketing", "cs", "hq",
	}
	for _, g := range generic {
		if local == g || strings.HasPrefix(local, g+".") || strings.HasPrefix(local, g+"_") || strings.HasPrefix(local, g+"-") {
			return true
		}
	}
	return false
}

func filterMaigretProfileURLs(hits []string) []string {
	var out []string
	for _, h := range hits {
		h = strings.TrimSpace(h)
		low := strings.ToLower(h)
		if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") {
			out = append(out, h)
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// maigretBin 本地 maigret CLI（editable 安装自 tools/maigret）。
func maigretBin() string {
	if p := strings.TrimSpace(os.Getenv("MAIGRET_BIN")); p != "" {
		return p
	}
	root := repoRoot()
	for _, cand := range []string{
		filepath.Join(root, "tools", "osint-extra-venv", "bin", "maigret"),
		filepath.Join(filepath.Dir(osintExtraPython()), "maigret"),
	} {
		if fileExists(cand) {
			return cand
		}
	}
	return ""
}
