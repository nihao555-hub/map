package engine

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const sherlockDataURL = "https://raw.githubusercontent.com/sherlock-project/sherlock/master/sherlock_project/resources/data.json"

// sherlockSite is the subset of sherlock-project/sherlock data.json we need.
type sherlockSite struct {
	URL    string `json:"url"`
	NSFW   bool   `json:"isNSFW"`
	Schema string `json:"$schema"`
}

type sherlockTemplate struct {
	Platform string
	URL      string
}

// parseSherlockSocials keeps Sherlock sites we can verify as a company
// social homepage. LinkedIn /in/ is rewritten to /company/.
func parseSherlockSocials(raw []byte) []sherlockTemplate {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	byPlat := map[string]sherlockTemplate{}
	for name, blob := range doc {
		if name == "$schema" {
			continue
		}
		var site sherlockSite
		if err := json.Unmarshal(blob, &site); err != nil || site.NSFW || !strings.Contains(site.URL, "{}") {
			continue
		}
		if strings.Contains(site.URL, "{}.") {
			continue
		}
		tmpl := site.URL
		if strings.Contains(strings.ToLower(tmpl), "linkedin.com/in/") {
			tmpl = "https://www.linkedin.com/company/{}"
		}
		hit, ok := ParseSocialURL(strings.ReplaceAll(tmpl, "{}", "signify"), "", "")
		if !ok || !isSocialHomepage(hit) || !sherlockKeepPlatform[hit.Platform] {
			continue
		}
		if _, exists := byPlat[hit.Platform]; exists && hit.Platform != PlatformLinkedIn {
			continue
		}
		byPlat[hit.Platform] = sherlockTemplate{Platform: hit.Platform, URL: tmpl}
	}
	for _, raw := range sameHandleURLs("signify") {
		hit, ok := ParseSocialURL(raw, "", "")
		if !ok || !sherlockKeepPlatform[hit.Platform] {
			continue
		}
		if _, exists := byPlat[hit.Platform]; exists {
			continue
		}
		byPlat[hit.Platform] = sherlockTemplate{
			Platform: hit.Platform,
			URL:      strings.ReplaceAll(raw, "signify", "{}"),
		}
	}
	out := make([]sherlockTemplate, 0, len(byPlat))
	for _, t := range byPlat {
		out = append(out, t)
	}
	return out
}

func sherlockURLs(handle string, templates []sherlockTemplate) []string {
	h := strings.Trim(handle, "/")
	if h == "" {
		return nil
	}
	if len(templates) == 0 {
		return sameHandleURLs(h)
	}
	out := make([]string, 0, len(templates))
	seen := map[string]struct{}{}
	for _, t := range templates {
		raw := strings.ReplaceAll(t.URL, "{}", h)
		if raw == "" {
			continue
		}
		key := strings.ToLower(raw)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func trustedSherlockHandles(row Merchant) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(h string) {
		h = probeableHandle(h)
		if h == "" {
			return
		}
		key := strings.ToLower(h)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, h)
	}
	add(handleFromHomepage(row.Homepage))
	for _, p := range row.Profiles {
		if p.Platform == PlatformWebsite {
			add(handleFromHomepage(p.URL))
			continue
		}
		add(p.Handle)
		if hit, ok := ParseSocialURL(p.URL, "", ""); ok {
			add(hit.Handle)
		}
	}
	return out
}

func distinctiveNameHandle(name string) string {
	compact := strings.ReplaceAll(foldLegalName(name), " ", "")
	h := probeableHandle(compact)
	if h == "" || utf8.RuneCountInString(h) < 6 {
		return ""
	}
	return h
}

func distinctiveNeedsLinkedIn(handle string) bool {
	return true
}

var sherlockKeepPlatform = map[string]bool{
	PlatformFacebook:  true,
	PlatformInstagram: true,
	PlatformLinkedIn:  true,
	PlatformYouTube:   true,
	PlatformX:         true,
}

func filterSherlockHits(got []Profile, needLinkedIn bool) []Profile {
	if len(got) == 0 {
		return nil
	}
	keep := got[:0]
	hasLI := false
	core := 0
	for _, p := range got {
		if !sherlockKeepPlatform[p.Platform] {
			continue
		}
		if p.Handle == "u" || p.Handle == "U" || strings.HasSuffix(strings.ToLower(p.URL), "x.com/u") {
			continue
		}
		keep = append(keep, p)
		switch p.Platform {
		case PlatformLinkedIn:
			if strings.Contains(strings.ToLower(p.URL), "/company/") {
				hasLI = true
			}
		case PlatformFacebook, PlatformInstagram, PlatformYouTube:
			core++
		}
	}
	if needLinkedIn && !hasLI {
		return nil
	}
	if !hasLI && core < 2 {
		return nil
	}
	return keep
}
