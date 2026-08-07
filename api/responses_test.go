package api

import "testing"

func TestScrapeRequestDefaultsForceEmail(t *testing.T) {
	req := ScrapeRequest{Email: false, FastMode: true}
	req.SetDefaults()
	if !req.Email {
		t.Fatal("email extraction must be forced on")
	}
	if req.FastMode {
		t.Fatal("fast mode must be disabled so website contact enrichment can run")
	}
}
