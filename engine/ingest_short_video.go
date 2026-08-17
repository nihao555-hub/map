package engine

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/sync/errgroup"
)

// DefaultShortVideoKeywords is the full 外贸品类扫 used for Douyin / TikTok
// business homepages. Public indexes have no dump of every shop account.
var DefaultShortVideoKeywords = []string{
	"LED灯", "灯具", "照明", "灯带", "台灯", "户外灯",
	"电动工具", "五金", "工具", "电钻", "角磨机", "扳手",
	"配电柜", "开关柜", "电缆", "电线", "开关", "插座", "变压器", "逆变器",
	"家具", "办公家具", "沙发", "床垫", "柜子",
	"服装", "女装", "男装", "童装", "内衣", "运动鞋", "鞋子", "箱包",
	"化妆品", "护肤品", "美妆", "洗护",
	"包装", "纸箱", "塑料袋", "胶带",
	"阀门", "泵", "压缩机", "轴承", "机械", "数控机床", "模具",
	"太阳能", "光伏", "储能",
	"汽车配件", "轮胎", "刹车片", "机油",
	"塑料制品", "橡胶", "硅胶",
	"食品", "零食", "海鲜", "茶叶", "咖啡", "白酒", "饮料",
	"卫浴", "瓷砖", "建材", "铝型材", "钢材", "玻璃", "门窗",
	"纺织", "面料", "家纺", "毛巾",
	"玩具", "宠物用品", "母婴",
	"家电", "小家电", "厨房用品", "日用品", "清洁用品",
	"医疗器械", "劳保", "安防", "监控",
	"农业机械", "化肥", "饲料", "农药",
	"自行车", "摩托车配件", "滑板车",
	"音响", "耳机", "手机配件", "充电器", "数据线",
	"打印机", "办公用品", "文具",
	"power tools", "LED lighting", "switchgear", "furniture",
	"auto parts", "packaging", "solar panel", "kitchenware",
	"textiles", "cosmetics", "valves", "pumps", "bearings",
	"bathroom fittings", "ceramic tiles", "hardware tools",
	"electric scooter", "phone accessories", "medical supplies",
	"pet supplies", "baby products", "home appliances",
	"panel listrik", "perabot", "suku cadang", "alat listrik",
}

// FastShortVideoKeywords is the compact 2-hour set. LocalSearchTerms
// still expands each one into Thai / Malay / Vietnamese / Indonesian.
var FastShortVideoKeywords = []string{
	"LED灯", "电动工具", "配电柜", "家具", "服装", "鞋子", "化妆品",
	"包装", "阀门", "太阳能", "汽车配件", "食品", "卫浴", "家电", "五金",
}

var seaShortVideoKeywords = []string{
	"panel listrik", "lemari listrik", "toko listrik", "toko lampu",
	"toko furniture", "pabrik", "grosir", "perabot",
	"suku cadang", "alat listrik", "perkakas listrik",
	"เครื่องมือไฟฟ้า", "ร้านไฟ", "โรงงาน",
	"đồ điện", "cửa hàng đèn", "nhà máy",
	"kedai lampu", "alatan kuasa",
	"power tools shop", "LED lighting shop", "furniture store",
}

// seaCitySidecarKeywords is city × shop phrase. TikTok user search only
// returns the first result page, so city terms are how we fan out in SEA.
var seaCities = []string{
	"jakarta", "surabaya", "bandung", "medan",
	"bangkok", "chiang mai",
	"kuala lumpur", "penang",
	"ho chi minh", "hanoi",
	"manila", "cebu", "singapore",
}

var seaCitySeeds = []string{
	"toko listrik", "toko lampu", "furniture shop", "LED shop",
	"auto parts", "shoe shop", "clothing wholesale", "hardware store",
}

func seaCitySidecarKeywords() []string {
	out := make([]string, 0, len(seaCities)*len(seaCitySeeds))
	for _, city := range seaCities {
		for _, seed := range seaCitySeeds {
			out = append(out, seed+" "+city)
		}
	}
	return out
}

var meShortVideoKeywords = []string{
	"lighting dubai", "furniture dubai", "auto parts dubai",
	"أدوات كهربائية", "أثاث",
}

