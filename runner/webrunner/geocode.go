package webrunner

import (
	"context"
	"fmt"
	"math"

	"github.com/gosom/google-maps-scraper/grid"
	"github.com/gosom/google-maps-scraper/web"
)

// geocode 用 Nominatim / 词典 / 离线城市表把地点名转成经纬度范围。
// 实现复用 web.ResolveLocationAnchor，避免中文地名超时后退化成单点 ~20 条。
func geocode(ctx context.Context, query string) (grid.BoundingBox, error) {
	return geocodeInCountry(ctx, query, "")
}

func geocodeInCountry(ctx context.Context, query, countryCode string) (grid.BoundingBox, error) {
	point, err := web.ResolveLocationAnchor(ctx, query, countryCode)
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

// anchorBBox 以地图选点（经纬度锚点）为中心构造网格范围。
// 完全不依赖外部地理编码服务，离线可用、零延迟。
// halfKm 为中心到边界的公里数，<=0 时默认 5km（即约 10km×10km 全覆盖）。
func anchorBBox(lat, lon, halfKm float64) grid.BoundingBox {
	if halfKm <= 0 {
		halfKm = 5
	}

	latPad := halfKm / 111.0 // 纬度 1° ≈ 111km
	lonPad := latPad
	if c := math.Cos(lat * math.Pi / 180); c > 0.01 {
		lonPad = halfKm / (111.0 * c)
	}

	return grid.BoundingBox{
		MinLat: lat - latPad,
		MaxLat: lat + latPad,
		MinLon: lon - lonPad,
		MaxLon: lon + lonPad,
	}
}
