package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseEventsEyeSearchResults(t *testing.T) {
	html := `<html><body><table class="results">
	<tr>
	  <td><a href="/fairs/f-january-furniture-show-26758-1.html"><b>JANUARY FURNITURE SHOW</b><i>UK furniture event</i></a></td>
	  <td>Birmingham (UK - United Kingdom)</td>
	</tr>
	<tr>
	  <td><a href="/fairs/f-maison-objet-1751-1.html"><b>MAISON &amp; OBJET</b><i>Decoration and furniture</i></a></td>
	  <td>Paris (France)</td>
	</tr>
	<tr>
	  <td><a href="/fairs/trade-shows-by-theme.html">skip directory</a></td>
	  <td></td>
	</tr>
	</table></body></html>`
	hits := parseEventsEyeFairs([]byte(html), "https://www.eventseye.com")
	if len(hits) != 2 {
		t.Fatalf("hits=%+v", hits)
	}
	if hits[0].Name != "JANUARY FURNITURE SHOW" || hits[0].Extra["city"] != "Birmingham" || hits[0].Country != "GB" {
		t.Fatalf("first=%+v extra=%v", hits[0], hits[0].Extra)
	}
	if !strings.Contains(hits[0].HomepageURL, "/fairs/f-january-furniture-show-26758-1.html") {
		t.Fatalf("home=%s", hits[0].HomepageURL)
	}
	if hits[1].Extra["city"] != "Paris" || hits[1].Country != "FR" {
		t.Fatalf("second=%+v", hits[1])
	}
}

func TestParseEventsEyeThemeDates(t *testing.T) {
	html := `<html><body><table class="tradeshows">
	<tr>
	  <td><a href="f-aiff-australian-international-furniture-fair-6700-1.html"><b>AIFF</b><i>Furniture fair</i></a></td>
	  <td>once a year</td>
	  <td><a href="cy1_trade-shows-melbourne.html">Melbourne (Australia)</a></td>
	  <td>03/15/2027<br><i>4 days</i></td>
	</tr>
	</table></body></html>`
	hits := parseEventsEyeFairs([]byte(html), defaultEventsEyeURL)
	if len(hits) != 1 {
		t.Fatalf("hits=%+v", hits)
	}
	if hits[0].Name != "AIFF" || hits[0].Extra["city"] != "Melbourne" || hits[0].Country != "AU" {
		t.Fatalf("hit=%+v extra=%v", hits[0], hits[0].Extra)
	}
	if hits[0].Extra["start"] != "2027-03-15" {
		t.Fatalf("start=%s", hits[0].Extra["start"])
	}
}

func TestEventsEyeThemeSlugsAndSearchURL(t *testing.T) {
	slugs := eventsEyeThemeSlugs("家具", "furniture")
	if len(slugs) == 0 || slugs[0] != "decoration-furniture-lighting" {
		t.Fatalf("slugs=%v", slugs)
	}
	u := eventsEyeSearchURL(defaultEventsEyeURL, "Canton Fair")
	if !strings.Contains(u, "tsearch.pl") || !strings.Contains(u, "Canton") {
		t.Fatalf("url=%s", u)
	}
	if !looksLikeNamedFair("Canton Fair") || looksLikeNamedFair("furniture") {
		t.Fatal("named-fair heuristic")
	}
}

func TestSearchExhibitionFromEventsEye(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "decoration-furniture-lighting") {
			t.Errorf("path=%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`<html><body><table class="tradeshows">
		<tr>
		  <td><a href="f-ciff-123-1.html"><b>CIFF</b><i>China International Furniture Fair</i></a></td>
		  <td>twice a year</td>
		  <td>Guangzhou (China)</td>
		  <td>03/18/2027</td>
		</tr>
		</table></body></html>`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), EventsEyeURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "furniture", Kind: KindExhibition})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Name != "CIFF" {
		t.Fatalf("hits=%+v", res.Hits)
	}
	if res.Hits[0].Country != "CN" || res.Hits[0].Extra["start"] != "2027-03-18" {
		t.Fatalf("hit=%+v extra=%v", res.Hits[0], res.Hits[0].Extra)
	}
	if !strings.Contains(strings.Join(res.Sources, ","), "eventseye") {
		t.Fatalf("sources=%v", res.Sources)
	}
}

func TestWikidataFairSPARQLUsesMoreClasses(t *testing.T) {
	q := wikidataFairEntitySPARQL("furniture", "furniture", "", 80)
	if !strings.Contains(q, "wd:Q2856432") || !strings.Contains(q, "wd:Q625994") {
		t.Fatalf("sparql=%s", q)
	}
	if !strings.Contains(q, "LIMIT 50") {
		t.Fatalf("limit not capped: %s", q)
	}
}
