package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseEmageExhibitorTable(t *testing.T) {
	html := `<html><body><table>
	<tr><td>公司名</td><td>展位号</td><td>所在国家/地区</td></tr>
	<tr><td>2-Connect ApS</td><td>E1D26</td><td>丹麦</td></tr>
	<tr><td>浙江艾美家居有限公司</td><td>1.1A01</td><td>中国</td></tr>
	<tr><td>公司名</td><td>展位号</td><td>地区</td></tr>
	</table></body></html>`
	hits := parseExhibitorDocument([]byte(html), "https://exhibitors.emagecompany.com/wood/furniture-china30.html", "家具中国")
	if len(hits) != 2 {
		t.Fatalf("hits=%+v", hits)
	}
	if hits[0].Name != "2-Connect ApS" || hits[0].Extra["booth"] != "E1D26" {
		t.Fatalf("first %+v", hits[0])
	}
	if hits[1].Name != "浙江艾美家居有限公司" || hits[1].Role != RoleSeller {
		t.Fatalf("second %+v", hits[1])
	}
}

func TestParseBrixExhibitorJSON(t *testing.T) {
	raw := []byte(`{"exhibitors":[
		{"title":"AB Skinnwille","stand":"B04:26","url":"https://mobelmassan.com/exhibitors/ab-skinnwille/"},
		{"title":"Aksaga","stand":"B00:04","url":"/exhibitors/aksaga/"}
	]}`)
	hits := parseExhibitorJSON(raw, "https://mobelmassan.com/exhibitors/", "Möbelmässan")
	if len(hits) != 2 || hits[0].Name != "AB Skinnwille" || hits[0].Extra["booth"] != "B04:26" {
		t.Fatalf("%+v", hits)
	}
	if !strings.Contains(hits[1].HomepageURL, "mobelmassan.com/exhibitors/aksaga") {
		t.Fatalf("home=%s", hits[1].HomepageURL)
	}
}

func TestSearchExhibitorsFromEmageIndex(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/wood/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><a href="/wood/ciff-gz2026.html">2026中国（广州）国际家具博览会参展商名单</a>
		<a href="/wood/motor.html">2026电机展参展商名单</a></html>`))
	})
	mux.HandleFunc("/wood/ciff-gz2026.html", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<table>
		<tr><td>展商公司名称</td><td>展区</td><td>展位号</td></tr>
		<tr><td>浙江艾美家居有限公司</td><td>民用家具</td><td>1.1A01</td></tr>
		<tr><td>Bellus Furniture Oü</td><td>民用家具</td><td>B02:22</td></tr>
		</table>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), DisablePublic: false}
	pages := c.emageListsFromIndex(context.Background(), srv.URL+"/wood/", "furniture", "furniture")
	if len(pages) != 1 || !strings.Contains(pages[0].URL, "ciff-gz2026") {
		t.Fatalf("pages=%+v", pages)
	}
	hits, src := c.fetchExhibitorList(context.Background(), pages[0].URL, pages[0].Fair, 20)
	if len(hits) != 2 || hits[0].Name != "浙江艾美家居有限公司" {
		t.Fatalf("src=%s hits=%+v", src, hits)
	}
	if hits[0].Extra["booth"] != "1.1A01" {
		t.Fatalf("booth %+v", hits[0].Extra)
	}
}

func TestLookupFairExhibitorsRequiresName(t *testing.T) {
	_, err := (&Client{}).LookupFairExhibitors(context.Background(), "", "", 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestKeepExhibitorName(t *testing.T) {
	if keepExhibitorName("公司名") || keepExhibitorName("A") || keepExhibitorName("行 业 名 录") || !keepExhibitorName("2-Connect ApS") {
		t.Fatal("name filter")
	}
}

func TestExhibitorJSONEndpointFromBrixHTML(t *testing.T) {
	html := []byte(`<div data-exhibitors-endpoint="https://objects.example.com/exhibitormodel/"></div>`)
	got := exhibitorJSONEndpoint(html, "https://mobelmassan.com/en/exhibitors/")
	if got != "https://objects.example.com/exhibitormodel/all.json" {
		t.Fatalf("got %s", got)
	}
}

func TestSearchExhibitionExhibitorsUsesLists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "wood") && strings.HasSuffix(r.URL.Path, "/") {
			_, _ = w.Write([]byte(`<a href="https://example.invalid/wood/ciff.html">2026广州国际家具博览会参展商名单</a>`))
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "furniture", Kind: KindExhibition, Role: RoleSeller})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("DisablePublic should skip live lists, got %+v", res.Hits)
	}
}

func TestParseExhibitorJSONEmpty(t *testing.T) {
	if hits := parseExhibitorJSON([]byte(`{"exhibitors":[]}`), "https://x/", "x"); len(hits) != 0 {
		t.Fatalf("%+v", hits)
	}
}
