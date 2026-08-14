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

	if len(dy.Hits)+len(tk.Hits) == 0 {
		t.Fatal("live public search returned no profiles for 电动工具 or power tools")
	}

	for _, h := range append(dy.Hits, tk.Hits...) {
		if h.HomepageURL == "" || !strings.Contains(h.MessageHint, "不会代发") {
			t.Fatalf("incomplete hit %+v", h)
		}
	}
}
