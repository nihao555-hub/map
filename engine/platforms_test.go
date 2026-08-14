package engine

import "testing"

func TestPeoplePlatformCatalogMatchesSearchableSet(t *testing.T) {
	cat := PeoplePlatformCatalog()
	if len(cat) != len(PeoplePlatforms) {
		t.Fatalf("catalog=%d platforms=%d", len(cat), len(PeoplePlatforms))
	}

	seen := map[string]bool{}
	defaults := 0
	for _, p := range cat {
		if p.ID == "" || p.Label == "" {
			t.Fatalf("empty platform %+v", p)
		}
		if seen[p.ID] {
			t.Fatalf("duplicate %s", p.ID)
		}
		seen[p.ID] = true
		if p.Default {
			defaults++
		}
	}

	if !seen[PlatformFacebook] || !seen[PlatformDouyin] {
		t.Fatalf("missing facebook/douyin: %+v", cat)
	}

	if seen[PlatformExhibition] || seen[PlatformCustoms] {
		t.Fatal("exhibition/customs must not appear as people platforms")
	}

	if defaults != len(DefaultPeoplePlatforms) {
		t.Fatalf("defaults=%d want %d", defaults, len(DefaultPeoplePlatforms))
	}
}

func TestWantedPeoplePlatformsIgnoresUnknown(t *testing.T) {
	got := wantedPeoplePlatforms([]string{" facebook ", "not-a-site", "tiktok"})
	if !got[PlatformFacebook] || !got[PlatformTikTok] {
		t.Fatalf("got=%v", got)
	}
	if got["not-a-site"] {
		t.Fatal("unknown platform leaked")
	}
	if got[PlatformDouyin] {
		t.Fatal("unselected known platform should stay off")
	}

	fallback := wantedPeoplePlatforms(nil)
	for _, p := range DefaultPeoplePlatforms {
		if !fallback[p] {
			t.Fatalf("missing default %s", p)
		}
	}
}
