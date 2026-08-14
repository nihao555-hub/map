package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	defaultLimit      = 20
	maxLimit          = 50
	messagePolicyNote = "私信只打开官方主页，由您登录后手动发送。系统不会代发或绕过平台私信接口。"
)

// Search runs customer discovery. People search uses public web indexes by default
// and overlays cloned OSS sidecars (TikTok-Api / f2) when they are healthy.
func (c *Client) Search(ctx context.Context, q Query) (Result, error) {
	start := time.Now()
	q.Keyword = strings.TrimSpace(q.Keyword)
	q.Kind = strings.ToLower(strings.TrimSpace(q.Kind))

	if q.Keyword == "" {
		return Result{}, fmt.Errorf("keyword is required")
	}

	if q.Kind == "" {
		q.Kind = KindPeople
	}

	if q.Limit <= 0 {
		q.Limit = defaultLimit
	}

	if q.Limit > maxLimit {
		q.Limit = maxLimit
	}

	var (
		res Result
		err error
	)

	switch q.Kind {
	case KindPeople:
		res, err = c.searchPeople(ctx, q)
	case KindExhibition:
		res = exhibitionUnavailable(q.Keyword)
	case KindCustoms:
		res = customsUnavailable(q.Keyword)
	default:
		return Result{}, fmt.Errorf("unknown kind %q", q.Kind)
	}

	if err != nil {
		return Result{}, err
	}

	res.Keyword = q.Keyword
	res.Kind = q.Kind
	res.TookMS = time.Since(start).Milliseconds()
	res.SearchedAt = time.Now().UTC()

	if res.Note == "" && q.Kind == KindPeople {
		res.Note = messagePolicyNote
	}

	return res, nil
}

func (c *Client) searchPeople(ctx context.Context, q Query) (Result, error) {
	wanted := wantedPeoplePlatforms(q.Platforms)

	var (
		mu       sync.Mutex
		hits     []Hit
		warnings []string
		sources  []string
	)

	add := func(items []Hit, src string, warn string, err error) {
		mu.Lock()
		defer mu.Unlock()

		if err != nil {
			warnings = append(warnings, err.Error())
		}

		if warn != "" {
			warnings = append(warnings, warn)
		}

		if src != "" && len(items) > 0 {
			sources = append(sources, src)
		}

		hits = append(hits, items...)
	}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		items, warns, srcs := c.searchPublicProfiles(gctx, q.Keyword, wanted, q.Limit)
		src := strings.Join(srcs, "+")
		warn := strings.Join(warns, "; ")
		add(items, src, warn, nil)

		return nil
	})

	if wanted[PlatformTikTok] && c != nil && c.sidecarAlive(ctx, c.TikTokURL) {
		g.Go(func() error {
			items, warn, err := c.searchTikTokAPI(gctx, q.Keyword, q.Limit)
			add(items, "tiktok-api", warn, err)

			return nil
		})
	}

	if wanted[PlatformTikTok] && c != nil && c.sidecarAlive(ctx, c.F2URL) {
		g.Go(func() error {
			items, warn, err := c.searchF2(gctx, q.Keyword, PlatformTikTok, q.Limit)
			add(items, "f2-tiktok", warn, err)

			return nil
		})
	}

	if wanted[PlatformDouyin] && c != nil && c.sidecarAlive(ctx, c.F2URL) {
		g.Go(func() error {
			items, warn, err := c.searchF2(gctx, q.Keyword, PlatformDouyin, q.Limit)
			add(items, "f2-douyin", warn, err)

			return nil
		})
	}

	if wanted[PlatformDouyin] && c != nil && c.TikHubToken != "" {
		g.Go(func() error {
			items, err := c.searchTikHubDouyin(gctx, q.Keyword, q.Limit)
			add(items, "tikhub", "", err)

			return nil
		})
	}

	_ = g.Wait()

	merged := mergeHits(hits, q.Keyword, q.Limit)
	if len(merged) == 0 {
		warnings = append(warnings,
			"未找到公开主页。可换关键词，或启动 docker compose -f docker-compose.engine.yaml up -d 使用 TikTok-Api / f2。")
	}

	return Result{
		Hits:     merged,
		Warnings: uniqueStrings(warnings),
		Sources:  uniqueStrings(sources),
		Note:     messagePolicyNote,
	}, nil
}

func exhibitionUnavailable(keyword string) Result {
	return Result{
		Hits: nil,
		Warnings: []string{
			"展会获客没有高 star、仍在维护、许可证可商用的开源项目可复用（10times 相关仓库均为 0–1★ 且停更）。",
			"下一步按原优先级走第三方 API（例如 Apify 10times actor），而不是自研爬虫。",
		},
		Sources: []string{},
		Note:    "关键词「" + keyword + "」暂未检索。圈选模块「展会获客」待接入第三方展会 API。",
	}
}

func customsUnavailable(keyword string) Result {
	return Result{
		Hits: nil,
		Warnings: []string{
			"海关数据没有高 star 开源库可克隆（Customs-Crawler ~12★ 且依赖 Cookie 绕 Cloudflare，不嵌入）。",
			"已有 PR #12 复用 Kirchner / ImportYeti 第三方提单 API，本需求按「先 OSS、没有再第三方」先不自研。",
		},
		Sources: []string{},
		Note:    "关键词「" + keyword + "」请在海关 PR 合并后使用逐票提单；此处不自研爬虫。",
	}
}

func mergeHits(items []Hit, keyword string, limit int) []Hit {
	seen := make(map[string]Hit, len(items))
	order := make([]string, 0, len(items))
	kw := strings.ToLower(strings.TrimSpace(keyword))

	for _, hit := range items {
		if hit.ID == "" {
			hit.ID = hit.Platform + ":" + hit.HomepageURL
		}

		hit.Score += keywordBonus(hit, kw)
		if prev, ok := seen[hit.ID]; ok {
			if hit.Score > prev.Score {
				seen[hit.ID] = hit
			}

			continue
		}

		seen[hit.ID] = hit
		order = append(order, hit.ID)
	}

	out := make([]Hit, 0, len(order))
	for _, id := range order {
		out = append(out, seen[id])
	}

	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Score > out[i].Score {
				out[i], out[j] = out[j], out[i]
			}
		}
	}

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}

	return out
}

func keywordBonus(hit Hit, kw string) int {
	if kw == "" {
		return 0
	}

	blob := strings.ToLower(strings.Join([]string{hit.Name, hit.Handle, hit.Title, hit.Snippet}, " "))
	switch {
	case strings.EqualFold(hit.Handle, strings.TrimPrefix(kw, "@")):
		return 40
	case strings.Contains(blob, kw):
		return 15
	default:
		return 0
	}
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))

	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}

		if _, ok := seen[s]; ok {
			continue
		}

		seen[s] = struct{}{}
		out = append(out, s)
	}

	return out
}
