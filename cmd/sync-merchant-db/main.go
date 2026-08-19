package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosom/google-maps-scraper/engine"
	"github.com/gosom/google-maps-scraper/s3uploader"

	_ "modernc.org/sqlite"
)

func main() {
	db := flag.String("db", engine.DefaultMerchantDB, "本地 SQLite 路径")
	upload := flag.Bool("upload", false, "把本地库上传到 OSS/S3")
	download := flag.Bool("download", false, "从 OSS/S3 拉库到本地")
	bucket := flag.String("bucket", "", "桶名，默认 ENGINE_OSS_BUCKET / OSS_BUCKET")
	key := flag.String("key", "", "对象键，默认 ENGINE_OSS_KEY 或 engine/merchants.db")
	flag.Parse()

	if *upload == *download {
		fmt.Fprintln(os.Stderr, "请指定 -upload 或 -download 之一")
		os.Exit(2)
	}

	opt := s3uploader.OptionsFromEnv()
	if strings.TrimSpace(*bucket) != "" {
		opt.Bucket = strings.TrimSpace(*bucket)
	}
	if strings.TrimSpace(*key) != "" {
		opt.Key = strings.TrimSpace(*key)
	}
	if !opt.Ready() {
		fmt.Fprintln(os.Stderr, "缺少 OSS 配置：ENGINE_OSS_ACCESS_KEY / ENGINE_OSS_SECRET_KEY / ENGINE_OSS_BUCKET，可选 ENGINE_OSS_ENDPOINT（阿里云如 oss-cn-hangzhou.aliyuncs.com）")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	started := time.Now()
	var err error
	if *upload {
		err = uploadDB(ctx, opt, *db)
	} else {
		err = downloadDB(ctx, opt, *db)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "同步失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ok db=%s oss=%s/%s %s\n", *db, opt.Bucket, opt.Key, time.Since(started).Round(time.Millisecond))
}

func uploadDB(ctx context.Context, opt s3uploader.Options, dbPath string) error {
	dbPath = strings.TrimSpace(dbPath)
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("本地库: %w", err)
	}
	snap, err := snapshotSQLite(ctx, dbPath)
	if err != nil {
		return err
	}
	defer os.Remove(snap)
	st, err := os.Stat(snap)
	if err != nil {
		return err
	}
	fmt.Printf("上传 %s (%d bytes) -> %s/%s\n", snap, st.Size(), opt.Bucket, opt.Key)
	return s3uploader.UploadLocalFile(ctx, opt, snap)
}

func downloadDB(ctx context.Context, opt s3uploader.Options, dbPath string) error {
	dbPath = strings.TrimSpace(dbPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil && filepath.Dir(dbPath) != "." {
		return err
	}
	tmp := dbPath + ".part"
	n, err := s3uploader.DownloadToFile(ctx, opt, tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	fmt.Printf("下载 %s/%s (%d bytes) -> %s\n", opt.Bucket, opt.Key, n, dbPath)
	return os.Rename(tmp, dbPath)
}

func snapshotSQLite(ctx context.Context, src string) (string, error) {
	dir := filepath.Dir(src)
	dst := filepath.Join(dir, fmt.Sprintf(".merchants-oss-%d.db", time.Now().UnixNano()))
	db, err := sql.Open("sqlite", src+"?_pragma=busy_timeout(30000)")
	if err != nil {
		return "", err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		_ = os.Remove(dst)
		return "", fmt.Errorf("快照本地库: %w", err)
	}
	return dst, nil
}
