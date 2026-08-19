package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfilesFromOfficialHTMLKeepsSocialHomepages(t *testing.T) {
	html := []byte(`<html><body>
	  <a href="https://www.facebook.com/signify">Facebook</a>
	  <a href="https://www.instagram.com/signify">Instagram</a>
	  <a href="https://www.linkedin.com/company/signify">LinkedIn</a>
	  <a href="https://www.youtube.com/watch?v=dQw4w9wg">skip video</a>
	</body></html>`)
	got := profilesFromOfficialHTML("gleif:abc", "https://www.signify.com", html)
	by := map[string]Profile{}
	for _, p := range got {
		by[p.Platform] = p
	}
	if by[PlatformWebsite].URL != "https://www.signify.com" {
		t.Fatalf("website=%+v", by[PlatformWebsite])
	}
	if by[PlatformFacebook].Handle == "" || by[PlatformInstagram].URL == "" || by[PlatformLinkedIn].URL == "" {
		t.Fatalf("%+v", got)
	}
	if _, ok := by[PlatformYouTube]; ok {
		t.Fatalf("video leaked: %+v", got)
	}
}

func TestListHomepagesMissingSocialsPrefersShops(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "osm:1", Source: "osm", Name: "Shop", Country: "DE", Homepage: "https://shop.example"},
		{ExtID: "gleif:bare", Source: "gleif", Name: "Bare Co", Country: "NL", Homepage: "https://bare.example"},
		{ExtID: "gleif:has", Source: "gleif", Name: "Has Social", Country: "NL", Homepage: "https://has.example",
			Profiles: []Profile{{ExtID: "gleif:has", Platform: PlatformFacebook, URL: "https://www.facebook.com/has", Source: "wikidata-social"}}},
		{ExtID: "gleif:reg", Source: "gleif", Name: "Registry", Country: "NL", Homepage: "https://search.gleif.org/#/record/x"},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := dir.ListHomepagesMissingSocials(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].ExtID != "osm:1" {
		t.Fatalf("want shop/OSM before GLEIF legal names, got %+v", rows)
	}
	for _, row := range rows {
		if row.ExtID == "gleif:has" || row.ExtID == "gleif:reg" {
			t.Fatalf("should skip has-social / registry: %+v", rows)
		}
	}
}

func TestAttachOfficialHTMLSocialsToGLEIF(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:001", Source: "gleif", Name: "Licht Kraus", Country: "DE", Homepage: "https://licht.example"},
	}); err != nil {
		t.Fatal(err)
	}
	html := []byte(`<a href="https://www.facebook.com/lichtkraus">fb</a>`)
	profs := profilesFromOfficialHTML("gleif:001", "https://licht.example", html)
	if _, _, err := dir.attachExisting(context.Background(), []Merchant{{
		ExtID: "gleif:001", Source: "gleif", Homepage: "https://licht.example", Profiles: profs,
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := dir.ProfilesFor(context.Background(), []string{"gleif:001"})
	if err != nil {
		t.Fatal(err)
	}
	var sawFB bool
	for _, p := range got["gleif:001"] {
		if p.Platform == PlatformFacebook && strings.Contains(p.URL, "lichtkraus") {
			sawFB = true
		}
	}
	if !sawFB {
		t.Fatalf("%+v", got)
	}
}
