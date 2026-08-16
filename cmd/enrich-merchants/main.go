package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/gosom/google-maps-scraper/engine"
)

func main() {
	db := flag.String("db", engine.DefaultMerchantDB, "SQLite 路径")
	limit := flag.Int("limit", 20000, "最多补全多少家有官网的店")
	workers := flag.Int("workers", 8, "并发抓取数")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := engine.OptionsFromEnv()
	if client.HTTP != nil {
		client.HTTP.Timeout = 20 * time.Second
	}
	fmt.Printf("开始补全已验证社媒 db=%s limit=%d workers=%d\n", *db, *limit, *workers)
	started := time.Now()
	st, err := client.EnrichMerchants(ctx, engine.EnrichOptions{
		DBPath:  *db,
		Limit:   *limit,
		Workers: *workers,
	})
	fmt.Println(st.String())
	fmt.Printf("墙钟 %s\n", time.Since(started).Round(time.Millisecond))
	if err != nil {
		fmt.Fprintf(os.Stderr, "补全失败: %v\n", err)
		os.Exit(1)
	}
}