// DefaultShortVideoCountries is Southeast Asia first, then Middle East, then the West.
var DefaultShortVideoCountries = []string{
	"ID", "TH", "MY", "VN", "SG", "PH",
	"AE", "SA", "TR",
	"US", "DE", "GB",
}

type shortVideoRegion struct {
	Name      string
	Countries []string
	Extra     []string
}

func defaultShortVideoRegions() []shortVideoRegion {
	return []shortVideoRegion{
		{Name: "sea", Countries: []string{"ID", "TH", "MY", "VN", "SG", "PH"}, Extra: append(append([]string{}, seaShortVideoKeywords...), seaCitySidecarKeywords()...)},
		{Name: "me", Countries: []string{"AE", "SA", "TR", "EG", "QA"}, Extra: meShortVideoKeywords},
		{Name: "west", Countries: []string{"US", "DE", "GB", "FR", "NL", "IT"}},
	}
}

func resolveShortVideoRegions(opt HarvestOptions) []shortVideoRegion {
	all := defaultShortVideoRegions()
	if len(opt.Regions) == 0 {
		if opt.Fast {
			return all
		}
		return nil
	}
	want := map[string]bool{}
	for _, r := range opt.Regions {
		want[strings.ToLower(strings.TrimSpace(r))] = true
	}
	var out []shortVideoRegion
	for _, r := range all {
		if want[r.Name] {
			out = append(out, r)
		}
	}
	return out
}

func shortVideoWanted() map[string]bool {
	return map[string]bool{
		PlatformTikTok: true,
		PlatformDouyin: true,
	}
}

