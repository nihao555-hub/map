package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchPeopleFromTikTokAPISidecar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}

		if r.URL.Path != "/search/users" {
			http.NotFound(w, r)
			return
		}

		if r.URL.Query().Get("count") != "40" {
			t.Errorf("sidecar count=%s want 40", r.URL.Query().Get("count"))
		}

		_ = json.NewEncoder(w).Encode(sidecarResponse{
			Source: "tiktok-api",
			Users: []sidecarUser{{
				Platform:  "tiktok",
				UniqueID:  "powertools_id",
				Nickname:  "Power Tools ID",
				Signature: "Importer in Jakarta",
			}},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), TikTokURL: srv.URL, F2URL: "http://127.0.0.1:1", DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "power tools", Kind: KindPeople, Platforms: []string{PlatformTikTok}})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Hits) != 1 {
		t.Fatalf("hits=%+v warnings=%v", res.Hits, res.Warnings)
	}

	if res.Hits[0].Handle != "powertools_id" || res.Hits[0].Platform != PlatformTikTok {
		t.Fatalf("hit %+v", res.Hits[0])
	}

	if !strings.Contains(res.Note, "不会代发") {
		t.Fatalf("note=%s", res.Note)
	}
}

func TestSearchPeopleFromF2DouyinProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}

		_ = json.NewEncoder(w).Encode(sidecarResponse{
			Source: "f2-douyin",
			Users: []sidecarUser{{
				Platform:    "douyin",
				SecUID:      "MS4wLjABAAAAtest",
				Nickname:    "某工厂",
				Signature:   "电动工具",
				HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAtest",
			}},
		})
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), TikTokURL: "http://127.0.0.1:1", F2URL: srv.URL, DisablePublic: true}
	res, err := c.Search(context.Background(), Query{
		Keyword:   "https://www.douyin.com/user/MS4wLjABAAAAtest",
		Kind:      KindPeople,
		Platforms: []string{PlatformDouyin},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Hits) != 1 || res.Hits[0].Platform != PlatformDouyin {
		t.Fatalf("hits=%+v", res.Hits)
	}
}

