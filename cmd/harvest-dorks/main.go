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
	keywords := flag.String("keywords", "", "逗号分隔品类；空则用内置外贸清单")
	countries := flag.String("countries", "", "逗号分隔国家码，空表示不限+东南亚/美/德")
	workers := flag.Int("workers", 0, "并发数；0 用引擎默认（sidecar=2，fast=8）")
	queryLimit := flag.Int("query-limit", 0, "品类模式：每个品类×国家最多几条公式；公司名模式：最多查多少家")
	names := flag.Bool("names", false, "按已入库 GLEIF 公司全名搜社媒（比按品类扫更准）")
	shortVideo := flag.Bool("short-video", false, "只收抖音/TikTok 企业号主页（TikTok-Api / f2 + 公开索引）")
	fast := flag.Bool("fast", false, "两小时窗口：先灌 Wikidata 全量 TikTok/抖音号，再按东南亚→中东→欧美扫")
	seedOnly := flag.Bool("seed-only", false, "只灌 Wikidata 已标注的 TikTok/抖音号，不跑公开检索")
	sidecar := flag.Bool("sidecar", false, "用 TikTok-Api/f2 按词搜用户，默认东南亚，跳过公开检索")
	regions := flag.String("regions", "", "sea,me,west；空则 sidecar 默认 sea，fast 默认 sea,me,west")
	deadline := flag.Duration("deadline", 0, "最长跑多久，例如 110m")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	opt := engine.HarvestOptions{
		DBPath:     *db,
		Workers:    *workers,
		QueryLimit: *queryLimit,
		Role:       engine.RoleBuyer,
		Fast:       *fast,
		SeedOnly:   *seedOnly,
		Sidecar:    *sidecar,
		Deadline:   *deadline,
	}
	if s := strings.TrimSpace(*regions); s != "" {
		opt.Regions = splitCSV(s)
	}
	if s := strings.TrimSpace(*keywords); s != "" {
		opt.Keywords = splitCSV(s)
	}
	if s := strings.TrimSpace(*countries); s != "" {
		opt.Countries = splitCSV(s)
	}

	client := engine.OptionsFromEnv()
	if client.HTTP != nil {
		if *sidecar {
			client.HTTP.Timeout = 90 * time.Second
		} else {
			client.HTTP.Timeout = 25 * time.Second
		}
	}
	var (
		st  engine.HarvestStats
		err error
	)
	if *shortVideo {
		if (*fast || *seedOnly) && (client.WikidataURL == "" || strings.Contains(client.WikidataURL, "query.wikidata.org")) {
			client.WikidataURL = engine.QleverWikidataSPARQL
		}
		fmt.Printf("开始抖音/TikTok 企业号收割 fast=%v seed-only=%v sidecar=%v db=%s\n", *fast, *seedOnly, *sidecar, *db)
		st, err = client.HarvestShortVideo(ctx, opt)
	} else if *names {
		fmt.Printf("开始公司名公式收割 db=%s limit=%d\n", *db, *queryLimit)
		st, err = client.HarvestNameDorks(ctx, opt)
	} else {
		fmt.Printf("开始批量公式收割 db=%s keywords=%v countries=%v\n", *db, firstOr(opt.Keywords, engine.DefaultHarvestKeywords), firstOr(opt.Countries, engine.DefaultHarvestCountries))
		st, err = client.HarvestTradeDorks(ctx, opt)
	}
	fmt.Println(st.String())
	if len(st.ByPlat) > 0 {
		fmt.Printf("按平台: %v\n", st.ByPlat)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "收割失败: %v\n", err)
		os.Exit(1)
	}
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstOr(got, fallback []string) []string {
	if len(got) == 0 {
		return fallback
	}
	return got
}
