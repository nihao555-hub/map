package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorldBankTradeNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/country/cn/indicator/TX.VAL.MRCH.CD.WT") {
			t.Errorf("path=%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"page":1},[{"country":{"id":"CN","value":"China"},"date":"2023","value":3379044000000}]]`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), WorldBankURL: srv.URL}
	note := c.worldBankTradeNote(context.Background(), "CN", RoleSeller)
	if !strings.Contains(note, "世界银行") || !strings.Contains(note, "2023") || !strings.Contains(note, "$") {
		t.Fatalf("note=%s", note)
	}
}

func TestSearchCustomsIncludesWorldBankNote(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/lead-finder", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"year": 2024, "importers": []any{}})
	})
	mux.HandleFunc("/country/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"page":1},[{"country":{"id":"US","value":"United States"},"date":"2023","value":3100000000000}]]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), CustomsBaseURL: srv.URL, WorldBankURL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "shoes", Kind: KindCustoms, Role: RoleBuyer, Year: 2024})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Note, "世界银行") {
		t.Fatalf("note=%s", res.Note)
	}
	if !strings.Contains(strings.Join(res.Sources, ","), "worldbank") {
		t.Fatalf("sources=%v", res.Sources)
	}
}