func TestSearchPeoplePublicWebSearch(t *testing.T) {
	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(ddgDouyinHTML)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
		TikTokURL:  "",
		F2URL:      "",
		SkipExpand: true,
	}

	res, err := c.Search(context.Background(), Query{
		Keyword:   "电动工具",
		Kind:      KindPeople,
		Role:      RoleSeller,
		Platforms: []string{PlatformDouyin, PlatformTikTok},
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Hits) < 2 {
		t.Fatalf("hits=%+v warnings=%v", res.Hits, res.Warnings)
	}

	var sawTK, sawDY bool
	for _, h := range res.Hits {
		if h.Platform == PlatformTikTok && h.Handle == "boschpowertools" {
			sawTK = true
		}

		if h.Platform == PlatformDouyin && strings.Contains(h.HomepageURL, "douyin.com/user/") {
			sawDY = true
		}

		if h.MessageURL == "" || !strings.Contains(h.MessageHint, "不会代发") {
			t.Fatalf("incomplete hit %+v", h)
		}
	}

	if !sawTK || !sawDY {
		t.Fatalf("missing profiles tk=%v dy=%v hits=%+v", sawTK, sawDY, res.Hits)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSearchRequiresKeyword(t *testing.T) {
	if _, err := (&Client{}).Search(context.Background(), Query{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestExhibitionDoesNotSelfCrawl(t *testing.T) {
	res, err := (&Client{DisablePublic: true}).Search(context.Background(), Query{Keyword: "Canton Fair", Kind: KindExhibition})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Hits) != 0 {
		t.Fatalf("expected no hits when public indexes are off, got %+v", res.Hits)
	}
	if !strings.Contains(res.Note, "AUMA") && !strings.Contains(res.Note, "Wikidata") {
		t.Fatalf("note=%s", res.Note)
	}
}

func TestCustomsDoesNotSelfCrawl(t *testing.T) {
	res, err := (&Client{DisablePublic: true}).Search(context.Background(), Query{Keyword: "Allbirds", Kind: KindCustoms})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Hits) != 0 {
		t.Fatalf("expected no self-built hits, got %+v", res.Hits)
	}
}

func TestParseSidecarUsersError(t *testing.T) {
	_, _, err := parseSidecarUsers([]byte(`{"error":"down","users":[]}`), PlatformTikTok, "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSidecarCount(t *testing.T) {
	if sidecarCount(0) != sidecarDefaultCount || sidecarCount(-1) != sidecarDefaultCount {
		t.Fatalf("default %d %d", sidecarCount(0), sidecarCount(-1))
	}
	if sidecarCount(3) != sidecarMinCount {
		t.Fatalf("min %d", sidecarCount(3))
	}
	if sidecarCount(12) != 12 {
		t.Fatalf("pass-through %d", sidecarCount(12))
	}
	if sidecarCount(99) != sidecarMaxCount {
		t.Fatalf("max %d", sidecarCount(99))
	}
}

func TestMergeHitsUnlimitedWhenLimitZero(t *testing.T) {
	items := make([]Hit, 0, 35)
	for i := 0; i < 35; i++ {
		items = append(items, Hit{
			ID:          "id-" + strings.Repeat("a", i+1),
			Name:        "factory shop",
			Platform:    PlatformFacebook,
			HomepageURL: "https://www.facebook.com/shop" + strings.Repeat("x", i+1),
			Score:       1,
		})
	}
	out := mergeHits(items, "factory", 0, "", "")
	if len(out) != 35 {
		t.Fatalf("got %d want 35", len(out))
	}
}

func TestMergeHitsPrefersMerchantOverTutorial(t *testing.T) {
	out := mergeHits([]Hit{
		{ID: "yt1", Platform: PlatformYouTube, Name: "LED灯带安装图解", Title: "LED灯带安装图解", HomepageURL: "https://www.youtube.com/watch?v=abc1234"},
		{ID: "fb1", Platform: PlatformFacebook, Name: "全成照明 Led燈飾專賣店", Title: "全成照明 Led燈飾專賣店 | Taichung", HomepageURL: "https://www.facebook.com/led0955478666"},
		{ID: "fb2", Platform: PlatformFacebook, Name: "PlayFunDeal", HomepageURL: "https://www.facebook.com/PlayFunDeal"},
		{ID: "yt2", Platform: PlatformYouTube, Name: "老灯官方", Handle: "laodeng", HomepageURL: "https://www.youtube.com/@laodeng"},
	}, "LED灯", 0, "", "")
	if len(out) == 0 || out[0].ID != "fb1" {
		t.Fatalf("want lighting shop first, got %+v", out)
	}
	for _, h := range out {
		if h.ID == "yt1" {
			t.Fatal("video must not appear")
		}
		if h.ID == "fb2" {
			t.Fatal("unrelated page must be cleaned out")
		}
		if !isSocialHomepage(h) {
			t.Fatalf("non-homepage leaked %+v", h)
		}
	}
}

func TestMergeHitsBuyerPrefersImporter(t *testing.T) {
	out := mergeHits([]Hit{
		{ID: "factory", Platform: PlatformDouyin, Name: "LED灯厂家直销", HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAFactory", Snippet: "工厂批发"},
		{ID: "buyer", Platform: PlatformFacebook, Name: "Malaysia LED Importer", HomepageURL: "https://www.facebook.com/ledimporter", Snippet: "procurement buyer of LED lights"},
	}, "LED", 0, RoleBuyer, "")
	if len(out) != 1 || out[0].ID != "buyer" {
		t.Fatalf("want only importer, got %+v", out)
	}
	if out[0].Role != RoleBuyer || out[0].Country != "MY" {
		t.Fatalf("hit %+v", out[0])
	}
}

func TestMergeHitsBuyerDropsFlagshipAndKeepsThaiImporter(t *testing.T) {
	out := mergeHits([]Hit{
		{ID: "dy", Platform: PlatformDouyin, Name: "东成旗舰店", HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAADongcheng", Snippet: "电动工具"},
		{ID: "pet", Platform: PlatformDouyin, Name: "萌宠小店", HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAPetShop", Snippet: "宠物用品"},
		{ID: "th", Platform: PlatformFacebook, Name: "Bangkok Power Tools Importer", HomepageURL: "https://www.facebook.com/bkktools", Snippet: "importer of power tools Thailand"},
	}, "电动工具", 0, RoleBuyer, "TH")
	if len(out) != 1 || out[0].ID != "th" {
		t.Fatalf("want only Thai importer, got %+v", out)
	}
	if out[0].Role != RoleBuyer || out[0].Country != "TH" {
		t.Fatalf("hit %+v", out[0])
	}
}

func TestMergeHitsDropsCountryMismatch(t *testing.T) {
	out := mergeHits([]Hit{
		{ID: "cn", Platform: PlatformDouyin, Name: "博世中国电动工具采购", HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAcn", Snippet: "电动工具采购"},
		{ID: "th", Platform: PlatformFacebook, Name: "Thai Power Tools Importer", HomepageURL: "https://www.facebook.com/thaipower", Snippet: "importer of power tools Bangkok Thailand"},
	}, "电动工具", 0, RoleBuyer, "TH")
	if len(out) != 1 || out[0].ID != "th" {
		t.Fatalf("got %+v", out)
	}
}

func TestNormalizeRoleDefaultsBuyer(t *testing.T) {
	if NormalizeRole("") != RoleBuyer || NormalizeRole("卖家") != RoleSeller {
		t.Fatalf("buyer=%s seller=%s", NormalizeRole(""), NormalizeRole("卖家"))
	}
}

func TestBlobMatchesKeywordHandle(t *testing.T) {
	if !blobMatchesKeyword("https://www.facebook.com/PowerbiltTools official page", "power tools") {
		t.Fatal("expected PowerbiltTools to match power tools")
	}
	if blobMatchesKeyword("https://www.facebook.com/randomshop", "power tools") {
		t.Fatal("false positive")
	}
}

func TestMergeHitsKeepsProductHandle(t *testing.T) {
	out := mergeHits([]Hit{
		{ID: "fb", Platform: PlatformFacebook, Name: "Powerbilt Tools Importer", Handle: "PowerbiltTools", HomepageURL: "https://www.facebook.com/PowerbiltTools", Snippet: "importer of power tools"},
	}, "power tools", 0, RoleBuyer, "")
	if len(out) != 1 {
		t.Fatalf("dropped importer handle %+v", out)
	}
}

func TestMergeHitsDropsClickbait(t *testing.T) {
	out := mergeHits([]Hit{{
		ID:          "fb",
		Platform:    PlatformFacebook,
		Name:        "65 में ख़रीदे 150 पर बेचे 😱",
		HomepageURL: "https://www.facebook.com/sirajsaifipage",
		Snippet:     "LED light trading",
	}}, "LED灯", 0, RoleBuyer, "")
	if len(out) != 0 {
		t.Fatalf("clickbait leaked %+v", out)
	}
}

func TestMergeHitsBuyerDropsBrandWithoutCustomerSignal(t *testing.T) {
	out := mergeHits([]Hit{
		{ID: "brand", Platform: PlatformFacebook, Name: "Powerbilt Tools", Handle: "PowerbiltTools", HomepageURL: "https://www.facebook.com/PowerbiltTools", Snippet: "tools"},
		{ID: "deal", Platform: PlatformFacebook, Name: "PISARN Power Tools", HomepageURL: "https://www.facebook.com/pisarnpowertools", Snippet: "dealer and trading of power tools Thailand"},
	}, "power tools", 0, RoleBuyer, "")
	if len(out) != 1 || out[0].ID != "deal" {
		t.Fatalf("want dealer only, got %+v", out)
	}
}

func TestMergeHitsKeepsQueryStampedEmptySnippet(t *testing.T) {
	out := mergeHits([]Hit{{
		ID:          "dy",
		Platform:    PlatformDouyin,
		Name:        "深圳灯饰采购部",
		HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAledbuyer",
		Extra:       map[string]string{"q": "site:douyin.com/user LED灯"},
	}}, "LED灯", 0, RoleBuyer, "")
	if len(out) != 1 {
		t.Fatalf("query-stamped buyer dropped %+v", out)
	}
}

func TestMergeHitsBuyerKeepsCompanyDropsPersonalShop(t *testing.T) {
	out := mergeHits([]Hit{
		{ID: "buy", Platform: PlatformDouyin, Name: "林芝雄海电动工具一站式采购", HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAbuyer", Snippet: "机电物资"},
		{ID: "co", Platform: PlatformDouyin, Name: "江苏工佰汇五金工具有限公司", HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAco", Snippet: "五金工具", Extra: map[string]string{"q": "site:douyin.com/user 电动工具"}},
		{ID: "shop", Platform: PlatformDouyin, Name: "电动工具小王", HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAshop", Snippet: "电动工具"},
		{ID: "off", Platform: PlatformDouyin, Name: "盛隆绿巨人电动工具官方账号", HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAoff", Snippet: "官方旗舰"},
	}, "电动工具", 0, RoleBuyer, "")
	if len(out) != 2 {
		t.Fatalf("want buyer+company, got %+v", out)
	}
	for _, h := range out {
		if h.ID == "shop" || h.ID == "off" {
			t.Fatalf("personal/official shop leaked %+v", h)
		}
	}
}

func TestMergeHitsDropsUnrelatedQueryStamp(t *testing.T) {
	out := mergeHits([]Hit{{
		ID:          "dy",
		Platform:    PlatformDouyin,
		Name:        "宝藏老师",
		HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAteacher",
		Extra:       map[string]string{"q": "site:douyin.com/user 电动工具"},
	}}, "电动工具", 0, RoleBuyer, "")
	if len(out) != 0 {
		t.Fatalf("unrelated card leaked via query stamp %+v", out)
	}
}

func TestMergeHitsDropsGenericProductName(t *testing.T) {
	out := mergeHits([]Hit{
		{ID: "gen", Platform: PlatformYouTube, Name: "LED", Handle: "LEDs", HomepageURL: "https://www.youtube.com/@LEDs", Snippet: "YouTube About"},
		{ID: "store", Platform: PlatformFacebook, Name: "LED Lighting Store", HomepageURL: "https://www.facebook.com/ledlightingstore", Snippet: "LED lighting shop"},
	}, "LED灯", 0, RoleBuyer, "")
	if len(out) != 1 || out[0].ID != "store" {
		t.Fatalf("generic LED channel leaked %+v", out)
	}
}

func TestMergeHitsDropsUnlabeledWhenCountrySelected(t *testing.T) {
	out := mergeHits([]Hit{
		{ID: "id", Platform: PlatformFacebook, Name: "Toko Listrik Jaya", Country: "ID", HomepageURL: "https://www.facebook.com/tokolistrikjaya", Snippet: "panel listrik importir Jakarta"},
		{ID: "us", Platform: PlatformFacebook, Name: "Electrical Wholesalers", HomepageURL: "https://www.facebook.com/EWCTNewHaven", Snippet: "electrical wholesaler"},
	}, "配电柜", 0, RoleBuyer, "ID")
	if len(out) != 1 || out[0].ID != "id" {
		t.Fatalf("want Indonesian buyer only, got %+v", out)
	}
}

func TestSearchSplitsIndonesiaProductKeyword(t *testing.T) {
	_, err := (&Client{DisablePublic: true}).Search(context.Background(), Query{Keyword: "印尼", Kind: KindPeople})
	if err == nil {
		t.Fatal("country-only keyword should be rejected")
	}
}

func TestMergeHitsBuyerKeepsCategoryStore(t *testing.T) {
	out := mergeHits([]Hit{
		{ID: "store", Platform: PlatformFacebook, Name: "LED Lighting Store", HomepageURL: "https://www.facebook.com/ledlightingstore", Snippet: "LED lighting shop in KL"},
		{ID: "how", Platform: PlatformYouTube, Name: "How to install LED lights", Title: "LED tutorial", HomepageURL: "https://www.youtube.com/@ledhowto", Snippet: "how to tutorial"},
		{ID: "flag", Platform: PlatformDouyin, Name: "飞利浦LED旗舰店", HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAFlag", Snippet: "官方旗舰"},
	}, "LED灯", 0, RoleBuyer, "")
	if len(out) != 1 || out[0].ID != "store" {
		t.Fatalf("want category store only, got %+v", out)
	}
}

func TestMergeHitsBuyerStillDropsFactoryDespiteQuery(t *testing.T) {
	out := mergeHits([]Hit{{
		ID:          "dy",
		Platform:    PlatformDouyin,
		Name:        "LED灯厂家直销",
		HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAFactory",
		Snippet:     "工厂批发",
		Extra:       map[string]string{"q": "site:douyin.com/user LED灯 采购"},
	}}, "LED灯", 0, RoleBuyer, "")
	if len(out) != 0 {
		t.Fatalf("factory leaked via query stamp %+v", out)
	}
}
