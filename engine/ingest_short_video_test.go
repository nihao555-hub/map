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
