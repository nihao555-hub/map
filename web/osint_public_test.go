package web

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLookupRDAPWhoDat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hit, emails, err := lookupRDAPWhoDat(ctx, "fore.coffee")
	if err != nil {
		t.Fatalf("rdap: %v", err)
	}
	t.Logf("hit=%#v emails=%v", hit, emails)
	if hit == nil {
		t.Fatalf("expected rdap/who-dat hit")
	}
}

func TestLookupWikipediaExtractAlfamart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	makers, extract, err := lookupWikipediaExtract(ctx, "Alfamart")
	if err != nil {
		t.Fatalf("wiki: %v", err)
	}
	if extract == "" {
		t.Fatalf("expected extract")
	}
	t.Logf("extract=%s makers=%v", truncateRunes(extract, 160), makers)
}

func TestLookupWaybackCDX(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	emails, makers, urls, err := lookupWaybackTeamPages(ctx, "https://fore.coffee", "fore.coffee")
	if err != nil {
		t.Logf("wayback soft-fail: %v", err)
		return
	}
	t.Logf("emails=%v makers=%d urls=%v", emails, len(makers), urls)
}

func TestBuildPlaceIntelPublicSourcesGordi(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	dir := t.TempDir()
	svc := &Service{dataFolder: dir}
	place := Place{
		Title: "Gordi HQ", Website: "http://www.gordi.id/", Address: "Jakarta, Indonesia",
		PlaceID: "gordi_live", Emails: "hi@gordi.id", Category: "Coffee",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	intel, err := svc.BuildPlaceIntel(ctx, "live-gordi", place)
	if err != nil {
		t.Fatalf("intel: %v", err)
	}
	b, _ := json.MarshalIndent(intel, "", "  ")
	_ = os.WriteFile("/opt/cursor/artifacts/intel-gordi-live.json", b, 0o644)
	t.Logf("provider=%s conf=%s emails=%d makers=%d named=%d sources=%d registry=%v",
		intel.Provider, intel.Confidence, len(intel.ExtraEmails), len(intel.DecisionMakers),
		countNamedPeople(intel.DecisionMakers), len(intel.Sources), intel.CompanyRegistry != nil)
	for _, d := range intel.DecisionMakers {
		if looksLikeRealPerson(d) {
			t.Logf("NAMED %s | %s | %s", d.Name, d.Title, d.Source)
		}
	}
	blob := strings.ToLower(intel.Provider + " " + strings.Join(intel.Sources, " "))
	okSrc := false
	for _, n := range []string{"rdap", "wikipedia", "wikidata", "katana", "wayback", "crtsh", "gleif", "ddg", "photon", "theharvester"} {
		if strings.Contains(blob, n) {
			okSrc = true
			break
		}
	}
	if !okSrc {
		t.Fatalf("expected public/osint providers in %s", intel.Provider)
	}
}
