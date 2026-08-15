package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchExhibitionFromWikidata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "" {
			t.Errorf("missing SPARQL")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": map[string]any{
				"bindings": []map[string]any{
					{
						"itemLabel":    map[string]string{"value": "Canton Fair"},
						"countryLabel": map[string]string{"value": "China"},
						"cityLabel":    map[string]string{"value": "Guangzhou"},
						"start":        map[string]string{"value": "2026-04-15T00:00:00Z"},
						"website":      map[string]string{"value": "https://www.cantonfair.org.cn/"},
						"item":         map[string]string{"value": "http://www.wikidata.org/entity/Q1033955"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), WikidataURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "furniture", Kind: KindExhibition})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 || res.Hits[0].Name != "Canton Fair" {
		t.Fatalf("hits=%+v", res.Hits)
	}
	if res.Hits[0].Country != "CN" || res.Hits[0].Extra["city"] != "Guangzhou" {
		t.Fatalf("hit %+v", res.Hits[0])
	}
	if !strings.Contains(res.Hits[0].HomepageURL, "cantonfair") {
		t.Fatalf("home=%s", res.Hits[0].HomepageURL)
	}
}

func TestExtractOrganicFairResults(t *testing.T) {
	html := `<html><body>
	<li class="b_algo"><h2><a href="https://www.auma.de/en/furniture-fair">Cologne Furniture Fair</a></h2><p>Trade fair in Germany</p></li>
	<a href="https://www.bing.com/search">skip</a>
	</body></html>`
	hits := extractOrganicResults([]byte(html), "bing")
	if len(hits) == 0 {
		t.Fatal("no organic hits")
	}
	kept := []Hit{}
	for _, h := range hits {
		h.Kind = KindExhibition
		if keepExhibitionHit(h, false) {
			kept = append(kept, h)
		}
	}
	if len(kept) == 0 || !strings.Contains(kept[0].Name, "Furniture") {
		t.Fatalf("kept=%+v all=%+v", kept, hits)
	}
}

func TestExtractOrganicResultsIgnoresLooseAnchors(t *testing.T) {
	html := `<html><body>
	<a href="https://www.kraken.com/features/margin-trading">Kraken Margin Trading</a>
	<li class="b_algo"><h2><a href="https://www.auma.de/en/furniture-fair">Cologne Furniture Fair</a></h2></li>
	</body></html>`
	hits := extractOrganicResults([]byte(html), "bing")
	for _, h := range hits {
		if strings.Contains(strings.ToLower(h.HomepageURL), "kraken") {
			t.Fatalf("loose anchor leaked: %+v", hits)
		}
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Name, "Furniture") {
		t.Fatalf("hits=%+v", hits)
	}
}

func TestKeepExhibitionHitDropsExhibitorNoise(t *testing.T) {
	junk := Hit{Name: "Kraken Margin Trading", HomepageURL: "https://www.kraken.com/features/margin-trading", Snippet: "trade"}
	if keepExhibitionHit(junk, true) || keepExhibitionHit(junk, false) {
		t.Fatal("kraken should be dropped")
	}
	fair := Hit{Name: "Cologne Furniture Fair exhibitors", HomepageURL: "https://www.auma.de/en/exhibitors", Snippet: "exhibitor list"}
	if !keepExhibitionHit(fair, true) {
		t.Fatal("named exhibitor page should stay")
	}
	dir := Hit{Name: "Furniture exhibitions", HomepageURL: "https://10times.com/furniture", Snippet: "trade fair calendar"}
	if keepExhibitionHit(dir, false) {
		t.Fatal("directory calendar should be dropped")
	}
	cal := Hit{Name: "Furniture Exhibitions Calendar 2026 - 2027", HomepageURL: "https://expoassist.net/en/furniture", Snippet: "trade fair"}
	if keepExhibitionHit(cal, false) {
		t.Fatal("calendar page should be dropped")
	}
}

func TestWikidataFairLabelSPARQLContainsProduct(t *testing.T) {
	q := wikidataFairLabelSPARQL("furniture", "furniture", "", 8)
	if !strings.Contains(q, "CONTAINS") || !strings.Contains(q, "furniture") {
		t.Fatalf("sparql=%s", q)
	}
}

func TestSearchExhibitionFromOpenDataset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "Canton Fair", "website": "https://www.cantonfair.org.cn/", "city": "Guangzhou", "country": "China", "start_date": "2026-04-15", "end_date": "2026-05-05", "industry": "General Merchandise"},
			{"name": "Maison&Objet", "website": "https://www.maison-objet.com/", "city": "Paris", "country": "France", "start_date": "2026-01-15", "industry": "Consumer Goods"},
			{"name": "CES", "website": "https://www.ces.tech/", "city": "Las Vegas", "country": "United States", "start_date": "2026-01-06", "industry": "Technology & Electronics"},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), FairCalendarURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "furniture", Kind: KindExhibition})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) < 2 {
		t.Fatalf("hits=%+v", res.Hits)
	}
	foundCanton := false
	foundMaison := false
	for _, h := range res.Hits {
		if strings.Contains(h.Name, "Canton") && h.Extra["city"] == "Guangzhou" && h.Extra["start"] != "" {
			foundCanton = true
		}
		if strings.Contains(h.Name, "Maison") {
			foundMaison = true
		}
	}
	if !foundCanton || !foundMaison {
		t.Fatalf("canton/maison missing: %+v", res.Hits)
	}
}

func TestSearchExhibitionSkipsWhenUnconfigured(t *testing.T) {
	c := &Client{DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "CES", Kind: KindExhibition})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("hits=%+v", res.Hits)
	}
}