// HarvestShortVideo stores Douyin / TikTok business homepage URLs only.
// It reuses davidteather/TikTok-Api and Johnserf-Seed/f2 when those
// sidecars are up, plus public web indexes. It cannot dump the platforms.
func (c *Client) HarvestShortVideo(ctx context.Context, opt HarvestOptions) (HarvestStats, error) {
	started := time.Now()
	if opt.DBPath == "" {
		opt.DBPath = ResolveMerchantDB("")
	}
	if opt.Sidecar && len(opt.Regions) == 0 && !opt.Fast {
		opt.Regions = []string{"sea"}
	}
	if opt.Fast || opt.Sidecar {
		if len(opt.Keywords) == 0 {
			opt.Keywords = append([]string{}, FastShortVideoKeywords...)
		}
		if opt.Deadline <= 0 {
			opt.Deadline = 110 * time.Minute
		}
	}
	if opt.Sidecar {
		if opt.Workers <= 0 {
			opt.Workers = 2
		}
		if opt.QueryLimit <= 0 {
			opt.QueryLimit = 0
		}
	} else if opt.Fast {
		if opt.Workers <= 0 {
			opt.Workers = 8
		}
		if opt.QueryLimit <= 0 {
			opt.QueryLimit = 2
		}
	} else {
		if len(opt.Keywords) == 0 {
			opt.Keywords = append([]string{}, DefaultShortVideoKeywords...)
		}
		if opt.Workers <= 0 {
			opt.Workers = 4
		}
		if opt.QueryLimit <= 0 {
			opt.QueryLimit = 6
		}
	}
	if len(opt.Countries) == 0 {
		opt.Countries = append([]string{}, DefaultShortVideoCountries...)
	}
	if opt.Deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opt.Deadline)
		defer cancel()
	}
	dir, err := OpenDirectory(opt.DBPath)
	if err != nil {
		return HarvestStats{Took: time.Since(started), Err: err.Error()}, err
	}
	defer dir.Close()

	wanted := shortVideoWanted()
	var (
		mu       sync.Mutex
		seen     = map[string]Hit{}
		flushed  = map[string]struct{}{}
		queries  int
		inserted int
		byPlat   = map[string]int{}
	)
	remember := func(kw, cc string, batch []Hit, nq int) {
		queries += nq
		for _, h := range batch {
			id := shortVideoExtID(h)
			if id == "" {
				continue
			}
			if prev, ok := seen[id]; ok && !(h.Name != "" && (prev.Name == "" || prev.Name == prev.Handle)) {
				continue
			}
			if h.Extra == nil {
				h.Extra = map[string]string{}
			}
			h.Extra["ext_id"] = id
			h.Extra["src"] = h.Platform
			h.Extra["shop"] = harvestShop(kw)
			if h.Country == "" {
				h.Country = strings.ToUpper(cc)
			}
			seen[id] = h
		}
	}
	flush := func() error {
		var rows []Merchant
		for id, h := range seen {
			if _, ok := flushed[id]; ok {
				continue
			}
			rows = append(rows, hitToMerchant(h, id))
			flushed[id] = struct{}{}
			byPlat[h.Platform]++
		}
		if len(rows) == 0 {
			return nil
		}
		n, err := dir.InsertBatch(ctx, rows)
		inserted += n
		logIngest("short-video flush +%d inserted=%d unique=%d", n, inserted, len(seen))
		return err
	}
	runBatch := func(limit int, keywords, countries []string) error {
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(limit)
		for _, kw := range keywords {
			kw := strings.TrimSpace(kw)
			if kw == "" {
				continue
			}
			for _, cc := range countries {
				cc := cc
				g.Go(func() error {
					if gctx.Err() != nil {
						return nil
					}
					batch, nq := c.collectShortVideoHits(gctx, kw, cc, wanted, opt.QueryLimit)
					mu.Lock()
					defer mu.Unlock()
					remember(kw, cc, batch, nq)
					logIngest("short-video %s %s +%d (unique=%d inserted=%d)", kw, firstNonEmpty(cc, "ALL"), len(batch), len(seen), inserted)
					return flush()
				})
			}
		}
		return g.Wait()
	}
	runSidecarTerms := func(limit int, terms []string) error {
		if limit <= 0 {
			limit = 2
		}
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(limit)
		for _, term := range terms {
			term := strings.TrimSpace(term)
			if term == "" {
				continue
			}
			g.Go(func() error {
				if gctx.Err() != nil {
					return nil
				}
				batch := c.collectSidecarHits(gctx, term)
				mu.Lock()
				defer mu.Unlock()
				remember(term, "SEA", batch, 1)
				logIngest("short-video sidecar %s +%d (unique=%d inserted=%d)", term, len(batch), len(seen), inserted)
				return flush()
			})
		}
		return g.Wait()
	}
	finish := func(err error) (HarvestStats, error) {
		st := HarvestStats{
			Keywords: len(opt.Keywords),
			Queries:  queries,
			Hits:     len(seen),
			Inserted: inserted,
			Profiles: len(seen),
			ByPlat:   byPlat,
			Took:     time.Since(started),
		}
		if err != nil && ctx.Err() == nil {
			st.Err = err.Error()
		}
		_ = dir.RecordRun(ctx, "short-video", started, inserted, st.String())
		if err != nil && ctx.Err() != nil {
			return st, nil
		}
		return st, err
	}

	if opt.Fast || opt.SeedOnly {
		orig := ""
		if c != nil {
			orig = c.WikidataURL
			if orig == "" || strings.Contains(orig, "query.wikidata.org") {
				c.WikidataURL = QleverWikidataSPARQL
			}
			wd := c.ingestWikidataShortVideo(ctx, dir)
			logIngest("short-video wikidata seed %s", wd)
			c.WikidataURL = orig
		}
		if opt.SeedOnly {
			return finish(nil)
		}
	}
	if opt.Wayback {
		wb := c.ingestWaybackTikTok(ctx, dir)
		logIngest("short-video wayback %s", wb)
		if !opt.Sidecar && !opt.Fast {
			return finish(nil)
		}
	}

	regions := resolveShortVideoRegions(opt)
	sidecarUp := c != nil && (c.sidecarAlive(ctx, c.TikTokURL) || c.sidecarAlive(ctx, c.F2URL))
	if sidecarUp && (opt.Sidecar || opt.Fast) {
		kws := append([]string{}, opt.Keywords...)
		ccs := append([]string{}, opt.Countries...)
		if len(regions) > 0 {
			kws = nil
			ccs = nil
			for _, reg := range regions {
				kws = append(kws, opt.Keywords...)
				kws = append(kws, reg.Extra...)
				ccs = append(ccs, reg.Countries...)
			}
		}
		terms := uniqueShortVideoTerms(kws, ccs)
		logIngest("short-video sidecar terms=%d workers=%d", len(terms), opt.Workers)
		if err := runSidecarTerms(opt.Workers, terms); err != nil {
			return finish(err)
		}
		if opt.Sidecar {
			return finish(nil)
		}
	}

	if len(regions) > 0 {
		for _, reg := range regions {
			if ctx.Err() != nil {
				return finish(ctx.Err())
			}
			kws := append(append([]string{}, opt.Keywords...), reg.Extra...)
			logIngest("short-video region %s countries=%v keywords=%d", reg.Name, reg.Countries, len(kws))
			if err := runBatch(opt.Workers, kws, reg.Countries); err != nil {
				return finish(err)
			}
		}
		return finish(nil)
	}

	// Non-fast: keep a short Douyin pass, then SEA→ME→West country order.
	homeWorkers := opt.Workers
	if homeWorkers > 2 {
		homeWorkers = 2
	}
	if err := runBatch(homeWorkers, opt.Keywords, []string{""}); err != nil {
		return finish(err)
	}
	var overseas []string
	for _, cc := range opt.Countries {
		if strings.TrimSpace(cc) == "" {
			continue
		}
		overseas = append(overseas, cc)
	}
	if err := runBatch(opt.Workers, opt.Keywords, overseas); err != nil {
		return finish(err)
	}
	return finish(nil)
}

