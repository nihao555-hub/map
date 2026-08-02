//nolint:testpackage // shares the internal web test package with web_test.go
package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContainsChinese(t *testing.T) {
	if !containsChinese("咖啡店") {
		t.Fatal("expected chinese")
	}
	if containsChinese("coffee shop") {
		t.Fatal("did not expect chinese")
	}
}

func TestTranslateBusinessTerm(t *testing.T) {
	tests := []struct {
		term, lang, want string
	}{
		{"咖啡", "en", "coffee"},
		{"咖啡店", "en", "coffee shop"},
		{"拉面", "ja", "ラーメン"},
		{"美甲店", "en", "nail salon"},
		{"牙医", "en", "dentist"},
		{"书店", "en", "bookstore"},
		// 后缀回退：词典只有「美甲」时，「美甲店」也可命中
		{"咖啡馆", "en", "cafe"},
	}

	for _, tt := range tests {
		got, ok := translateBusinessTerm(tt.term, tt.lang)
		if !ok || got != tt.want {
			t.Fatalf("translateBusinessTerm(%q,%q)=(%q,%v) want %q", tt.term, tt.lang, got, ok, tt.want)
		}
	}
}

func TestLocalizeSearchQueryUsesLexicon(t *testing.T) {
	kws, loc, did := localizeSearchQuery(context.Background(), []string{"咖啡店"}, "纽约曼哈顿", "en")
	if !did {
		t.Fatal("expected translation")
	}
	if loc != "Manhattan, New York" {
		t.Fatalf("loc=%q", loc)
	}
	if len(kws) != 1 || kws[0] != "coffee shop in Manhattan, New York" {
		t.Fatalf("keywords=%v", kws)
	}
}

func TestLocalizeSearchQueryJapanese(t *testing.T) {
	kws, loc, did := localizeSearchQuery(context.Background(), []string{"拉面"}, "东京涩谷", "ja")
	if !did {
		t.Fatal("expected translation")
	}
	if loc != "Shibuya, Tokyo" {
		t.Fatalf("loc=%q", loc)
	}
	if len(kws) != 1 || kws[0] != "ラーメン in Shibuya, Tokyo" {
		t.Fatalf("keywords=%v", kws)
	}
}

func TestLocalizeSearchQueryKeepsChineseForChina(t *testing.T) {
	kws, _, did := localizeSearchQuery(context.Background(), []string{"咖啡馆"}, "深圳", "zh")
	if did {
		t.Fatal("china zh should not translate")
	}
	if len(kws) != 1 || kws[0] != "咖啡馆 in 深圳" {
		t.Fatalf("keywords=%v", kws)
	}
}

func TestTranslateTextMyMemory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"responseData":{"translatedText":"bookstore"}}`)
	}))
	defer srv.Close()

	orig := myMemoryTranslateURL
	myMemoryTranslateURL = srv.URL
	defer func() { myMemoryTranslateURL = orig }()

	got, err := translateText(context.Background(), "书店", "zh", "en")
	if err != nil {
		t.Fatalf("translateText: %v", err)
	}
	if got != "bookstore" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeMT(t *testing.T) {
	if got := normalizeMT("NailSalon"); got != "Nail Salon" {
		t.Fatalf("got %q", got)
	}
}

func TestShortDisplayName(t *testing.T) {
	if got := shortDisplayName("New York, United States"); got != "New York, United States" {
		t.Fatalf("got %q", got)
	}
	if got := shortDisplayName("Shibuya, Tamagawa Street, Tokyo"); got != "Shibuya, Tamagawa Street" {
		t.Fatalf("got %q", got)
	}
}
