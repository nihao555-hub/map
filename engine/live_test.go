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
