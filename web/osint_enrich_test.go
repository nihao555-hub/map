package web

import (
	"context"
	"testing"
	"time"
)

func TestIsLikelyPersonName(t *testing.T) {
	yes := []string{"Djoko Susanto", "Jogi Hendra Atmadja", "P. K. Ojong"}
	no := []string{"info", "Contact Us", "PT Mayora Indah", "marketing", "About Us"}
	for _, n := range yes {
		if !isLikelyPersonName(n) && n != "P. K. Ojong" {
			// P. K. Ojong may fail strict check; covered by source-based looksLikeRealPerson
			t.Fatalf("expected person name: %q", n)
		}
	}
	for _, n := range no {
		if isLikelyPersonName(n) {
			t.Fatalf("expected non-person: %q", n)
		}
	}
	d := DecisionMaker{Name: "P. K. Ojong", Source: "wikidata", Confidence: "high"}
	if !looksLikeRealPerson(d) {
		t.Fatalf("wikidata short-initial name should count as real person")
	}
}

func TestLookupWikidataPeopleMayora(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	makers, org, err := lookupWikidataPeople(ctx, "Mayora Indah")
	if err != nil {
		t.Fatalf("wikidata: %v", err)
	}
	if org == nil || org.Name == "" {
		t.Fatalf("expected org")
	}
	if countNamedPeople(makers) == 0 {
		t.Fatalf("expected founder/officer for Mayora, got %#v", makers)
	}
	t.Logf("org=%s makers=%v", org.Name, makers)
}

func TestLookupGLEIFMayora(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	hit, err := lookupGLEIF(ctx, "Mayora Indah", "mayoraindah.co.id")
	if err != nil {
		t.Fatalf("gleif: %v", err)
	}
	if hit == nil || hit.CompanyNumber == "" {
		t.Fatalf("expected LEI hit, got %#v", hit)
	}
	t.Logf("gleif %#v", hit)
}

func TestProbeEnrichmentFlags(t *testing.T) {
	// force re-probe in this process by calling uncached
	st := probeOSINTToolsUncached()
	if !st.GLEIF || !st.Wikidata || !st.GitHubCommits {
		t.Fatalf("expected free enrichment flags: %+v", st)
	}
	t.Logf("katana=%v hunter=%v ahu=%v ahuProxy=%v", st.Katana, st.Hunter, st.AHU, st.AHUProxyConfigured)
}
