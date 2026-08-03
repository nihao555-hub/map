package web

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestIsLikelyPersonName(t *testing.T) {
	yes := []string{"Djoko Susanto", "Jogi Hendra Atmadja", "Ahmed Rubaie", "Hugh Njemanze"}
	no := []string{
		"info", "Contact Us", "PT Mayora Indah", "marketing", "About Us",
		"Partner Directory", "Threat Intel Sharing", "for your organization",
		"Executive Leadership", "Core Values",
	}
	for _, n := range yes {
		if !isLikelyPersonName(n) {
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
	text := "Executive Leadership Ahmed Rubaie Chief Executive Officer The Anomali Team. Partner Directory Trial and purchase threat intelligence."
	got := extractPeopleFromText(text, "https://example.com/about")
	if countNamedPeople(got) == 0 {
		t.Fatalf("expected Ahmed Rubaie from title pattern, got %#v", got)
	}
	for _, m := range got {
		for _, bad := range []string{"Partner", "Directory", "Threat", "Trial"} {
			if strings.Contains(m.Name, bad) {
				t.Fatalf("false positive name: %q", m.Name)
			}
		}
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
