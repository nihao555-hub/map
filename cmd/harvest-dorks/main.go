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
	workers := flag.Int("workers", 6, "同时跑多少个品类×国家")
	queryLimit := flag.Int("query-limit", 0, "每个品类×国家最多几条公式，0=全部")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	opt := engine.HarvestOptions{
		DBPath:     *db,
		Workers:    *workers,
		QueryLimit: *queryLimit,
		Role:       engine.RoleBuyer,
	}
	if s := strings.TrimSpace(*keywords); s != "" {
		opt.Keywords = splitCSV(s)
	}
	if s := strings.TrimSpace(*countries); s != "" {
		opt.Countries = splitCSV(s)
	}

	client := engine.OptionsFromEnv()
	if client.HTTP != nil {
		client.HTTP.Timeout = 25 * time.Second
	}
	fmt.Printf("开始批量公式收割 db=%s keywords=%v countries=%v\n", *db, firstOr(opt.Keywords, engine.DefaultHarvestKeywords), firstOr(opt.Countries, engine.DefaultHarvestCountries))
	st, err := client.HarvestTradeDorks(ctx, opt)
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
