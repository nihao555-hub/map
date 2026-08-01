package webrunner

import (
	"context"
	"fmt"

	"github.com/gosom/google-maps-scraper/grid"
	"github.com/gosom/google-maps-scraper/web"
)

// geocode 用 Nominatim (OpenStreetMap) 把地点名转成经纬度范围
// 免费，不需要 API key。实现复用 web.Geocode，与普通模式的地理锚定共用
func geocode(ctx context.Context, query string) (grid.BoundingBox, error) {
	point, err := web.Geocode(ctx, query)
	if err != nil {
		return grid.BoundingBox{}, err
	}

	if point.MinLat == 0 && point.MaxLat == 0 && point.MinLon == 0 && point.MaxLon == 0 {
		return grid.BoundingBox{}, fmt.Errorf("invalid bounding box from geocode")
	}

	return grid.BoundingBox{
		MinLat: point.MinLat,
		MinLon: point.MinLon,
		MaxLat: point.MaxLat,
		MaxLon: point.MaxLon,
	}, nil
}

// expandBBox 把 bbox 向外扩展一定比例，确保覆盖完整区域
func expandBBox(bbox grid.BoundingBox, ratio float64) grid.BoundingBox {
	latPad := (bbox.MaxLat - bbox.MinLat) * ratio
	lonPad := (bbox.MaxLon - bbox.MinLon) * ratio

	return grid.BoundingBox{
		MinLat: bbox.MinLat - latPad,
		MaxLat: bbox.MaxLat + latPad,
		MinLon: bbox.MinLon - lonPad,
		MaxLon: bbox.MaxLon + lonPad,
	}
}
