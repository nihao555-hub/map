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
