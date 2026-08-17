package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestYellowPageQueriesSEA(t *testing.T) {
	t.Parallel()
	got := yellowPageQueries([]string{"LED灯", "furniture"}, []string{"ID", "MY"})
	if len(got) < 6 {
		t.Fatalf("queries=%d %v", len(got), got)
	}
	var sawID, sawMY, sawEU bool
	for _, q := range got {
		if strings.Contains(q, "yellowpages.co.id") {
			sawID = true
		}
		if strings.Contains(q, "yellowpages.com.my") {
			sawMY = true
		}
		if strings.Contains(q, "europages.com") {
			sawEU = true
		}
		if strings.Contains(q, "yellowpages.co.id") && strings.Contains(q, "Malaysia") {
			t.Fatalf("ID directory queried for MY: %s", q)
		}
	}
	if !sawID || !sawMY || !sawEU {
		t.Fatalf("missing directory: %v", got)
	}
}

func TestParseYellowPageListing(t *testing.T) {
	t.Parallel()
	html := []byte(`<html><head><title>PT Lampu Jaya | Europages</title></head>
	<body>
	  <h1>PT Lampu Jaya</h1>
	  <a href="tel:+62215551234">Call</a>
	  <a class="website" href="https://www.lampujaya.co.id">Visit website</a>
	  <a href="https://www.facebook.com/lampujaya">Facebook</a>
	</body></html>`)
	got := parseYellowPageListing("https://www.europages.com/pt-lampu-jaya.html", html)
	if got.Name != "PT Lampu Jaya" || got.Source != "yellowpages" {
		t.Fatalf("%+v", got)
	}
	if got.Phone != "+62215551234" {
		t.Fatalf("phone=%q", got.Phone)
	}
	if !strings.Contains(got.Homepage, "lampujaya.co.id") {
		t.Fatalf("home=%q", got.Homepage)
	}
	if got.ExtID != "yp:lampujaya.co.id" {
		t.Fatalf("ext=%s", got.ExtID)
	}
	var sawFB bool
	for _, p := range got.Profiles {
		if p.Platform == PlatformFacebook && p.Handle == "lampujaya" {
			sawFB = true
		}
	}
	if !sawFB {
		t.Fatalf("profiles=%+v", got.Profiles)
	}
}