func (c *Client) collectShortVideoHits(ctx context.Context, keyword, country string, wanted map[string]bool, queryLimit int) ([]Hit, int) {
	terms := harvestDorkTerms(keyword, country)
	dorks := shortVideoQueries(terms, country)
	if queryLimit > 0 && len(dorks) > queryLimit {
		dorks = dorks[:queryLimit]
	}
	batch, nq := c.runTradeDorks(ctx, dorks, wanted)
	out := make([]Hit, 0, len(batch)+16)
	for _, h := range batch {
		if keepShortVideoBusiness(h, keyword) {
			out = append(out, h)
		}
	}
	if country == "" || strings.EqualFold(country, "CN") {
		out = append(out, c.collectSidecarHits(ctx, keyword)...)
		if c.TikHubToken != "" {
			if items, err := c.searchTikHubDouyin(ctx, keyword, 30); err == nil {
				for _, h := range items {
					if keepShortVideoBusiness(h, keyword) {
						out = append(out, h)
					}
				}
			}
		}
	}
	return out, nq
}

func uniqueShortVideoTerms(keywords, countries []string) []string {
	if len(countries) == 0 {
		countries = []string{""}
	}
	seen := map[string]bool{}
	var out []string
	add := func(term string) {
		term = strings.TrimSpace(term)
		key := strings.ToLower(term)
		if term == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, term)
	}
	for _, kw := range keywords {
		add(kw)
		for _, cc := range countries {
			for _, term := range harvestDorkTerms(kw, cc) {
				add(term)
			}
		}
	}
	return out
}

func (c *Client) collectSidecarHits(ctx context.Context, keyword string) []Hit {
	if c == nil {
		return nil
	}
	var out []Hit
	keep := func(items []Hit) {
		for _, h := range items {
			if keepShortVideoBusiness(h, keyword) {
				out = append(out, h)
			}
		}
	}
	if c.sidecarAlive(ctx, c.TikTokURL) {
		if items, _, err := c.searchTikTokAPI(ctx, keyword, 30); err == nil {
			keep(items)
		} else {
			logIngest("short-video TikTok-Api %s: %v", keyword, err)
		}
	}
	if c.sidecarAlive(ctx, c.F2URL) {
		if items, _, err := c.searchF2(ctx, keyword, PlatformTikTok, 30); err == nil {
			keep(items)
		} else {
			logIngest("short-video f2 %s: %v", keyword, err)
		}
	}
	return out
}

