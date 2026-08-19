package engine

import (
	"strings"
	"unicode/utf8"
)

// packCompanyHits collapses one company/person into a single card with all
// verified social homepages attached on Profiles.
func packCompanyHits(hits []Hit) []Hit {
	if len(hits) < 2 {
		return hits
	}
	used := make([]bool, len(hits))
	out := make([]Hit, 0, len(hits))
	for i := range hits {
		if used[i] {
			continue
		}
		used[i] = true
		card := hits[i]
		for j := i + 1; j < len(hits); j++ {
			if used[j] || !sameCompany(card, hits[j]) {
				continue
			}
			used[j] = true
			card = mergeCompanyCard(card, hits[j])
		}
		out = append(out, card)
	}
	return out
}

func sameCompany(a, b Hit) bool {
	if aid, bid := merchantExtID(a), merchantExtID(b); aid != "" && aid == bid {
		return true
	}
	if a.HomepageURL != "" && b.Extra != nil && strings.EqualFold(strings.TrimSpace(b.Extra["via"]), a.HomepageURL) {
		return true
	}
	if b.HomepageURL != "" && a.Extra != nil && strings.EqualFold(strings.TrimSpace(a.Extra["via"]), b.HomepageURL) {
		return true
	}
	ha := strings.ToLower(probeableHandle(a.Handle))
	hb := strings.ToLower(probeableHandle(b.Handle))
	if ha != "" && ha == hb {
		return true
	}
	na, nb := foldSearchText(a.Name), foldSearchText(b.Name)
	return na != "" && na == nb && utf8.RuneCountInString(strings.TrimSpace(a.Name)) >= 4
}

func mergeCompanyCard(card, extra Hit) Hit {
	add := func(h Hit) {
		if h.HomepageURL == "" {
			return
		}
		if strings.EqualFold(h.HomepageURL, card.HomepageURL) {
			card.Verified = card.Verified || h.Verified
			card.Handle = firstNonEmpty(card.Handle, h.Handle)
			return
		}
		for i := range card.Profiles {
			if strings.EqualFold(card.Profiles[i].HomepageURL, h.HomepageURL) {
				card.Profiles[i].Verified = card.Profiles[i].Verified || h.Verified
				return
			}
			if card.Profiles[i].Platform == h.Platform && h.Platform != "" && h.Platform != PlatformWebsite {
				if h.Verified && !card.Profiles[i].Verified {
					card.Profiles[i] = stripProfiles(h)
				}
				return
			}
		}
		card.Profiles = append(card.Profiles, stripProfiles(h))
		if h.Verified {
			card.Score += 6
		}
	}
	if extra.HomepageURL != "" {
		if card.HomepageURL == "" {
			card.HomepageURL = extra.HomepageURL
			card.MessageURL = extra.MessageURL
			card.Platform = extra.Platform
			card.Handle = extra.Handle
			card.Verified = extra.Verified
		} else if card.Platform == PlatformWebsite && extra.Platform != PlatformWebsite && extra.Verified {
			// keep company website as primary, social goes to profiles
			add(extra)
		} else if extra.Platform == PlatformWebsite && card.Platform != PlatformWebsite {
			old := stripProfiles(card)
			card.HomepageURL = extra.HomepageURL
			card.MessageURL = extra.MessageURL
			card.Platform = extra.Platform
			card.Verified = extra.Verified
			add(old)
		} else {
			add(extra)
		}
	}
	for _, p := range extra.Profiles {
		add(p)
	}
	if extra.Score > card.Score {
		card.Score = extra.Score
	}
	return card
}

func stripProfiles(h Hit) Hit {
	h.Profiles = nil
	return h
}

func sortHitsByCountry(hits []Hit) []Hit {
	if len(hits) < 2 {
		return hits
	}
	out := append([]Hit(nil), hits...)
	less := func(i, j int) bool {
		ci := strings.ToUpper(strings.TrimSpace(out[i].Country))
		cj := strings.ToUpper(strings.TrimSpace(out[j].Country))
		if ci == "" {
			ci = "ZZ"
		}
		if cj == "" {
			cj = "ZZ"
		}
		if ci != cj {
			return ci < cj
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return foldSearchText(out[i].Name) < foldSearchText(out[j].Name)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if less(j, i) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func profileCount(hits []Hit) int {
	n := 0
	for _, h := range hits {
		if h.HomepageURL != "" {
			n++
		}
		n += len(h.Profiles)
	}
	return n
}
