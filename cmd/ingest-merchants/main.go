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
	gleifZip := flag.String("gleif-zip", "", "已下载的 GLEIF Golden Copy csv.zip；空则自动下载")
	gleifLimit := flag.Int("gleif-limit", 0, "GLEIF 最多导入条数，0=全量")
	skipGLEIF := flag.Bool("skip-gleif", false, "跳过 GLEIF")
	skipOSM := flag.Bool("skip-osm", false, "跳过 OSM 全品类城市店铺")
	osmLimit := flag.Int("osm-limit", 1500, "每个城市最多拉多少家店")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	client := engine.OptionsFromEnv()
	if client.HTTP != nil {
		client.HTTP.Timeout = 90 * time.Second
	}
	started := time.Now()
	fmt.Printf("开始全量入库 db=%s\n", *db)
	stats, err := client.IngestMerchants(ctx, engine.IngestOptions{
		DBPath:          *db,
		GLEIFZip:        *gleifZip,
		GLEIFLimit:      *gleifLimit,
		SkipGLEIF:       *skipGLEIF,
		SkipOSM:         *skipOSM,
		Overpass:        !*skipOSM,
		OSMLimitPerCity: *osmLimit,
	})
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
