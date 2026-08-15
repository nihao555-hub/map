package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	maxLimit             = 20000
	messagePolicyNote    = "系统不会代发。公开网页索引按品类找店铺/公司/采购商主页，不是外贸通那种一次几万条的企业库。"
	marketingPolicyNote  = "系统不会代发。"
	customsPolicyNote    = "系统不会代发。逐票企业来自多家公开海关源的实时检索（美国海关海运提单，Kirchner / ImportYeti）；金额和国家口径来自联合国 Comtrade 与世界银行。不是外贸通那种全球企业库，也没有联系人穿透。"
	exhibitionPolicyNote = "系统不会代发。展会按关键词实时查 EventsEye（全球约 1.2 万场）、AUMA、Wikidata 和开源展会日历；参展商名单来自展会官网和公开名录，不是 50 万采购商库，也不做名片 OCR。"
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

	if q.Kind == KindPeople {
		q.Role = NormalizeRole(q.Role)
	}

	if q.Limit < 0 {
		q.Limit = 0
	}
	if q.Limit > maxLimit {
		q.Limit = maxLimit
	}

	if !testing.Testing() && (q.Kind == KindCustoms || q.Kind == KindExhibition || q.Kind == KindPeople) {
		return c.searchRealtime(ctx, q, start)
	}

	if cached, ok := lookupSearchCache(q); ok {
		cached.TookMS = time.Since(start).Milliseconds()
		cached.SearchedAt = time.Now().UTC()
		cached.Cached = true
		return cached, nil
	}

	res, err := c.runKind(ctx, q)
	if err != nil {
		return Result{}, err
	}
	res = finalizeResult(q, res, start)
	storeSearchCache(q, res)
	return res, nil
}

func (c *Client) runKind(ctx context.Context, q Query) (Result, error) {
	switch q.Kind {
	case KindPeople:
		return c.searchPeople(ctx, q)
	case KindMarketing:
		return c.searchMarketing(ctx, q)
	case KindExhibition:
		return c.searchExhibition(ctx, q)
	case KindCustoms:
		return c.searchCustoms(ctx, q)
	default:
		return Result{}, fmt.Errorf("unknown kind %q", q.Kind)
	}
}

