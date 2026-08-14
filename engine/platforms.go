package engine

import "strings"

// PeoplePlatforms is every social network the engine can parse and search.
// Exhibition / customs are separate modules and are not listed here.
var PeoplePlatforms = []string{
	PlatformFacebook,
	PlatformLinkedIn,
	PlatformInstagram,
	PlatformYouTube,
	PlatformTikTok,
	PlatformX,
	PlatformPinterest,
	PlatformThreads,
	PlatformDouyin,
}

// PlatformInfo describes a people-search platform for the Web UI.
type PlatformInfo struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
}

// PeoplePlatformCatalog returns the platforms the engine actually searches.
func PeoplePlatformCatalog() []PlatformInfo {
	defaults := make(map[string]bool, len(DefaultPeoplePlatforms))
	for _, p := range DefaultPeoplePlatforms {
		defaults[p] = true
	}

	out := make([]PlatformInfo, 0, len(PeoplePlatforms))
	for _, p := range PeoplePlatforms {
		out = append(out, PlatformInfo{
			ID:      p,
			Label:   PeoplePlatformLabel(p),
			Default: defaults[p],
		})
	}

	return out
}

// PeoplePlatformLabel is the display name shown in chips and tables.
func PeoplePlatformLabel(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case PlatformFacebook:
		return "Facebook"
	case PlatformLinkedIn:
		return "LinkedIn"
	case PlatformInstagram:
		return "Instagram"
	case PlatformYouTube:
		return "YouTube"
	case PlatformTikTok:
		return "TikTok"
	case PlatformX:
		return "X"
	case PlatformPinterest:
		return "Pinterest"
	case PlatformThreads:
		return "Threads"
	case PlatformDouyin:
		return "抖音"
	default:
		return id
	}
}

func wantedPeoplePlatforms(in []string) map[string]bool {
	known := make(map[string]bool, len(PeoplePlatforms))
	for _, p := range PeoplePlatforms {
		known[p] = true
	}

	wanted := make(map[string]bool, len(in))
	for _, p := range in {
		id := strings.ToLower(strings.TrimSpace(p))
		if known[id] {
			wanted[id] = true
		}
	}

	if len(wanted) == 0 {
		for _, p := range DefaultPeoplePlatforms {
			wanted[p] = true
		}
	}

	return wanted
}
