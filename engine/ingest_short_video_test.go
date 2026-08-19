package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShortVideoQueriesCompanyIntents(t *testing.T) {
	qs := shortVideoQueries([]string{"电动工具", "power tools"}, "")
	got := queryStrings(qs)
	for _, want := range []string{
		"site:tiktok.com/@ 电动工具",
		`site:tiktok.com/@ "power tools" (official OR shop OR store OR factory OR supplier OR manufacturer)`,
		"site:douyin.com/user 电动工具 (工厂 OR 旗舰店 OR 批发 OR 厂家 OR 贸易)",
	} {
		if !containsString(got, want) {
			t.Fatalf("missing %q in %+v", want, got)
		}
	}
	overseas := shortVideoQueries([]string{"power tools"}, "US")
	for _, q := range overseas {
		if q.platform == PlatformDouyin {
			t.Fatalf("douyin leaked into US harvest: %+v", q)
		}
	}
}

func TestKeepShortVideoBusiness(t *testing.T) {
	shop := Hit{
		Platform:    PlatformDouyin,
		Name:        "东成旗舰店",
		Handle:      "MS4wLjABAAAADongcheng",
		HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAADongcheng",
		Snippet:     "电动工具厂家批发",
	}
	if !keepShortVideoBusiness(shop, "电动工具") {
		t.Fatal("factory shop dropped")
	}
	brand := Hit{
		Platform:    PlatformTikTok,
		Name:        "Bosch Power Tools",
		Handle:      "boschpowertools",
		HomepageURL: "https://www.tiktok.com/@boschpowertools",
		Snippet:     "official power tools",
	}
	if !keepShortVideoBusiness(brand, "power tools") {
		t.Fatal("brand handle dropped")
	}
	vlog := Hit{
		Platform:    PlatformDouyin,
		Name:        "某主播",
		Handle:      "MS4wLjABAAAAvlog",
		HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAvlog",
		Snippet:     "日常分享 vlog",
	}
	if keepShortVideoBusiness(vlog, "电动工具") {
		t.Fatal("personal vlog kept")
	}
	randHandle := Hit{
		Platform:    PlatformTikTok,
		Name:        "audiencebetween",
		Handle:      "audiencebetween",
		HomepageURL: "https://www.tiktok.com/@audiencebetween",
	}
	if keepShortVideoBusiness(randHandle, "五金") {
		t.Fatal("random latin handle kept")
	}
}

