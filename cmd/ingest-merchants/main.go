package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gosom/google-maps-scraper/engine"
)

func main() {
	db := flag.String("db", engine.DefaultMerchantDB, "SQLite 路径")
	gleifZip := flag.String("gleif-zip", "", "已下载的 GLEIF Golden Copy csv.zip；空则自动下载")
	gleifLimit := flag.Int("gleif-limit", 0, "GLEIF 最多导入条数，0=全量")
	skipGLEIF := flag.Bool("skip-gleif", false, "跳过 GLEIF")
	skipOSM := flag.Bool("skip-osm", false, "跳过 OSM 全品类城市店铺")
	skipWikidata := flag.Bool("skip-wikidata", false, "跳过 Wikidata 东南亚公司")
	leiSocials := flag.Bool("lei-socials", false, "只用 Wikidata LEI 给已入库的 GLEIF 补官网/社媒")
	publicSocials := flag.Bool("public-socials", false, "用 ROR / Wikidata 官网 / 同名同国已验证主页给 GLEIF 补主页")
	maxPublic := flag.Bool("public-max", false, "把剩下的公开源用尽：Wikidata 全量社媒、OSM contact:*、GLEIF 父子继承")
	rrOnly := flag.Bool("rr-only", false, "只做同名同国对拷 + GLEIF Level 2 父子继承")
	moreSocials := flag.Bool("more-socials", false, "补还没跑过的公开源：拆开 OSM 框、Wikidata 母公司、SEC 官网、已有官网刮社媒、Sherlock 同名探测")
	attachOnly := flag.Bool("attach-only", false, "只做 ROR/P856/同名对拷，不再拉 Wikidata 国家公司")
	rorZip := flag.String("ror-zip", "", "已下载的 ROR dump zip；空则自动下载")
	rrZip := flag.String("rr-zip", "", "已下载的 GLEIF Relationship csv.zip；空则自动下载")
	websiteLimit := flag.Int("website-limit", 0, "官网刮社媒最多抓多少家，0=默认 30000")
	websiteWorkers := flag.Int("website-workers", 16, "官网刮社媒并发数")
	sherlock := flag.Bool("sherlock", false, "用 Sherlock 站点表：官网/已有 handle 探姐妹社媒，独特法律名需 LinkedIn 公司页或双平台")
	sherlockLimit := flag.Int("sherlock-limit", 0, "Sherlock 已有官网/社媒最多探多少家，0=默认 15000")
	sherlockNameLimit := flag.Int("sherlock-names", 0, "Sherlock 独特法律名最多探多少家，0=默认 8000")
	sherlockWorkers := flag.Int("sherlock-workers", 12, "Sherlock 探测并发")
	attachSocials := flag.Bool("attach-socials", false, "全库缺社媒：官网刮取 + OSM contact + 店名检索 FB/IG/LI + Sherlock 姐妹页，可断点续跑")
	attachLimit := flag.Int("attach-limit", 0, "全库补社媒最多处理多少家，0=全部还没探过的")
	attachWorkers := flag.Int("attach-workers", 10, "全库补社媒并发")
	sea := flag.Bool("sea", false, "只补东南亚城市店铺 + Wikidata 公司")
	osmLimit := flag.Int("osm-limit", 2000, "每个城市最多拉多少家店")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := engine.OptionsFromEnv()
	if client.HTTP != nil {
		client.HTTP.Timeout = 90 * time.Second
		if *publicSocials || *maxPublic || *moreSocials || *sherlock || *attachSocials {
			client.HTTP.Timeout = 180 * time.Second
		}
	}
	opt := engine.IngestOptions{
		DBPath:            *db,
		GLEIFZip:          *gleifZip,
		GLEIFLimit:        *gleifLimit,
		SkipGLEIF:         *skipGLEIF,
		SkipOSM:           *skipOSM,
		SkipWikidata:      *skipWikidata,
		PublicSocials:     *publicSocials,
		MaxPublic:         *maxPublic,
		RROnly:            *rrOnly,
		MoreSocials:       *moreSocials,
		AttachOnly:        *attachOnly,
		RORZip:            *rorZip,
		RRZip:             *rrZip,
		WebsiteLimit:      *websiteLimit,
		WebsiteWorkers:    *websiteWorkers,
		Sherlock:          *sherlock,
		SherlockLimit:     *sherlockLimit,
		SherlockNameLimit: *sherlockNameLimit,
		SherlockWorkers:   *sherlockWorkers,
		AttachSocials:     *attachSocials,
		AttachLimit:       *attachLimit,
		AttachWorkers:     *attachWorkers,
		Overpass:          !*skipOSM,
		OSMLimitPerCity:   *osmLimit,
	}
	if *sea {
		opt.SkipGLEIF = true
		opt.OSMBoxes = engine.SEAIngestBoxes()
		if *osmLimit == 2000 {
			opt.OSMLimitPerCity = 2000
		}
	}
	if *leiSocials {
		opt.SkipGLEIF = true
		opt.SkipOSM = true
		opt.Overpass = false
		opt.SkipWikidata = true
		opt.WikidataLEI = true
	}
	if *publicSocials {
		opt.SkipGLEIF = true
		opt.SkipOSM = true
		opt.Overpass = false
		opt.SkipWikidata = true
		opt.PublicSocials = true
		opt.AttachOnly = *attachOnly
	}
	if *maxPublic {
		opt.SkipGLEIF = true
		opt.Overpass = false
		opt.SkipWikidata = true
		opt.PublicSocials = false
		opt.MaxPublic = true
		if strings.Contains(client.WikidataURL, "query.wikidata.org") {
			client.WikidataURL = engine.QleverWikidataSPARQL
		}
	}
	if *rrOnly {
		opt.SkipGLEIF = true
		opt.SkipOSM = true
		opt.Overpass = false
		opt.SkipWikidata = true
		opt.RROnly = true
	}
	if *sherlock {
		opt.SkipGLEIF = true
		opt.SkipOSM = true
		opt.Overpass = false
		opt.SkipWikidata = true
		opt.PublicSocials = false
		opt.MaxPublic = false
		opt.MoreSocials = false
		opt.Sherlock = true
	}
	if *attachSocials {
		opt.SkipGLEIF = true
		opt.SkipOSM = true
		opt.Overpass = false
		opt.SkipWikidata = true
		opt.PublicSocials = false
		opt.MaxPublic = false
		opt.MoreSocials = false
		opt.Sherlock = false
		opt.AttachSocials = true
	}
	if *moreSocials {
		opt.SkipGLEIF = true
		opt.Overpass = false
		opt.SkipWikidata = true
		opt.PublicSocials = false
		opt.MaxPublic = false
		opt.MoreSocials = true
		if strings.Contains(client.WikidataURL, "query.wikidata.org") {
			client.WikidataURL = engine.QleverWikidataSPARQL
		}
	}
	started := time.Now()
	fmt.Printf("开始入库 db=%s sea=%v cities=%d\n", *db, *sea, len(opt.OSMBoxes))
	stats, err := client.IngestMerchants(ctx, opt)
	elapsed := time.Since(started)
	total := 0
	for _, st := range stats {
		fmt.Println(st.String())
		total += st.Rows
	}
	fmt.Printf("合计: %d 条 / %s\n", total, elapsed.Round(time.Millisecond))
	if err != nil {
		fmt.Fprintf(os.Stderr, "入库失败: %v\n", err)
		os.Exit(1)
	}
}