func finalizeResult(q Query, res Result, start time.Time) Result {
	if q.Precise && q.Kind != KindCustoms && q.Kind != KindExhibition {
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
	if res.Note == "" && q.Kind == KindCustoms {
		res.Note = customsPolicyNote
	}
	if res.Note == "" && q.Kind == KindExhibition {
		res.Note = exhibitionPolicyNote
	}
	return res
}

// searchRealtime always returns within about 1s. Fresh cache is instant;
// expired cache is served immediately while a refresh runs; a cold query
// waits up to realtimeBudget then returns whatever is ready.
func (c *Client) searchRealtime(ctx context.Context, q Query, start time.Time) (Result, error) {
	if fresh, ok := lookupSearchCache(q); ok {
		fresh.TookMS = time.Since(start).Milliseconds()
		fresh.SearchedAt = time.Now().UTC()
		fresh.Cached = true
		fresh.Refreshing = refreshInFlight(q)
		return fresh, nil
	}

	stale, hasStale := lookupStaleCache(q)
	already := refreshInFlight(q)
	job := kickSearchRefresh(c, q)
	if hasStale {
		stale.TookMS = time.Since(start).Milliseconds()
		stale.SearchedAt = time.Now().UTC()
		stale.Cached = true
		stale.Refreshing = true
		return stale, nil
	}
	if already {
		return Result{
			Keyword:    q.Keyword,
			Kind:       q.Kind,
			Note:       policyNoteFor(q.Kind),
			TookMS:     time.Since(start).Milliseconds(),
			SearchedAt: time.Now().UTC(),
			Refreshing: true,
		}, nil
	}

	timer := time.NewTimer(realtimeBudget)
	defer timer.Stop()
	select {
	case <-job.done:
	case <-timer.C:
	case <-ctx.Done():
	}

	if fresh, ok := lookupSearchCacheAlways(q); ok {
		fresh.TookMS = time.Since(start).Milliseconds()
		fresh.SearchedAt = time.Now().UTC()
		fresh.Refreshing = refreshInFlight(q)
		return fresh, nil
	}
	if stale, ok := lookupCache(q, true); ok {
		stale.TookMS = time.Since(start).Milliseconds()
		stale.SearchedAt = time.Now().UTC()
		stale.Cached = true
		stale.Refreshing = refreshInFlight(q)
		return stale, nil
	}

	return Result{
		Keyword:    q.Keyword,
		Kind:       q.Kind,
		Hits:       nil,
		Note:       policyNoteFor(q.Kind),
		TookMS:     time.Since(start).Milliseconds(),
		SearchedAt: time.Now().UTC(),
		Refreshing: refreshInFlight(q),
	}, nil
}

func policyNoteFor(kind string) string {
	switch kind {
	case KindMarketing:
		return marketingPolicyNote
	case KindCustoms:
		return customsPolicyNote
	case KindExhibition:
		return exhibitionPolicyNote
	default:
		return messagePolicyNote
	}
}

func (c *Client) searchPeople(ctx context.Context, q Query) (Result, error) {
	wanted := wantedPeoplePlatforms(q.Platforms)

	var (
		mu       sync.Mutex
		hits     []Hit
		warnings []string
		sources  []string
		expanded []string
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
		items, warns, srcs, terms := c.searchPublicProfiles(gctx, q, wanted, func(partial []Hit) {
			publishPeopleProgress(q, partial)
		})
		src := strings.Join(srcs, "+")
		warn := strings.Join(warns, "; ")
		add(items, src, warn, nil)
		mu.Lock()
		expanded = terms
		mu.Unlock()

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

	merged := mergeHits(hits, q.Keyword, q.Limit, q.Role, q.Country)
	if len(merged) > 0 {
		extra := c.expandMerchantSocials(ctx, merged, wanted)
		if len(extra) > 0 {
			merged = mergeHits(append(merged, extra...), q.Keyword, q.Limit, q.Role, q.Country)
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
		Expanded: expanded,
	}, nil
}

var (
	peopleProgressMu   sync.Mutex
	lastPeopleProgress = map[string]time.Time{}
)

func publishPeopleProgress(q Query, items []Hit) {
	if testing.Testing() || len(items) == 0 {
		return
	}
	key := searchCacheKey(q)
	peopleProgressMu.Lock()
	if lastPeopleProgress == nil {
		lastPeopleProgress = map[string]time.Time{}
	}
	if time.Since(lastPeopleProgress[key]) < 800*time.Millisecond {
		peopleProgressMu.Unlock()
		return
	}
	lastPeopleProgress[key] = time.Now()
	peopleProgressMu.Unlock()

	merged := mergeHits(items, q.Keyword, q.Limit, q.Role, q.Country)
	if len(merged) == 0 {
		return
	}
	storeSearchCache(q, finalizeResult(q, Result{
		Hits: merged,
		Note: messagePolicyNote,
	}, time.Now()))
}

func mergeHits(items []Hit, keyword string, limit int, role, country string) []Hit {
	seen := make(map[string]Hit, len(items))
	order := make([]string, 0, len(items))
	kw := strings.ToLower(strings.TrimSpace(keyword))

	for _, hit := range items {
		if hit.ID == "" {
			hit.ID = hit.Platform + ":" + hit.HomepageURL
		}
		if hit.Kind != KindMarketing {
			if !isSocialHomepage(hit) || isNoiseHit(hit, kw, role) {
				continue
			}
			hit = cleanHitName(hit)
		}

		hit.Score += keywordBonus(hit, kw)
		hit.Score += merchantBonus(hit, kw, role)
		if role != "" {
			hit.Role = inferHitRole(hit, role)
		}
		hit.Country, hit.CountryLabel = inferHitCountry(hit, country)
		hit.Score += countryBonus(hit, country)
		if selected := strings.ToUpper(strings.TrimSpace(country)); selected != "" && hit.Country != "" && hit.Country != selected {
			continue
		}
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

func hitPageBlob(hit Hit) string {
	return foldSearchText(strings.Join([]string{
		hit.Name, hit.Handle, hit.Title, hit.Snippet, hit.Contact, hit.HomepageURL,
	}, " "))
}

func hitKeywordBlob(hit Hit) string {
	blob := hitPageBlob(hit)
	if hit.Extra != nil && hit.Extra["q"] != "" {
		blob = foldSearchText(blob + " " + hit.Extra["q"])
	}
	return blob
}

func hitRoleBlob(hit Hit) string {
	return foldSearchText(strings.Join([]string{hit.Name, hit.Handle, hit.Title, hit.Snippet}, " "))
}

func hitMatchesKeyword(hit Hit, kw string) bool {
	return blobMatchesKeyword(hitKeywordBlob(hit), kw)
}

func blobMatchesKeyword(blob, kw string) bool {
	kw = foldSearchText(kw)
	if kw == "" {
		return true
	}
	blob = foldSearchText(blob)
	if strings.Contains(blob, kw) {
		return true
	}
	for _, alias := range productSearchAliases(kw) {
		if alias != "" && strings.Contains(blob, foldSearchText(alias)) {
			return true
		}
	}
	// "power tools" should match handles like PowerbiltTools / boschpowertools.
	if tokens := strings.Fields(kw); len(tokens) >= 2 {
		ok := true
		for _, tok := range tokens {
			tok = foldSearchText(tok)
			if len([]rune(tok)) < 2 || !strings.Contains(blob, tok) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}

	return false
}

func keywordBonus(hit Hit, kw string) int {
	if kw == "" {
		return 0
	}

	blob := hitKeywordBlob(hit)
	kw = foldSearchText(kw)
	switch {
	case strings.EqualFold(hit.Handle, strings.TrimPrefix(kw, "@")):
		return 40
	case blobMatchesKeyword(blob, kw):
		return 15
	default:
		return 0
	}
}

func merchantBonus(hit Hit, kw, role string) int {
	roleBlob := hitRoleBlob(hit)
	kwBlob := hitKeywordBlob(hit)
	score := 0
	if isProfileURL(hit.HomepageURL) {
		score += 12
	}
	if isContentURL(hit.HomepageURL) {
		score -= 18
	}

	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleBuyer:
		if hasCustomerToken(roleBlob) {
			score += 24
		}
		if hasCompanyToken(roleBlob) {
			score += 6
		}
		if hasFactoryToken(roleBlob) && !hasCustomerToken(roleBlob) {
			score -= 20
		}
		if looksLikeOfficialBrand(roleBlob) {
			score -= 18
		}
		if kw != "" && !blobMatchesKeyword(kwBlob, kw) && !hasCustomerToken(roleBlob) {
			score -= 28
		}
	case RoleSeller:
		if hasSellerToken(roleBlob) {
			score += 22
		}
		if kw != "" && !blobMatchesKeyword(kwBlob, kw) && !hasMerchantToken(roleBlob) {
			score -= 28
		}
	default:
		if hasMerchantToken(roleBlob) {
			score += 22
		}
		if kw != "" && !blobMatchesKeyword(kwBlob, kw) && !hasMerchantToken(roleBlob) {
			score -= 28
		}
	}

	return score
}

func inferHitRole(hit Hit, queryRole string) string {
	blob := hitRoleBlob(hit)
	switch {
	case hasCustomerToken(blob) && !hasFactoryToken(blob):
		return RoleBuyer
	case hasFactoryToken(blob) && !hasBuyerToken(blob):
		return RoleSeller
	default:
		return NormalizeRole(queryRole)
	}
}

func looksLikeProfileName(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" || genericSocialLabel(n) {
		return false
	}
	if strings.HasPrefix(n, "MS4w") {
		return false
	}
	return true
}

func isGenericProductName(name, kw string) bool {
	n := foldSearchText(strings.TrimSpace(name))
	if n == "" {
		return true
	}
	n = strings.Trim(n, "()[]{}<>|/\\.,;:：·-—_")
	if n == "" {
		return true
	}
	kw = foldSearchText(kw)
	if kw != "" && (n == kw || n == strings.ReplaceAll(kw, " ", "")) {
		return true
	}
	for _, alias := range productSearchAliases(kw) {
		a := foldSearchText(alias)
		if a != "" && (n == a || n == strings.ReplaceAll(a, " ", "")) {
			return true
		}
	}
	switch n {
	case "led", "leds", "lighting", "light", "lights", "lamp", "lamps":
		return true
	}

	return false
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

func isNoiseHit(hit Hit, kw, role string) bool {
	if !isSocialHomepage(hit) {
		return true
	}

	if genericSocialLabel(hit.Name) {
		return true
	}
	if isGenericProductName(hit.Name, kw) {
		return true
	}
	if looksLikeClickbait(hit.Name) {
		return true
	}

	roleBlob := hitRoleBlob(hit)
	if looksLikeTutorial(roleBlob + " " + foldSearchText(hit.HomepageURL)) {
		return true
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role == RoleBuyer && looksLikeOfficialBrand(roleBlob) && !hasCustomerToken(roleBlob) {
		return true
	}
	if role == RoleBuyer && hasFactoryToken(roleBlob) && !hasBuyerToken(roleBlob) {
		if !strings.Contains(kw, "http") {
			return true
		}
	}
	pageMatch := kw == "" || blobMatchesKeyword(hitPageBlob(hit), kw)
	if !pageMatch {
		queryMatch := blobMatchesKeyword(hitKeywordBlob(hit), kw)
		if queryMatch {
			if role == RoleBuyer {
				if !hasBuyerKeepToken(roleBlob) {
					return true
				}
			} else if !hasMerchantToken(roleBlob) && !hasCompanyToken(roleBlob) {
				return true
			}
		} else if role == RoleBuyer {
			if !hasBuyerKeepToken(roleBlob) {
				return true
			}
		} else if !hasMerchantToken(roleBlob) {
			return true
		}
	} else if role == RoleBuyer && !strings.Contains(kw, "http") {
		if !hasBuyerKeepToken(roleBlob) {
			return true
		}
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
	return hasBuyerToken(blob) || hasSellerToken(blob) || hasShopToken(blob)
}

func hasCustomerToken(blob string) bool {
	return hasBuyerToken(blob) || hasResellerToken(blob)
}

func hasBuyerToken(blob string) bool {
	tokens := []string{
		"采购", "进口商", "进口", "采购商", "采购部", "求购", "寻找供应商",
		"importer", "importers", "buyer", "buyers",
		"procurement", "purchasing", "importing", "sourcing",
		"นำเข้า", "ผู้นำเข้า", "จัดซื้อ",
		"nhập khẩu", "pengimport", "importir",
	}
	return containsAnyToken(blob, tokens)
}

func hasResellerToken(blob string) bool {
	tokens := []string{
		"批发", "经销", "贸易", "商行",
		"wholesaler", "wholesale", "distributor", "dealer",
		"trading", "retailer",
	}
	return containsAnyToken(blob, tokens)
}

func hasFactoryToken(blob string) bool {
	tokens := []string{
		"厂家", "工厂", "制造商", "旗舰", "专卖", "专营",
		"factory", "manufacturer", "manufacturing",
	}
	if containsAnyToken(blob, tokens) {
		return true
	}
	for _, r := range []rune(blob) {
		if r == '厂' {
			return true
		}
	}
	return false
}

func looksLikeClickbait(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" {
		return false
	}
	for _, tok := range []string{"😱", "🔥", "😂", "点击", "点赞", "viral"} {
		if strings.Contains(n, tok) {
			return true
		}
	}
	return false
}

func looksLikeOfficialBrand(blob string) bool {
	return containsAnyToken(blob, []string{"官方", "official page", "official store", "official account", "旗舰店"})
}

func hasSellerToken(blob string) bool {
	return hasFactoryToken(blob) || containsAnyToken(blob, []string{"供应", "supplier"})
}

func hasBuyerKeepToken(blob string) bool {
	return hasCustomerToken(blob) || hasCompanyToken(blob) || hasShopToken(blob)
}

func hasShopToken(blob string) bool {
	tokens := []string{
		"店铺", "商行", "贸易",
		"trading", "retailer", "store", "shop",
	}
	if containsAnyToken(blob, tokens) {
		return true
	}
	for _, r := range []rune(blob) {
		if r == '店' {
			return true
		}
	}

	return false
}

func hasCompanyToken(blob string) bool {
	tokens := []string{
		"有限公司", "有限责任", "集团", " ltd", "llc", "gmbh", "pte",
		"sdn bhd", " co.", "company", "corp",
	}
	return containsAnyToken(blob, tokens)
}

func containsAnyToken(blob string, tokens []string) bool {
	for _, tok := range tokens {
		if strings.Contains(blob, tok) {
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
