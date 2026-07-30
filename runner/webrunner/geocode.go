package webrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gosom/google-maps-scraper/grid"
)

// geocodeResult 是 Nominatim 搜索结果
type geocodeResult struct {
	PlaceID     int    `json:"place_id"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
	DisplayName string `json:"display_name"`
	BoundingBox []string `json:"boundingbox"` // [minLat, maxLat, minLon, maxLon]
}

// geocode 用 Nominatim (OpenStreetMap) 把地点名转成经纬度范围
// 免费，不需要 API key
func geocode(ctx context.Context, query string) (grid.BoundingBox, error) {
	if query == "" {
		return grid.BoundingBox{}, fmt.Errorf("empty query")
	}

	// 构造 Nominatim 搜索 URL
	apiURL := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=1&addressdetails=0",
		url.QueryEscape(query),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return grid.BoundingBox{}, err
	}

	// Nominatim 要求设置 User-Agent
	req.Header.Set("User-Agent", "google-maps-scraper/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return grid.BoundingBox{}, fmt.Errorf("geocode request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return grid.BoundingBox{}, fmt.Errorf("geocode returned status %d", resp.StatusCode)
	}

	var results []geocodeResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return grid.BoundingBox{}, fmt.Errorf("geocode decode failed: %w", err)
	}

	if len(results) == 0 {
		return grid.BoundingBox{}, fmt.Errorf("no results found for %q", query)
	}

	bboxStr := results[0].BoundingBox
	if len(bboxStr) != 4 {
		return grid.BoundingBox{}, fmt.Errorf("invalid bounding box from geocode")
	}

	// Nominatim 返回顺序: [minLat, maxLat, minLon, maxLon]
	minLat, err := strconv.ParseFloat(bboxStr[0], 64)
	if err != nil {
		return grid.BoundingBox{}, err
	}
	maxLat, err := strconv.ParseFloat(bboxStr[1], 64)
	if err != nil {
		return grid.BoundingBox{}, err
	}
	minLon, err := strconv.ParseFloat(bboxStr[2], 64)
	if err != nil {
		return grid.BoundingBox{}, err
	}
	maxLon, err := strconv.ParseFloat(bboxStr[3], 64)
	if err != nil {
		return grid.BoundingBox{}, err
	}

	return grid.BoundingBox{
		MinLat: minLat,
		MinLon: minLon,
		MaxLat: maxLat,
		MaxLon: maxLon,
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
