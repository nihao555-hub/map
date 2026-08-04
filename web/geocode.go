package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// nominatimSearchURL 是 Nominatim (OpenStreetMap) 搜索接口，免费、不需要 API key。
// 声明为变量方便测试时替换
var nominatimSearchURL = "https://nominatim.openstreetmap.org/search"

// nominatimReverseURL 逆地理编码：经纬度 → 地址/国家
var nominatimReverseURL = "https://nominatim.openstreetmap.org/reverse"

// GeoPoint 是一次地理编码的结果
type GeoPoint struct {
	Lat         float64
	Lon         float64
	CountryCode string // ISO 3166-1 alpha-2 小写国家代码（可能为空）
	DisplayName string // Nominatim 展示名（可用 accept-language 控制）
	// 包围盒（Nominatim 返回顺序: minLat, maxLat, minLon, maxLon），网格模式用
	MinLat float64
	MaxLat float64
	MinLon float64
	MaxLon float64
}

// nominatimResult 是 Nominatim 搜索结果
type nominatimResult struct {
	Lat         string   `json:"lat"`
	Lon         string   `json:"lon"`
	DisplayName string   `json:"display_name"`
	BoundingBox []string `json:"boundingbox"` // [minLat, maxLat, minLon, maxLon]
	Address     struct {
		CountryCode string `json:"country_code"`
	} `json:"address"`
}

// Geocode 用 Nominatim 把地点名解析成经纬度和国家代码。
// 网格模式（runner/webrunner）与普通模式的地理锚定共用这一个实现。
func Geocode(ctx context.Context, query string) (GeoPoint, error) {
	return GeocodeLang(ctx, query, "")
}

// GeocodeLang 同 Geocode，可指定 accept-language（如 en）以拿到英文地名供海外搜索。
func GeocodeLang(ctx context.Context, query, acceptLang string) (GeoPoint, error) {
	return GeocodeInCountry(ctx, query, acceptLang, "")
}

// GeocodeInCountry 同 GeocodeLang，可用 ISO2 countrycodes 把结果锚定到目标国（如 id/th）。
func GeocodeInCountry(ctx context.Context, query, acceptLang, countryCode string) (GeoPoint, error) {
	if strings.TrimSpace(query) == "" {
		return GeoPoint{}, fmt.Errorf("empty query")
	}

	apiURL := fmt.Sprintf(
		"%s?q=%s&format=json&limit=1&addressdetails=1",
		nominatimSearchURL,
		url.QueryEscape(query),
	)
	if lang := strings.TrimSpace(acceptLang); lang != "" {
		apiURL += "&accept-language=" + url.QueryEscape(lang)
	}
	if cc := strings.ToLower(strings.TrimSpace(countryCode)); cc != "" && len(cc) == 2 {
		apiURL += "&countrycodes=" + url.QueryEscape(cc)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return GeoPoint{}, err
	}

	// Nominatim 要求设置 User-Agent
	req.Header.Set("User-Agent", "google-maps-scraper/1.0")
	if lang := strings.TrimSpace(acceptLang); lang != "" {
		req.Header.Set("Accept-Language", lang)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return GeoPoint{}, fmt.Errorf("geocode request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return GeoPoint{}, fmt.Errorf("geocode returned status %d", resp.StatusCode)
	}

	var results []nominatimResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return GeoPoint{}, fmt.Errorf("geocode decode failed: %w", err)
	}

	if len(results) == 0 {
		return GeoPoint{}, fmt.Errorf("no results found for %q", query)
	}

	return results[0].geoPoint()
}

