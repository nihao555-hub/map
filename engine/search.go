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
	maxLimit            = 200
	messagePolicyNote   = "系统不会代发。"
	marketingPolicyNote = "系统不会代发。"
)

// Search runs customer discovery. People search uses public web indexes by default
// and overlays cloned OSS sidecars (TikTok-Api / f2) when they are healthy.
func (c *Client) Search(ctx context.Context, q Query) (Result, error) {
	start := time.Now()
	q.Keyword = NormalizeKeyword(q.Keyword)
	q.Kind = strings.ToLower(strings.TrimSpace(q.Kind))
	q.Mode = strings.ToLower(strings.TrimSpace(q.Mode))
	q.Channel = strings.ToLower(strings.TrimSpace(q.Channel))

	if err := ValidateKeyword(q.Keyword, q.Precise); err != nil {
		return Result{}, err
	}

	q.Country = strings.ToUpper(strings.TrimSpace(q.Country))
	ctx = WithSearchCountry(ctx, q.Country)

	if q.Mode == ModeMarketing || q.Kind == KindMarketing {
		q.Kind = KindMarketing
		q.Mode = ModeMarketing
	}

	if q.Kind == "" {
		q.Kind = KindPeople
	}

	if q.Mode == "" && q.Kind == KindPeople {
		q.Mode = ModeHomepage
	}

	if q.Limit < 0 {
		q.Limit = 0
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
	case KindMarketing:
		res, err = c.searchMarketing(ctx, q)
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

	if q.Precise {
		res.Hits = filterPreciseHits(res.Hits, q.Keyword)
	}

	res.Keyword = q.Keyword
	res.Kind = q.Kind
	res.TookMS = time.Since(start).Milliseconds()
	res.SearchedAt = time.Now().UTC()

	if res.Note == "" && q.Kind == KindPeople {
		res.Note = messagePolicyNote
	}
	if res.Note == "" && q.Kind == KindMarketing {
		res.Note = marketingPolicyNote
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
		items, warns, srcs := c.searchPublicProfiles(gctx, q.Keyword, q.Country, wanted, q.Limit)
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
	if len(merged) > 0 {
		extra := c.expandMerchantSocials(ctx, merged, wanted)
		if len(extra) > 0 {
			merged = mergeHits(append(merged, extra...), q.Keyword, q.Limit)
			sources = append(sources, "expand-socials")
		}
		merged = groupExpandedHits(merged)
	}
	if len(merged) == 0 {
		warnings = append(warnings, "未找到公开主页，请换关键词。")
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
		if hit.Kind != KindMarketing {
			if !isSocialHomepage(hit) || isNoiseHit(hit, kw) {
				continue
			}
			hit = cleanHitName(hit)
		}

		hit.Score += keywordBonus(hit, kw)
		hit.Score += merchantBonus(hit, kw)
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

func filterPreciseHits(hits []Hit, keyword string) []Hit {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" || len(hits) == 0 {
		return hits
	}

	out := make([]Hit, 0, len(hits))
	for _, hit := range hits {
		if hitMatchesKeyword(hit, kw) {
			out = append(out, hit)
		}
	}

	return out
}

func hitMatchesKeyword(hit Hit, kw string) bool {
	blob := foldSearchText(strings.Join([]string{
		hit.Name, hit.Handle, hit.Title, hit.Snippet, hit.Contact, hit.HomepageURL,
	}, " "))

	return strings.Contains(blob, foldSearchText(kw))
}

func keywordBonus(hit Hit, kw string) int {
	if kw == "" {
		return 0
	}

	blob := foldSearchText(strings.Join([]string{hit.Name, hit.Handle, hit.Title, hit.Snippet}, " "))
	kw = foldSearchText(kw)
	switch {
	case strings.EqualFold(hit.Handle, strings.TrimPrefix(kw, "@")):
		return 40
	case strings.Contains(blob, kw):
		return 15
	default:
		return 0
	}
}

func merchantBonus(hit Hit, kw string) int {
	blob := foldSearchText(strings.Join([]string{hit.Name, hit.Handle, hit.Title, hit.Snippet, hit.HomepageURL}, " "))
	score := 0
	if isProfileURL(hit.HomepageURL) {
		score += 12
	}
	if isContentURL(hit.HomepageURL) {
		score -= 18
	}
	if hasMerchantToken(blob) {
		score += 22
	}
	if kw != "" && !strings.Contains(blob, foldSearchText(kw)) && !hasMerchantToken(blob) {
		score -= 28
	}

	return score
}

func genericSocialLabel(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.TrimSuffix(n, "...")
	switch n {
	case "facebook", "youtube", "tiktok", "instagram", "linkedin", "douyin", "抖音",
		"小红书", "twitter", "x", "threads", "pinterest", "telegram", "reddit",
		"twitch", "微博", "bilibili", "b站":
		return true
	}
	return false
}

func isNoiseHit(hit Hit, kw string) bool {
	if !isSocialHomepage(hit) {
		return true
	}

	if genericSocialLabel(hit.Name) {
		return true
	}

	blob := foldSearchText(strings.Join([]string{hit.Name, hit.Handle, hit.Title, hit.Snippet, hit.HomepageURL}, " "))
	if looksLikeTutorial(blob) {
		return true
	}
	if kw != "" && !strings.Contains(blob, foldSearchText(kw)) && !hasMerchantToken(blob) {
		return true
	}

	return false
}

func cleanHitName(hit Hit) Hit {
	n := strings.ToLower(strings.TrimSpace(hit.Name))
	n = strings.TrimSuffix(n, "...")
	switch n {
	case "videos", "photos", "posts", "about", "home", "more", "see more":
		if hit.Handle != "" {
			hit.Name = hit.Handle
		}
	}

	return hit
}

func isContentURL(raw string) bool {
	u := strings.ToLower(raw)
	if strings.Contains(u, "v.douyin.com/") || strings.Contains(u, "youtu.be/") {
		return true
	}
	for _, p := range []string{"/video/", "/watch?", "/watch/", "/shorts/", "/explore/", "/note/", "/collection/", "/short-video/"} {
		if strings.Contains(u, p) {
			return true
		}
	}
	if strings.HasSuffix(strings.Split(u, "?")[0], "/watch") {
		return true
	}

	return false
}

func isProfileURL(raw string) bool {
	u := strings.ToLower(raw)
	for _, p := range []string{"/user/", "/@", "/in/", "/company/", "/profile", "/people/"} {
		if strings.Contains(u, p) {
			return true
		}
	}
	if strings.Contains(u, "facebook.com/") && !isContentURL(u) {
		return true
	}

	return false
}

func hasMerchantToken(blob string) bool {
	tokens := []string{
		"厂家", "工厂", "专卖", "批发", "经销", "贸易", "进口", "出口", "采购", "商行",
		"照明", "灯饰", "店铺", "官方", "供应",
		"official", "wholesaler", "wholesale", "importer", "distributor",
		"retailer", "factory", "lighting", "trading", "supplier", "store", "shop",
	}
	for _, tok := range tokens {
		if strings.Contains(blob, tok) {
			return true
		}
	}
	// 「店」「厂」太短，只在独立词里算商家。
	for _, r := range []rune(blob) {
		if r == '店' || r == '厂' {
			return true
		}
	}

	return false
}

func looksLikeTutorial(blob string) bool {
	for _, tok := range []string{
		"安装图解", "工作原理", "亲自动手", "怎么安装", "怎么做", "教程", "教学",
		"图解", "原理", "diy", "how to", "tutorial", "explained", "演示",
	} {
		if strings.Contains(blob, tok) {
			return true
		}
	}

	return false
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