func shortVideoQueries(terms []string, country string) []publicQuery {
	terms = clipTerms(uniqueFoldedStrings(terms), 3)
	code := strings.ToUpper(strings.TrimSpace(LookupCountry(country).Code))
	allowDouyin := code == "" || code == "CN"
	var out []publicQuery
	seen := map[string]bool{}
	add := func(platform, query string) {
		query = strings.TrimSpace(query)
		if query == "" {
			return
		}
		key := platform + "\t" + query
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, publicQuery{platform: platform, query: query, preferGoogle: true})
	}
	for _, term := range terms {
		qt := quoteSearchTerm(term)
		if qt == "" {
			continue
		}
		add(PlatformTikTok, "site:tiktok.com/@ "+qt)
		add(PlatformTikTok, "site:tiktok.com/@ "+qt+" (official OR shop OR store OR factory OR supplier OR manufacturer)")
		if allowDouyin {
			add(PlatformDouyin, "site:douyin.com/user "+qt)
			add(PlatformDouyin, "site:douyin.com/user "+qt+" (工厂 OR 旗舰店 OR 批发 OR 厂家 OR 贸易)")
			add(PlatformDouyin, "intitle:"+qt+" site:douyin.com/user")
		}
	}
	return out
}

func shortVideoExtID(hit Hit) string {
	plat := strings.ToLower(strings.TrimSpace(hit.Platform))
	if plat != PlatformTikTok && plat != PlatformDouyin {
		return ""
	}
	handle := strings.ToLower(strings.TrimSpace(hit.Handle))
	if handle != "" {
		return plat + ":" + handle
	}
	home := strings.ToLower(strings.TrimSpace(hit.HomepageURL))
	if home == "" {
		return ""
	}
	return plat + ":" + strings.Trim(strings.NewReplacer("https://", "", "http://", "", "www.", "").Replace(home), "/")
}

func keepShortVideoBusiness(hit Hit, keyword string) bool {
	if hit.Platform != PlatformTikTok && hit.Platform != PlatformDouyin {
		return false
	}
	if !isSocialHomepage(hit) {
		return false
	}
	if hit.Platform == PlatformTikTok && !keepDorkHit(hit) {
		return false
	}
	if hit.Platform == PlatformDouyin && strings.TrimSpace(hit.HomepageURL) == "" {
		return false
	}
	blob := foldSearchText(hit.Name + " " + hit.Title + " " + hit.Snippet + " " + hit.Handle)
	if looksLikeClickbait(hit.Name) || looksLikeTutorial(blob) || looksLikePersonalCreator(blob) {
		return false
	}
	if hit.Verified || shortVideoBusinessSignal(blob) || looksLikeBrandHandle(hit.Handle) {
		return true
	}
	kw := foldSearchText(keyword)
	if kw != "" && strings.Contains(blob, kw) && (hasShopToken(blob) || hasFactoryToken(blob) || looksLikeOfficialBrand(blob)) {
		return true
	}
	return false
}

func shortVideoBusinessSignal(blob string) bool {
	if hasCompanyToken(blob) || hasShopToken(blob) || hasFactoryToken(blob) || looksLikeOfficialBrand(blob) {
		return true
	}
	return containsAnyToken(blob, []string{
		"批发", "厂家", "厂商", "企业", "经销", "进口商", "出口", "商贸", "实业", "旗舰",
		"wholesale", "importer", "exporter", "distributor", "supplier",
		"manufacturer", "enterprise", "brand", "wholesaler",
	})
}

func looksLikePersonalCreator(blob string) bool {
	return containsAnyToken(blob, []string{
		"vlog", "日常分享", "吃播", "搞笑", "探店日记", "个人号",
		"lifestyle", "dance challenge", "comedy", "asmr",
	})
}

func looksLikeBrandHandle(handle string) bool {
	h := strings.ToLower(strings.TrimSpace(handle))
	if len(h) < 5 {
		return false
	}
	if strings.HasPrefix(h, "ms4wljab") || strings.HasPrefix(h, "ms4w") {
		return false
	}
	letters := 0
	digits := 0
	for _, r := range h {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsDigit(r):
			digits++
		}
	}
	if letters == 0 || digits == len([]rune(h)) {
		return false
	}
	for _, tok := range []string{
		"official", "shop", "store", "factory", "tools", "light", "led",
		"brand", "mall", "trade", "supply", "wholesale", "import",
	} {
		if strings.Contains(h, tok) {
			return true
		}
	}
	return false
}