// ReverseGeocode 把经纬度解析成可读地址与国家代码（地图点选同步左侧表单用）
func ReverseGeocode(ctx context.Context, lat, lon float64) (GeoPoint, error) {
	apiURL := fmt.Sprintf(
		"%s?lat=%s&lon=%s&format=jsonv2&addressdetails=1&accept-language=zh",
		nominatimReverseURL,
		url.QueryEscape(strconv.FormatFloat(lat, 'f', 6, 64)),
		url.QueryEscape(strconv.FormatFloat(lon, 'f', 6, 64)),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return GeoPoint{}, err
	}

	req.Header.Set("User-Agent", "google-maps-scraper/1.0")
	req.Header.Set("Accept-Language", "zh")

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return GeoPoint{}, fmt.Errorf("reverse geocode request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return GeoPoint{}, fmt.Errorf("reverse geocode returned status %d", resp.StatusCode)
	}

	var result nominatimResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return GeoPoint{}, fmt.Errorf("reverse geocode decode failed: %w", err)
	}

	// reverse 接口不返回 lat/lon 时用请求坐标
	if strings.TrimSpace(result.Lat) == "" {
		result.Lat = strconv.FormatFloat(lat, 'f', 6, 64)
	}
	if strings.TrimSpace(result.Lon) == "" {
		result.Lon = strconv.FormatFloat(lon, 'f', 6, 64)
	}

	return result.geoPoint()
}

func (r *nominatimResult) geoPoint() (GeoPoint, error) {
	lat, err := strconv.ParseFloat(r.Lat, 64)
	if err != nil {
		return GeoPoint{}, fmt.Errorf("invalid lat from geocode: %w", err)
	}

	lon, err := strconv.ParseFloat(r.Lon, 64)
	if err != nil {
		return GeoPoint{}, fmt.Errorf("invalid lon from geocode: %w", err)
	}

	point := GeoPoint{
		Lat:         lat,
		Lon:         lon,
		CountryCode: strings.ToLower(r.Address.CountryCode),
		DisplayName: strings.TrimSpace(r.DisplayName),
	}

	// 包围盒是可选的，解析失败不视为错误
	if len(r.BoundingBox) == 4 {
		point.MinLat, _ = strconv.ParseFloat(r.BoundingBox[0], 64)
		point.MaxLat, _ = strconv.ParseFloat(r.BoundingBox[1], 64)
		point.MinLon, _ = strconv.ParseFloat(r.BoundingBox[2], 64)
		point.MaxLon, _ = strconv.ParseFloat(r.BoundingBox[3], 64)
	}

	return point, nil
}

// shortDisplayName 从 Nominatim 长展示名取前两段，适合拼进 Maps 查询
func shortDisplayName(display string) string {
	display = strings.TrimSpace(display)
	if display == "" {
		return ""
	}

	parts := strings.Split(display, ",")
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0])
	}

	a := strings.TrimSpace(parts[0])
	b := strings.TrimSpace(parts[1])
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}

	return a + ", " + b
}

// hasGeoAnchor 判断 lat/lon 是否是有效的锚定坐标。
// 表单默认值是 "0"/"0"（几内亚湾），视为未锚定
func hasGeoAnchor(latStr, lonStr string) bool {
	lat, err := strconv.ParseFloat(strings.TrimSpace(latStr), 64)
	if err != nil {
		return false
	}

	lon, err := strconv.ParseFloat(strings.TrimSpace(lonStr), 64)
	if err != nil {
		return false
	}

	return lat != 0 || lon != 0
}

// countryLang 国家代码 -> Google hl 语言参数（均为 2 字符，满足 JobData 校验）。
// 只覆盖常见国家，未覆盖的返回空串（保留用户选择的语言）
var countryLang = map[string]string{
	"cn": "zh", "hk": "zh", "tw": "zh", "mo": "zh",
	"jp": "ja", "kr": "ko", "th": "th", "vn": "vi",
	"id": "id", "my": "ms", "sg": "en", "ph": "en", "in": "en",
	"kh": "km", "la": "lo", "mm": "my", "bn": "ms",
	"us": "en", "gb": "en", "au": "en", "nz": "en", "ca": "en",
	"de": "de", "at": "de", "ch": "de", "fr": "fr", "es": "es",
	"it": "it", "pt": "pt", "br": "pt", "ru": "ru", "ua": "uk",
	"tr": "tr", "nl": "nl", "pl": "pl", "se": "sv", "ae": "ar", "sa": "ar",
}

// langForCountryCode 返回与目标地国家匹配的 hl 语言参数，未知国家返回空串
func langForCountryCode(countryCode string) string {
	return countryLang[strings.ToLower(countryCode)]
}
