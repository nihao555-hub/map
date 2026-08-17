package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"
)

const (
	maxAttachHits     = 24
	attachBudget      = 18 * time.Second
	attachConcurrency = 6
)

// attachMissingSocials fills empty social columns on already-accepted cards.
// OSM-only shops were skipped by enrich (no official site) and public Facebook
// hits were dropped because the page title does not contain the Chinese product
// word. Here we look up the shop by name using the same public indexes
// (theHarvester / Photon pattern) plus live OSM contact:* tags, then Sherlock
// sister URLs once a handle exists.
func (c *Client) attachMissingSocials(ctx context.Context, hits []Hit, wanted map[string]bool) []Hit {
	if c == nil || c.DisablePublic || c.SkipExpand || ctx.Err() != nil || len(hits) == 0 {
		return hits
	}
	budget, cancel := context.WithTimeout(ctx, attachBudget)
	defer cancel()

	type job struct {
		idx int
		hit Hit
	}
	var jobs []job
	for i, h := range hits {
		if hitHasSocial(h) {
			continue
		}
		if strings.TrimSpace(h.Name) == "" {
			continue
		}
		jobs = append(jobs, job{idx: i, hit: h})
		if len(jobs) >= maxAttachHits {
			break
		}
	}
	if len(jobs) == 0 {
		return hits
	}

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(budget)
	g.SetLimit(attachConcurrency)
	for _, j := range jobs {
		j := j
		g.Go(func() error {
			got := c.lookupMerchantSocials(gctx, j.hit, wanted)
			if len(got) == 0 {
				return nil
			}
			mu.Lock()
			hits[j.idx] = attachProfilesToHit(hits[j.idx], got)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()
	return hits
}

func hitHasSocial(h Hit) bool {
	if isNonWebsiteSocial(h.Platform, h.HomepageURL) {
		return true
	}
	for _, p := range h.Profiles {
		if isNonWebsiteSocial(p.Platform, p.HomepageURL) {
			return true
		}
	}
	return false
}

func isNonWebsiteSocial(platform, rawURL string) bool {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" || platform == PlatformWebsite {
		return false
	}
	return strings.TrimSpace(rawURL) != "" && !isRegistryHomepageURL(rawURL)
}

func isRegistryHomepageURL(raw string) bool {
	u := strings.ToLower(raw)
	return strings.Contains(u, "openstreetmap.org") || strings.Contains(u, "gleif.org")
}

func (c *Client) lookupMerchantSocials(ctx context.Context, hit Hit, wanted map[string]bool) []Profile {
	if ctx.Err() != nil {
		return nil
	}
	extID := merchantExtID(hit)
	seen := map[string]struct{}{}
	var out []Profile
	add := func(p Profile) {
		if p.URL == "" || p.Platform == "" || p.Platform == PlatformWebsite {
			return
		}
		if len(wanted) > 0 && !wanted[p.Platform] {
			return
		}
		key := p.Platform + "|" + strings.ToLower(p.URL)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		if extID != "" {
			p.ExtID = extID
		}
		out = append(out, p)
	}
	for _, p := range hit.Profiles {
		if p.Platform == "" || p.Platform == PlatformWebsite || strings.TrimSpace(p.HomepageURL) == "" {
			continue
		}
		add(Profile{
			Platform: p.Platform,
			URL:      p.HomepageURL,
			Handle:   p.Handle,
			Verified: p.Verified,
			Source:   firstNonEmpty(hitExtra(p, "src"), "existing"),
		})
	}

	if strings.HasPrefix(extID, "osm:") {
		for _, p := range c.lookupOSMContactProfiles(ctx, extID, hit.Name) {
			add(p)
		}
	}
	if len(out) == 0 {
		for _, h := range c.searchMerchantNameSocials(ctx, hit) {
			if !socialBelongsToMerchant(hit.Name, h) {
				continue
			}
			add(Profile{
				Platform: h.Platform,
				URL:      h.HomepageURL,
				Handle:   h.Handle,
				Verified: h.Verified,
				Source:   "name-search",
			})
		}
	}
	if handle := firstProbeableSocialHandle(hit, out); handle != "" {
		for _, raw := range sameHandleURLs(handle) {
			if ctx.Err() != nil {
				break
			}
			h, ok := ParseSocialURL(raw, hit.Name, hit.Snippet)
			if !ok {
				continue
			}
			if len(wanted) > 0 && !wanted[h.Platform] {
				continue
			}
			key := h.Platform + "|" + strings.ToLower(h.HomepageURL)
			if _, ok := seen[key]; ok {
				continue
			}
			if !c.probeProfileExists(ctx, h.HomepageURL) {
				continue
			}
			add(Profile{
				Platform: h.Platform,
				URL:      h.HomepageURL,
				Handle:   h.Handle,
				Verified: true,
				Source:   "sherlock",
			})
		}
	}
	return out
}

func firstProbeableSocialHandle(hit Hit, rows []Profile) string {
	if h := probeableHandle(hit.Handle); h != "" {
		return h
	}
	for _, p := range rows {
		if h := probeableHandle(p.Handle); h != "" {
			return h
		}
	}
	if isRealHomepage(hit.HomepageURL) {
		return handleFromHomepage(hit.HomepageURL)
	}
	return ""
}

func (c *Client) lookupOSMContactProfiles(ctx context.Context, extID, name string) []Profile {
	kind, id, ok := parseOSMExtID(extID)
	if !ok || c == nil {
		return nil
	}
	q := fmt.Sprintf("[out:json][timeout:8];\n%s(%d);\nout tags;\n", kind, id)
	raw, err := c.fetchOverpass(ctx, q)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var doc overpassDoc
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Elements) == 0 {
		return nil
	}
	return osmTagProfiles(extID, name, doc.Elements[0].Tags)
}

func parseOSMExtID(extID string) (kind string, id int64, ok bool) {
	extID = strings.TrimSpace(extID)
	if !strings.HasPrefix(extID, "osm:") {
		return "", 0, false
	}
	parts := strings.Split(extID, ":")
	if len(parts) != 3 {
		return "", 0, false
	}
	kind = parts[1]
	if kind != "node" && kind != "way" && kind != "relation" {
		return "", 0, false
	}
	n, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return kind, n, true
}

func (c *Client) searchMerchantNameSocials(ctx context.Context, hit Hit) []Hit {
	name := strings.TrimSpace(hit.Name)
	if name == "" || c == nil || utf8.RuneCountInString(name) < 4 || foldSearchText(name) == "" {
		return nil
	}
	geo := strings.TrimSpace(firstNonEmpty(hitExtra(hit, "city"), CountryQueryToken(hit.Country, false)))
	q := `"` + name + `"`
	if geo != "" {
		q += " " + geo
	}
	q += " (site:facebook.com OR site:instagram.com OR site:linkedin.com/company)"
	items, _, err := c.searchOneIndex(ctx, q, "")
	if err != nil || len(items) == 0 {
		return nil
	}
	return items
}

func hitExtra(hit Hit, key string) string {
	if hit.Extra == nil {
		return ""
	}
	return strings.TrimSpace(hit.Extra[key])
}

func socialBelongsToMerchant(merchant string, hit Hit) bool {
	needle := foldSearchText(merchant)
	if needle == "" || utf8.RuneCountInString(strings.TrimSpace(merchant)) < 4 {
		return false
	}
	blob := foldSearchText(strings.Join([]string{hit.Name, hit.Title, hit.Handle, hit.Snippet}, " "))
	return strings.Contains(blob, needle)
}
