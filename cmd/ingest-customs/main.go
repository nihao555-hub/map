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
	db := flag.String("db", engine.DefaultCustomsDB, "海关 SQLite 路径")
	year := flag.Int("year", 0, "年份，0=当前年")
	skipUS := flag.Bool("skip-us", false, "跳过美国 Kirchner 海运提单抽样")
	skipUK := flag.Bool("skip-uk", false, "跳过英国 HMRC uktradeinfo 批量")
	skipComtrade := flag.Bool("skip-comtrade", false, "跳过联合国 Comtrade 国家口径")
	usLimit := flag.Int("us-limit", 200, "每个关键词 Kirchner 最多返回多少家（API 上限 200）")
	keywords := flag.String("keywords", "", "美国提单关键词，逗号分隔；空=内置外贸品类+HS")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := engine.OptionsFromEnv()
	if client.HTTP != nil {
		client.HTTP.Timeout = 120 * time.Second
	}
	opt := engine.DefaultCustomsIngestOptions()
	opt.DBPath = *db
	if *year > 0 {
		opt.Year = *year
	}
	opt.SkipUS = *skipUS
	opt.SkipUK = *skipUK
	opt.SkipComtrade = *skipComtrade
	opt.USLimit = *usLimit
	if s := strings.TrimSpace(*keywords); s != "" {
		opt.USKeywords = splitCSV(s)
	}

	fmt.Printf("开始公开海关入库 db=%s year=%d skip_us=%v skip_uk=%v skip_comtrade=%v\n",
		opt.DBPath, opt.Year, opt.SkipUS, opt.SkipUK, opt.SkipComtrade)
	started := time.Now()
	stats, err := client.IngestCustomsPublic(ctx, opt)
	for _, st := range stats {
		fmt.Println(st.String())
	}
	store, openErr := engine.OpenCustomsStore(opt.DBPath)
	if openErr == nil {
		if counts, cerr := store.Counts(ctx); cerr == nil {
			fmt.Printf("库内计数: %v\n", counts)
		}
		_ = store.Close()
	}
	fmt.Printf("合计耗时: %s\n", time.Since(started).Round(time.Millisecond))
	if err != nil {
		fmt.Fprintf(os.Stderr, "海关入库失败: %v\n", err)
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
