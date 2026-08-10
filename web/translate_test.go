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
		// B2B：海外绝不以中文进 Maps；有当地词时优先当地（印尼实测 panel listrik ≫ switchgear）
		{"采购商", "en", "importer"},
		{"采购商", "id", "importir"},
		{"批发商", "en", "wholesaler"},
		{"批发商", "id", "grosir"},
		{"进口商", "id", "importir"},
		{"配电柜", "id", "panel listrik"},
		{"配电柜", "en", "switchgear"},
		{"贸易公司", "en", "trading company"},
		{"贸易公司", "id", "perusahaan dagang"},
	}

	for _, tt := range tests {
		got, ok := translateBusinessTerm(tt.term, tt.lang)
		if !ok || got != tt.want {
			t.Fatalf("translateBusinessTerm(%q,%q)=(%q,%v) want %q", tt.term, tt.lang, got, ok, tt.want)
		}
	}
}

func TestLocalizeSearchQueryBuyerNeverShipsChinese(t *testing.T) {
	// 模拟机翻全挂：词典仍须把「采购商」落到英文，绝不能出现汉字
	orig := myMemoryTranslateURL
	myMemoryTranslateURL = "http://127.0.0.1:1" // 强制失败
	defer func() { myMemoryTranslateURL = orig }()

	kws, _, did := localizeSearchQuery(context.Background(), []string{"采购商"}, "Bojongsari Baru, Bojongsari", "id")
	if !did {
		t.Fatal("expected translation via lexicon")
	}
	if len(kws) != 1 {
		t.Fatalf("keywords=%v", kws)
	}
	if containsChinese(kws[0]) {
		t.Fatalf("must not ship chinese to maps: %q", kws[0])
	}
	if kws[0] != "importir in Bojongsari Baru, Bojongsari" {
		t.Fatalf("keywords=%v", kws)
	}
}

func TestLocalizeSearchQueryLexiconPreferredOverAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"buyer company"}}]}`)
	}))
	defer srv.Close()

	t.Setenv("GRSAI_API_KEY", "test-key")
	t.Setenv("GRSAI_API_HOST", srv.URL)
	t.Setenv("GRSAI_MODEL", "gemini-3.1-flash-lite")

	kws, _, did := localizeSearchQuery(context.Background(), []string{"采购商"}, "Jakarta", "id", localizeOpts{
		CountryName: "Indonesia",
		UseAI:       true,
	})
	if !did {
		t.Fatal("expected translation")
	}
	// 词典优先：印尼用当地词 importir
	if len(kws) != 1 || kws[0] != "importir in Jakarta" {
		t.Fatalf("lexicon should win; got %v", kws)
	}
}

func TestLocalizeSearchQueryAIWhenLexiconMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"custom niche shop"}}]}`)
	}))
	defer srv.Close()

	t.Setenv("GRSAI_API_KEY", "test-key")
	t.Setenv("GRSAI_API_HOST", srv.URL)
	t.Setenv("GRSAI_MODEL", "gemini-3.1-flash-lite")

	kws, _, did := localizeSearchQuery(context.Background(), []string{"冷门细分词xyz"}, "Jakarta", "id", localizeOpts{
		CountryName: "Indonesia",
		UseAI:       true,
	})
	if !did {
		t.Fatal("expected AI translation for lexicon miss")
	}
	if len(kws) != 1 || kws[0] != "custom niche shop in Jakarta" {
		t.Fatalf("AI should fill lexicon miss; got %v", kws)
	}
}

func TestTranslateBusinessTermFuzzyTypo(t *testing.T) {
	// 输入法丢字：啡店 ← 咖啡店
	got, ok := translateBusinessTerm("啡店", "en")
	if !ok || got != "coffee shop" {
		t.Fatalf("fuzzy 啡店 -> got (%q,%v)", got, ok)
	}
	// 印尼优先当地 Maps 词
	got, ok = translateBusinessTerm("啡店", "id")
	if !ok || got != "kedai kopi" {
		t.Fatalf("fuzzy 啡店 id -> got (%q,%v)", got, ok)
	}
	got, ok = translateBusinessTerm("咖啡店", "id")
	if !ok || got != "kedai kopi" {
		t.Fatalf("咖啡店 id local -> got (%q,%v)", got, ok)
	}
}

func TestFuzzyPlaceLexicon(t *testing.T) {
	got, ok := fuzzyPlaceLexicon("约曼哈顿")
	if !ok || got != "Manhattan, New York" {
		t.Fatalf("got (%q,%v)", got, ok)
	}
	got, ok = fuzzyPlaceLexicon("雅加达")
	if !ok || got != "Jakarta" {
		t.Fatalf("jakarta got (%q,%v)", got, ok)
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

func TestMapsPlaceHintSkipsCoordinates(t *testing.T) {
	if !isLatLonLocation("22.582632, 114.061775") {
		t.Fatal("expected coords")
	}
	if got := mapsPlaceHint("22.582632, 114.061775", "Indonesia"); got != "Indonesia" {
		t.Fatalf("got %q", got)
	}
	if got := mapsPlaceHint("Jakarta", "Indonesia"); got != "Jakarta" {
		t.Fatalf("got %q", got)
	}
}
