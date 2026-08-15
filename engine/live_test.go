//go:build liveengine

package engine

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLivePublicSearchDouyinAndTikTok(t *testing.T) {
	c := OptionsFromEnv()
	c.TikTokURL = ""
	c.F2URL = ""

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	dy, err := c.Search(ctx, Query{Keyword: "电动工具", Kind: KindPeople, Platforms: []string{PlatformDouyin}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("douyin hits=%d sources=%v warnings=%v", len(dy.Hits), dy.Sources, dy.Warnings)
	for i, h := range dy.Hits {
		if i >= 5 {
			break
		}

		t.Logf("  dy %s %s %s", h.Name, h.Handle, h.HomepageURL)
	}

	tk, err := c.Search(ctx, Query{Keyword: "power tools", Kind: KindPeople, Platforms: []string{PlatformTikTok}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("tiktok hits=%d sources=%v warnings=%v", len(tk.Hits), tk.Sources, tk.Warnings)
	for i, h := range tk.Hits {
		if i >= 5 {
			break
		}

		t.Logf("  tk %s @%s %s", h.Name, h.Handle, h.HomepageURL)
	}

	ov, err := c.Search(ctx, Query{
		Keyword:   "power tools",
		Kind:      KindPeople,
		Platforms: []string{PlatformInstagram, PlatformYouTube, PlatformFacebook, PlatformLinkedIn, PlatformX},
		Limit:     15,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("overseas hits=%d sources=%v warnings=%v", len(ov.Hits), ov.Sources, ov.Warnings)
	seen := map[string]int{}
	for _, h := range ov.Hits {
		seen[h.Platform]++
		t.Logf("  %s %s %s", h.Platform, h.Name, h.HomepageURL)
	}

	if len(dy.Hits)+len(tk.Hits)+len(ov.Hits) == 0 {
		t.Fatal("live public search returned no profiles for 电动工具 or power tools")
	}

	for _, h := range append(append(dy.Hits, tk.Hits...), ov.Hits...) {
		if h.HomepageURL == "" || !strings.Contains(h.MessageHint, "不会代发") {
			t.Fatalf("incomplete hit %+v", h)
		}
	}
}

func TestLiveBuyerLEDMalaysia(t *testing.T) {
	c := OptionsFromEnv()
	c.TikTokURL = ""
	c.F2URL = ""
	c.SkipExpand = true

	search := func(name, keyword, role, country string) Result {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		res, err := c.Search(ctx, Query{Keyword: keyword, Kind: KindPeople, Role: role, Country: country})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res
	}

	logHits := func(name string, res Result) {
		buyer, withCountry := 0, 0
		for i, h := range res.Hits {
			if h.Role == RoleBuyer {
				buyer++
			}
			if h.CountryLabel != "" {
				withCountry++
			}
			if i < 20 {
				t.Logf("%s %d [%s/%s] %s | %s | %s", name, i+1, h.CountryLabel, h.Role, h.Platform, h.Name, strings.TrimSpace(h.Snippet))
			}
		}
		t.Logf("%s hits=%d buyer=%d with_country=%d/%d took=%dms", name, len(res.Hits), buyer, withCountry, len(res.Hits), res.TookMS)
	}

	unlimited := search("buyer-all", "LED灯", RoleBuyer, "")
	logHits("buyer-all", unlimited)
	my := search("buyer-my", "LED灯", RoleBuyer, "MY")
	logHits("buyer-my", my)
	en := search("buyer-en-my", "LED light", RoleBuyer, "MY")
	logHits("buyer-en-my", en)
	seller := search("seller-all", "LED灯", RoleSeller, "")
	logHits("seller-all", seller)

	if len(unlimited.Hits)+len(my.Hits)+len(en.Hits)+len(seller.Hits) == 0 {
		t.Fatal("no public homepages for LED")
	}
}

func TestLiveCustomsShoesAndCompany(t *testing.T) {
	c := OptionsFromEnv()
	c.TikTokURL = ""
	c.F2URL = ""

	logCustoms := func(name string, res Result) {
		t.Helper()
		t.Logf("%s hits=%d sources=%v took=%dms note=%s", name, len(res.Hits), res.Sources, res.TookMS, res.Note)
		for i, h := range res.Hits {
			if i >= 12 {
				break
			}
			t.Logf("  %s %s [%s] shipments=%s matching=%s product=%s via=%s %s",
				h.Role, h.Name, h.CountryLabel, h.Extra["shipments"], h.Extra["matching"], h.Extra["product"], h.Extra["via"], h.Snippet)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	company, err := c.Search(ctx, Query{Keyword: "FOOT LOCKER INC", Kind: KindCustoms, Role: RoleBuyer, Year: 2025, Limit: 10})
	cancel()
	if err != nil {
		t.Fatalf("company: %v", err)
	}
	logCustoms("company-footlocker", company)
	if len(company.Hits) == 0 {
		t.Fatal("公司名 FOOT LOCKER INC 应能从 Kirchner 公开档案拉到进口商行")
	}
	if jsonInt(company.Hits[0].Extra["shipments"]) == 0 {
		t.Fatalf("foot locker should have shipments, got %+v", company.Hits[0])
	}

	ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
	shoes, err := c.Search(ctx, Query{Keyword: "shoes", Kind: KindCustoms, Role: RoleBuyer, Year: 2025, Limit: 20})
	cancel()
	if err != nil {
		t.Fatalf("shoes buyer: %v", err)
	}
	logCustoms("shoes-buyer", shoes)

	realBuyer := 0
	for _, h := range shoes.Hits {
		if h.Role != RoleSeller && jsonInt(h.Extra["shipments"]) > 0 && looksLikeCompanyName(h.Name) {
			realBuyer++
		}
		if strings.Contains(h.Name, "中国") || strings.Contains(h.Snippet, "货源国") {
			t.Fatalf("Comtrade country row leaked into company list: %+v", h)
		}
	}
	if realBuyer == 0 {
		t.Fatal("产品词 shoes 没有返回带提单数的美国进口商")
	}

	ctx, cancel = context.WithTimeout(context.Background(), 120*time.Second)
	sellers, err := c.Search(ctx, Query{Keyword: "shoes", Kind: KindCustoms, Role: RoleSeller, Limit: 20})
	cancel()
	if err != nil {
		t.Fatalf("shoes seller: %v", err)
	}
	logCustoms("shoes-seller", sellers)
	if len(sellers.Hits) == 0 {
		t.Fatal("搜供应商应能从进口商档案抽出海外发货人")
	}
}

func TestLiveExhibitionFurniture(t *testing.T) {
	c := OptionsFromEnv()
	c.TikTokURL = ""
	c.F2URL = ""

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := c.Search(ctx, Query{Keyword: "furniture", Kind: KindExhibition, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("exhibition hits=%d sources=%v took=%dms note=%s", len(res.Hits), res.Sources, res.TookMS, res.Note)
	named := 0
	for i, h := range res.Hits {
		if i < 12 {
			t.Logf("  %s [%s] %s via=%s %s", h.Name, h.CountryLabel, h.HomepageURL, h.Extra["via"], h.Snippet)
		}
		blob := strings.ToLower(h.Name + " " + h.Snippet + " " + h.HomepageURL)
		if strings.Contains(blob, "fair") || strings.Contains(blob, "expo") || strings.Contains(blob, "messe") || strings.Contains(blob, "salon") {
			named++
		}
	}
	if named == 0 {
		t.Fatal("furniture 应能从 AUMA/Wikidata/公开网页找到具名展会")
	}
}
