package webrunner

import (
	"context"
	"sync/atomic"

	"github.com/gosom/google-maps-scraper/deduper"
)

// limitDeduper 在去重器上叠加数量上限：去重后的唯一商户数达到 max 后，
// 对任何新 key 都返回 false（视为已存在），搜索任务便不再播种新的详情任务，
// 从而实现「目标客户数量上限」。已达上限时搜索滚动本身仍会继续到自然结束，
// 但不会产生额外结果，抓满即止、不会截断已抓到的数据。
type limitDeduper struct {
	inner deduper.Deduper
	max   int64
	count int64
}

func newLimitDeduper(inner deduper.Deduper, max int) deduper.Deduper {
	return &limitDeduper{inner: inner, max: int64(max)}
}

func (d *limitDeduper) AddIfNotExists(ctx context.Context, key string) bool {
	if d.max > 0 && atomic.LoadInt64(&d.count) >= d.max {
		return false
	}

	if d.inner.AddIfNotExists(ctx, key) {
		atomic.AddInt64(&d.count, 1)
		return true
	}

	return false
}
