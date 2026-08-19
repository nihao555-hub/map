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
	limit := flag.Int("limit", 50000, "最多补全/探测多少家店")
	workers := flag.Int("workers", 12, "并发抓取数")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	dir, err := engine.OpenDirectory(*db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开库失败: %v\n", err)
		os.Exit(1)
	}
	inv, err := dir.Inventory(ctx)
	_ = dir.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "统计失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("库存 db=%s merchants=%d gleif=%d osm=%d profiles=%d verified=%d osm_with_social=%d\n",
		*db, inv.Merchants, inv.GLEIF, inv.OSM, inv.Profiles, inv.VerifiedProfiles, inv.OSMWithSocial)

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

	dir, err = engine.OpenDirectory(*db)
	if err == nil {
		if inv, err := dir.Inventory(ctx); err == nil {
			fmt.Printf("补全后 merchants=%d gleif=%d osm=%d profiles=%d verified=%d osm_with_social=%d\n",
				inv.Merchants, inv.GLEIF, inv.OSM, inv.Profiles, inv.VerifiedProfiles, inv.OSMWithSocial)
		}
		_ = dir.Close()
	}
}
