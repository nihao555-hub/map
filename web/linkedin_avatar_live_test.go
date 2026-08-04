//go:build liveintel

package web

import "testing"

func TestLiveLinkedInPublicAvatar(t *testing.T) {
	av, headline, _ := fetchLinkedInPublicMeta("https://www.linkedin.com/in/williamhgates/")
	t.Logf("avatar=%s headline=%s", av, headline)
	if av == "" || !isRealAvatarURL(av) {
		t.Fatalf("expected real LinkedIn avatar, got %q", av)
	}
	if headline == "" {
		t.Log("headline empty (acceptable on some regions)")
	}
}