func TestHarvestShortVideoInsertsHomepages(t *testing.T) {
	html := `<html><body><div id="search">
	  <div class="g"><a href="/url?q=https://www.tiktok.com/@boschpowertools&amp;sa=U">Bosch Power Tools official</a></div>
	  <div class="g"><a href="/url?q=https://www.douyin.com/user/MS4wLjABAAAAFactory&amp;sa=U">河北喜提电动工具工厂</a></div>
	  <div class="g"><a href="/url?q=https://www.tiktok.com/@dancestar&amp;sa=U">dance challenge vlog</a></div>
	</div></body></html>`
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/healthz") {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		_ = json.NewEncoder(w).Encode(sidecarResponse{Users: []sidecarUser{{
			Platform:    PlatformTikTok,
			UniqueID:    "mjdtpowertools",
			Nickname:    "MJD Tools Shop",
			Signature:   "power tools factory wholesale",
			HomepageURL: "https://www.tiktok.com/@mjdtpowertools",
			Verified:    true,
		}}, Source: "tiktok-api"})
	}))
	defer sidecar.Close()

	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), sidecar.URL) {
				return sidecar.Client().Transport.RoundTrip(req)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(html)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
		TikTokURL: sidecar.URL,
	}
	db := filepath.Join(t.TempDir(), "m.db")
	st, err := c.HarvestShortVideo(context.Background(), HarvestOptions{
		DBPath:     db,
		Keywords:   []string{"电动工具"},
		Countries:  []string{""},
		QueryLimit: 8,
		Workers:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Hits < 2 || st.Inserted < 2 {
		t.Fatalf("stats=%+v", st)
	}
	dir, err := OpenDirectory(db)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	tiktokN, err := dir.CountSource(context.Background(), "tiktok")
	if err != nil || tiktokN < 1 {
		t.Fatalf("tiktok=%d err=%v", tiktokN, err)
	}
	page, err := dir.Browse(context.Background(), DirectoryBrowseQuery{Filter: "with_social", Source: "tiktok"})
	if err != nil || page.Total < 1 {
		t.Fatalf("browse=%+v err=%v", page, err)
	}
}

func TestShortVideoExtID(t *testing.T) {
	id := shortVideoExtID(Hit{Platform: PlatformTikTok, Handle: "BoschPowerTools", HomepageURL: "https://www.tiktok.com/@boschpowertools"})
	if id != "tiktok:boschpowertools" {
		t.Fatalf("%s", id)
	}
}

func TestUniqueShortVideoTermsSEA(t *testing.T) {
	t.Parallel()
	terms := uniqueShortVideoTerms([]string{"电动工具", "toko listrik"}, []string{"ID", "TH"})
	if !containsString(terms, "panel listrik") && !containsString(terms, "perkakas listrik") && !containsString(terms, "toko listrik") {
		t.Fatalf("missing SEA local terms: %v", terms)
	}
	if !containsString(terms, "เครื่องมือไฟฟ้า") {
		t.Fatalf("missing Thai term: %v", terms)
	}
}

func TestSEACitySidecarKeywords(t *testing.T) {
	t.Parallel()
	got := seaCitySidecarKeywords()
	if len(got) < 80 {
		t.Fatalf("too few city terms: %d", len(got))
	}
	if !containsString(got, "toko listrik jakarta") || !containsString(got, "furniture shop bangkok") {
		t.Fatalf("%v", got[:8])
	}
	plan := resolveShortVideoRegions(HarvestOptions{Regions: []string{"sea"}})
	if !containsString(plan[0].Extra, "LED shop manila") {
		t.Fatal("SEA region should include city fan-out")
	}
}

func TestSidecarHarvestUsesTikTokAPIForIndonesia(t *testing.T) {
	var sawQ []string
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/healthz") {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		sawQ = append(sawQ, r.URL.Query().Get("q"))
		_ = json.NewEncoder(w).Encode(sidecarResponse{Users: []sidecarUser{{
			Platform:    PlatformTikTok,
			UniqueID:    "tokolistrikjaya",
			Nickname:    "Toko Listrik Jaya",
			Signature:   "panel listrik wholesale shop",
			HomepageURL: "https://www.tiktok.com/@tokolistrikjaya",
			Verified:    true,
		}}, Source: "tiktok-api"})
	}))
	defer sidecar.Close()

	c := &Client{
		HTTP:      sidecar.Client(),
		TikTokURL: sidecar.URL,
	}
	db := filepath.Join(t.TempDir(), "m.db")
	st, err := c.HarvestShortVideo(context.Background(), HarvestOptions{
		DBPath:   db,
		Keywords: []string{"toko listrik"},
		Regions:  []string{"sea"},
		Sidecar:  true,
		Workers:  1,
		Deadline: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Inserted < 1 || st.ByPlat[PlatformTikTok] < 1 {
		t.Fatalf("stats=%+v saw=%v", st, sawQ)
	}
	if len(sawQ) == 0 {
		t.Fatal("TikTok-Api was not called for SEA sidecar harvest")
	}
}

func TestSocialSearchTermsCoverCities(t *testing.T) {
	t.Parallel()
	got := socialSearchTerms(HarvestOptions{Regions: []string{"sea", "me"}})
	if len(got) < 200 {
		t.Fatalf("too few social terms: %d", len(got))
	}
	for _, want := range []string{"toko listrik jakarta", "furniture shop bangkok", "LED shop manila", "furniture dubai"} {
		if !containsString(got, want) {
			t.Fatalf("missing %q in %d terms", want, len(got))
		}
	}
	if looksLikeCityTerm("toko listrik jakarta") == false || looksLikeCityTerm("grosir") {
		t.Fatal("city term detect")
	}
}

func TestRelatedSeedHandles(t *testing.T) {
	t.Parallel()
	seen := map[string]Hit{
		"tiktok:tokolistrikjaya": {
			Platform:    PlatformTikTok,
			Name:        "Toko Listrik Jaya",
			Handle:      "tokolistrikjaya",
			HomepageURL: "https://www.tiktok.com/@tokolistrikjaya",
			Snippet:     "panel listrik wholesale shop",
			Verified:    true,
		},
		"tiktok:random": {
			Platform:    PlatformTikTok,
			Name:        "audiencebetween",
			Handle:      "audiencebetween",
			HomepageURL: "https://www.tiktok.com/@audiencebetween",
		},
	}
	got := relatedSeedHandles(seen, 10)
	if !containsString(got, "tokolistrikjaya") || containsString(got, "audiencebetween") {
		t.Fatalf("%v", got)
	}
}

func TestSocialSearchCallsTagAndRelated(t *testing.T) {
	var paths []string
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/healthz") {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		paths = append(paths, r.URL.Path)
		_ = json.NewEncoder(w).Encode(sidecarResponse{Users: []sidecarUser{{
			Platform:    PlatformTikTok,
			UniqueID:    "tokolistrikjaya",
			Nickname:    "Toko Listrik Jaya",
			Signature:   "panel listrik wholesale shop",
			HomepageURL: "https://www.tiktok.com/@tokolistrikjaya",
			Verified:    true,
		}}, Source: "tiktok-api"})
	}))
	defer sidecar.Close()

	c := &Client{HTTP: sidecar.Client(), TikTokURL: sidecar.URL}
	db := filepath.Join(t.TempDir(), "m.db")
	st, err := c.HarvestShortVideo(context.Background(), HarvestOptions{
		DBPath:       db,
		Keywords:     []string{"grosir"},
		Regions:      []string{"me"},
		SocialSearch: true,
		Workers:      1,
		Deadline:     time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.Inserted < 1 {
		t.Fatalf("stats=%+v paths=%v", st, paths)
	}
	var sawUsers, sawTag, sawRelated bool
	for _, p := range paths {
		switch p {
		case "/search/users":
			sawUsers = true
		case "/search/tag":
			sawTag = true
		case "/related":
			sawRelated = true
		}
	}
	if !sawUsers || !sawTag || !sawRelated {
		t.Fatalf("paths=%v", paths)
	}
}

func TestShortVideoRegionOrder(t *testing.T) {
	t.Parallel()
	plan := resolveShortVideoRegions(HarvestOptions{Fast: true})
	if len(plan) != 3 {
		t.Fatalf("regions=%d", len(plan))
	}
	if plan[0].Name != "sea" || plan[1].Name != "me" || plan[2].Name != "west" {
		t.Fatalf("order=%s %s %s", plan[0].Name, plan[1].Name, plan[2].Name)
	}
	if !containsString(plan[0].Extra, "panel listrik") {
		t.Fatal("SEA should include Bahasa electrical keyword")
	}
	if !containsString(plan[1].Extra, "أدوات كهربائية") {
		t.Fatal("ME should include Arabic electrical keyword")
	}
	subset := resolveShortVideoRegions(HarvestOptions{Regions: []string{"me", "sea"}})
	if len(subset) != 2 || subset[0].Name != "sea" || subset[1].Name != "me" {
		t.Fatalf("subset keeps default order, got %+v", subset)
	}
}
