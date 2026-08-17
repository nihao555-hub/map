package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectoryCoverageSplitsLegalNames(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "merchants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()

	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:fund", Source: "gleif", Name: "Oakmark International Fund", Shop: "FUND", Country: "US", City: "BOSTON", Homepage: "https://search.gleif.org/#/record/fund"},
		{ExtID: "osm:shop", Source: "osm", Name: "Toko Listrik Jaya", Shop: "electrical", Country: "ID", City: "Surabaya", Homepage: "https://www.openstreetmap.org/node/1", Profiles: []Profile{
			{ExtID: "osm:shop", Platform: PlatformFacebook, URL: "https://www.facebook.com/tokolistrikjaya", Handle: "tokolistrikjaya", Source: "osm-tag"},
		}},
		{ExtID: "wd:Q1", Source: "wikidata", Name: "Mycron Steel Berhad", Shop: "company", Country: "MY", Homepage: "https://www.mycronsteel.com"},
		{ExtID: "wd:Q2", Source: "wikidata", Name: "Bosch", Shop: "company", Country: "DE", Homepage: "https://www.tiktok.com/@boschpowertools", Profiles: []Profile{
			{ExtID: "wd:Q2", Platform: PlatformTikTok, URL: "https://www.tiktok.com/@boschpowertools", Handle: "boschpowertools", Source: "wikidata-social"},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	cov, err := dir.Coverage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cov.Merchants != 4 || cov.LegalNameOnly != 1 || cov.Operating != 3 {
		t.Fatalf("coverage=%+v", cov)
	}
	if cov.WithUsefulSocial != 2 || cov.NoSocial != 2 || cov.NoSocialButHomepage != 1 {
		t.Fatalf("socials=%+v", cov)
	}
	if cov.TikTokUnique != 1 || cov.DouyinUnique != 0 {
		t.Fatalf("short-video unique=%+v", cov)
	}
	if cov.BySource["gleif"] != 1 || cov.UsefulByPlatform[PlatformFacebook] != 1 {
		t.Fatalf("maps=%+v %+v", cov.BySource, cov.UsefulByPlatform)
	}
	if cov.Note == "" || !strings.Contains(cov.Note, "GLEIF") || !strings.Contains(cov.Note, "TikTok") {
		t.Fatalf("note=%s", cov.Note)
	}
	tt, err := dir.Browse(context.Background(), DirectoryBrowseQuery{Filter: "tiktok"})
	if err != nil || tt.Total != 1 || tt.Rows[0].Name != "Bosch" {
		t.Fatalf("tiktok=%+v err=%v", tt, err)
	}

	legal, err := dir.Browse(context.Background(), DirectoryBrowseQuery{Filter: "legal_name"})
	if err != nil || legal.Total != 1 || len(legal.Rows) != 1 || legal.Rows[0].Name != "Oakmark International Fund" {
		t.Fatalf("legal=%+v err=%v", legal, err)
	}
	home, err := dir.Browse(context.Background(), DirectoryBrowseQuery{Filter: "homepage"})
	if err != nil || home.Total != 1 || home.Rows[0].Name != "Mycron Steel Berhad" {
		t.Fatalf("homepage=%+v err=%v", home, err)
	}
	useful, err := dir.Browse(context.Background(), DirectoryBrowseQuery{Filter: "with_social"})
	if err != nil || useful.Total != 2 {
		t.Fatalf("useful=%+v err=%v", useful, err)
	}
	named, err := dir.Browse(context.Background(), DirectoryBrowseQuery{Filter: "legal_name", Name: "Oakmark", Country: "US"})
	if err != nil || named.Total != 1 {
		t.Fatalf("named=%+v err=%v", named, err)
	}
}