func TestExtractYellowPageURLs(t *testing.T) {
	t.Parallel()
	raw := []byte(`<html><a href="https://www.europages.com/acme-lighting.html">Acme</a>
	<a href="https://www.facebook.com/acme">skip</a>
	<a href="https://www.yellowpages.co.id/lampu-jaya">YP</a></html>`)
	got := extractYellowPageURLs(raw)
	if !containsString(got, "https://www.europages.com/acme-lighting.html") ||
		!containsString(got, "https://www.yellowpages.co.id/lampu-jaya") {
		t.Fatalf("%v", got)
	}
	for _, u := range got {
		if strings.Contains(u, "facebook.com") {
			t.Fatalf("social leaked: %v", got)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	t.Parallel()
	if normalizePhone("tel:+62 21 555 1234") != "+62215551234" {
		t.Fatal(normalizePhone("tel:+62 21 555 1234"))
	}
	if normalizePhone("123") != "" || normalizePhone("not-a-phone") != "" {
		t.Fatal("junk accepted")
	}
}

func TestSetPhoneAndYellowPagesOrder(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "gleif:bare", Source: "gleif", Name: "Bare Co", Country: "NL", Homepage: "https://bare.example"},
		{ExtID: "yp:shop.id", Source: "yellowpages", Name: "Toko", Country: "ID", Homepage: "https://shop.example"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := dir.SetPhone(context.Background(), "yp:shop.id", "+6221"); err != nil {
		t.Fatal(err)
	}
	rows, err := dir.ListHomepagesMissingSocials(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].ExtID != "yp:shop.id" {
		t.Fatalf("yellow pages should scrape first: %+v", rows)
	}
}

func TestYellowPageDirectURLs(t *testing.T) {
	t.Parallel()
	got := yellowPageDirectURLs([]string{"家具"}, []string{"ID", "DE"})
	var sawGS, saw11880, sawEP bool
	for _, u := range got {
		if strings.Contains(u, "gelbeseiten.de/suche/furniture") {
			sawGS = true
		}
		if strings.Contains(u, "11880.com/suche/furniture") {
			saw11880 = true
		}
		if strings.Contains(u, "europages.co.uk/companies/indonesia/furniture.html") {
			sawEP = true
		}
	}
	if !sawGS || !saw11880 || !sawEP {
		t.Fatalf("direct urls missing dirs: %v", got)
	}
}

func TestExtractYellowPageURLsResolvesListings(t *testing.T) {
	t.Parallel()
	raw := []byte(`<html>
	  <a href="/gsbiz/aaaa-bbbb-cccc">Firma</a>
	  <a href="https://www.11880.com/branchenbuch/berlin/1/acme.html">Acme</a>
	  <a href="/suche/furniture/bundesweit">skip search</a>
	  <script type="application/ld+json">{"@type":"LocalBusiness","name":"Acme","url":"https://www.11880.com/branchenbuch/koeln/2/beta.html","telephone":"+492211234567"}</script>
	</html>`)
	got := extractYellowPageURLsFrom("https://www.gelbeseiten.de/suche/furniture/bundesweit", raw)
	if !containsString(got, "https://www.gelbeseiten.de/gsbiz/aaaa-bbbb-cccc") ||
		!containsString(got, "https://www.11880.com/branchenbuch/berlin/1/acme.html") ||
		!containsString(got, "https://www.11880.com/branchenbuch/koeln/2/beta.html") {
		t.Fatalf("%v", got)
	}
	for _, u := range got {
		if strings.Contains(u, "/suche/") {
			t.Fatalf("search leaked: %v", got)
		}
	}
}

func TestParseYellowPageListingJSONLDAndEmailSite(t *testing.T) {
	t.Parallel()
	html := []byte(`<html><head><title>Furniture Möbel Trends | 11880</title></head>
	<body>
	  <h1>Furniture Möbel Trends</h1>
	  <a href="tel:+4915167062670">call</a>
	  <a href="https://furnituremoebeltrends.de">Website</a>
	  <script type="application/ld+json">{"@type":"LocalBusiness","name":"Furniture Möbel Trends","telephone":"+4915167062670","email":"info@furnituremoebeltrends.de"}</script>
	</body></html>`)
	got := parseYellowPageListing("https://www.11880.com/branchenbuch/hiddenhausen/1/furniture-moebel-trends.html", html)
	if got.Name != "Furniture Möbel Trends" || got.Phone != "+4915167062670" {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.Homepage, "furnituremoebeltrends.de") {
		t.Fatalf("home=%q", got.Homepage)
	}
}

func TestWebsiteFromEmail(t *testing.T) {
	t.Parallel()
	if websiteFromEmail("info@lampujaya.co.id") != "https://lampujaya.co.id" {
		t.Fatal(websiteFromEmail("info@lampujaya.co.id"))
	}
	if websiteFromEmail("sales@gmail.com") != "" || websiteFromEmail("x@gelbeseiten.de") != "" {
		t.Fatal("public mail accepted")
	}
}

func TestListHomepagesForYellowPageScrapeIncludesOSM(t *testing.T) {
	dir, err := OpenDirectory(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.InsertBatch(context.Background(), []Merchant{
		{ExtID: "osm:1", Source: "osm", Name: "Shop", Country: "ID", Homepage: "https://shop.example",
			Profiles: []Profile{{ExtID: "osm:1", Platform: PlatformFacebook, URL: "https://www.facebook.com/shop", Source: "osm"}}},
		{ExtID: "gleif:bare", Source: "gleif", Name: "Bare Co", Country: "NL", Homepage: "https://bare.example"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := dir.MarkEnriched(context.Background(), []string{"osm:1"}); err != nil {
		t.Fatal(err)
	}
	rows, err := dir.ListHomepagesForYellowPageScrape(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].ExtID != "osm:1" {
		t.Fatalf("OSM shop should stay on the pipeline: %+v", rows)
	}
	for _, row := range rows {
		if row.ExtID == "gleif:bare" {
			t.Fatalf("GLEIF should not join yellow-page scrape: %+v", rows)
		}
	}
}

func TestProfilesFromOfficialHTMLPlusPhone(t *testing.T) {
	t.Parallel()
	html := []byte(`<a href="https://www.instagram.com/lampujaya">ig</a> Call <a href="tel:+62215551234">p</a>`)
	profs := profilesFromOfficialHTML("yp:x", "https://lampujaya.co.id", html)
	if firstPhoneFromHTML(nil, string(html)) != "+62215551234" {
		t.Fatal(firstPhoneFromHTML(nil, string(html)))
	}
	var sawIG bool
	for _, p := range profs {
		if p.Platform == PlatformInstagram {
			sawIG = true
		}
	}
	if !sawIG {
		t.Fatalf("%+v", profs)
	}
}
