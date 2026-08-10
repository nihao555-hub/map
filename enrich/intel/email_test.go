package intel

import (
	"testing"
)

func TestEmbeddedDisposableDomainsLoaded(t *testing.T) {
	t.Parallel()

	domains := embeddedDisposableDomains()
	if len(domains) < 1000 {
		t.Fatalf("expected thousands of disposable domains, got %d", len(domains))
	}

	want := map[string]bool{
		"mailinator.com":    false,
		"guerrillamail.com": false,
		"10minutemail.com":  false,
		"yopmail.com":       false,
		"trashmail.com":     false,
	}

	for _, d := range domains {
		if _, ok := want[d]; ok {
			want[d] = true
		}
	}

	for domain, found := range want {
		if !found {
			t.Errorf("missing expected disposable domain %q", domain)
		}
	}
}
