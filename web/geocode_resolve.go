package web

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
)

// knownCityCenters 离线城市中心点：Nominatim 超时/不可达时仍能开网格全量，避免退化成单点 ~20 条。
// 坐标为市中心近似值，实际覆盖范围由任务 Radius（米）决定。
var knownCityCenters = map[string]GeoPoint{
	"jakarta":          {Lat: -6.2088, Lon: 106.8456, CountryCode: "id", DisplayName: "Jakarta"},
	"jakarta pusat":    {Lat: -6.1865, Lon: 106.8341, CountryCode: "id", DisplayName: "Jakarta Pusat"},
	"jakarta selatan":  {Lat: -6.2615, Lon: 106.8106, CountryCode: "id", DisplayName: "Jakarta Selatan"},
	"jakarta barat":    {Lat: -6.1683, Lon: 106.7588, CountryCode: "id", DisplayName: "Jakarta Barat"},
	"jakarta utara":    {Lat: -6.1384, Lon: 106.8637, CountryCode: "id", DisplayName: "Jakarta Utara"},
	"jakarta timur":    {Lat: -6.2250, Lon: 106.9004, CountryCode: "id", DisplayName: "Jakarta Timur"},
	"menteng":          {Lat: -6.1944, Lon: 106.8294, CountryCode: "id", DisplayName: "Menteng, Jakarta"},
	"tanah abang":      {Lat: -6.2053, Lon: 106.8108, CountryCode: "id", DisplayName: "Tanah Abang, Jakarta"},
	"gambir":           {Lat: -6.1754, Lon: 106.8272, CountryCode: "id", DisplayName: "Gambir, Jakarta"},
	"surabaya":         {Lat: -7.2575, Lon: 112.7521, CountryCode: "id", DisplayName: "Surabaya"},
	"bandung":          {Lat: -6.9175, Lon: 107.6191, CountryCode: "id", DisplayName: "Bandung"},
	"medan":            {Lat: 3.5952, Lon: 98.6722, CountryCode: "id", DisplayName: "Medan"},
	"yogyakarta":       {Lat: -7.7956, Lon: 110.3695, CountryCode: "id", DisplayName: "Yogyakarta"},
	"bali":             {Lat: -8.4095, Lon: 115.1889, CountryCode: "id", DisplayName: "Bali"},
	"singapore":        {Lat: 1.3521, Lon: 103.8198, CountryCode: "sg", DisplayName: "Singapore"},
	"kuala lumpur":     {Lat: 3.1390, Lon: 101.6869, CountryCode: "my", DisplayName: "Kuala Lumpur"},
	"bangkok":          {Lat: 13.7563, Lon: 100.5018, CountryCode: "th", DisplayName: "Bangkok"},
	"ho chi minh city": {Lat: 10.8231, Lon: 106.6297, CountryCode: "vn", DisplayName: "Ho Chi Minh City"},
	"hanoi":            {Lat: 21.0278, Lon: 105.8342, CountryCode: "vn", DisplayName: "Hanoi"},
	"manila":           {Lat: 14.5995, Lon: 120.9842, CountryCode: "ph", DisplayName: "Manila"},
	"tokyo":            {Lat: 35.6762, Lon: 139.6503, CountryCode: "jp", DisplayName: "Tokyo"},
	"osaka":            {Lat: 34.6937, Lon: 135.5023, CountryCode: "jp", DisplayName: "Osaka"},
	"seoul":            {Lat: 37.5665, Lon: 126.9780, CountryCode: "kr", DisplayName: "Seoul"},
	"new york":         {Lat: 40.7128, Lon: -74.0060, CountryCode: "us", DisplayName: "New York"},
	"los angeles":      {Lat: 34.0522, Lon: -118.2437, CountryCode: "us", DisplayName: "Los Angeles"},
	"london":           {Lat: 51.5074, Lon: -0.1278, CountryCode: "gb", DisplayName: "London"},
	"paris":            {Lat: 48.8566, Lon: 2.3522, CountryCode: "fr", DisplayName: "Paris"},
	"sydney":           {Lat: -33.8688, Lon: 151.2093, CountryCode: "au", DisplayName: "Sydney"},
	"melbourne":        {Lat: -37.8136, Lon: 144.9631, CountryCode: "au", DisplayName: "Melbourne"},
	"dubai":            {Lat: 25.2048, Lon: 55.2708, CountryCode: "ae", DisplayName: "Dubai"},
	"shanghai":         {Lat: 31.2304, Lon: 121.4737, CountryCode: "cn", DisplayName: "Shanghai"},
	"beijing":          {Lat: 39.9042, Lon: 116.4074, CountryCode: "cn", DisplayName: "Beijing"},
	"hong kong":        {Lat: 22.3193, Lon: 114.1694, CountryCode: "hk", DisplayName: "Hong Kong"},
	"taipei":           {Lat: 25.0330, Lon: 121.5654, CountryCode: "tw", DisplayName: "Taipei"},
}

// ResolveLocationAnchor 解析地点为可开网格的锚点。
// 顺序：离线城市表（词典命中）→ Nominatim 原文/英文 → 离线模糊回退。
// 失败时返回 error；成功时保证 Lat/Lon 可用，并尽量填满包围盒。
func ResolveLocationAnchor(ctx context.Context, query, countryCode string) (GeoPoint, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return GeoPoint{}, fmt.Errorf("empty query")
	}
	cc := strings.ToLower(strings.TrimSpace(countryCode))

	tryQueries := []string{query}
	if en, ok := zhPlaceLexicon[query]; ok && !strings.EqualFold(en, query) {
		tryQueries = append(tryQueries, en)
	} else if en, ok := fuzzyPlaceLexicon(query); ok && !strings.EqualFold(en, query) {
		tryQueries = append(tryQueries, en)
	}
	// 已是英文时也试「City, Country」提高命中
	if cc != "" && !containsChinese(query) {
		tryQueries = append(tryQueries, query+", "+cc)
	}

	// 常见中文/英文城市：优先离线，避免 Nominatim 超时把全量网格打成单点 ~20
	if point, ok := lookupKnownCity(query, tryQueries...); ok {
		if cc != "" {
			point.CountryCode = cc
		}
		ensurePointBBox(&point, 10)
		log.Printf("location resolved via offline city table q=%q -> %.4f,%.4f", query, point.Lat, point.Lon)
		return point, nil
	}

	var lastErr error
	for _, q := range tryQueries {
		point, err := GeocodeInCountry(ctx, q, "en", cc)
		if err != nil {
			lastErr = err
			continue
		}
		ensurePointBBox(&point, 10)
		if cc != "" && point.CountryCode == "" {
			point.CountryCode = cc
		}
		log.Printf("location resolved via nominatim q=%q -> %.4f,%.4f", q, point.Lat, point.Lon)
		return point, nil
	}

	if lastErr != nil {
		return GeoPoint{}, fmt.Errorf("resolve %q: %w", query, lastErr)
	}
	return GeoPoint{}, fmt.Errorf("resolve %q: no geocode result", query)
}

func lookupKnownCity(primary string, alts ...string) (GeoPoint, bool) {
	cands := append([]string{primary}, alts...)
	for _, c := range cands {
		key := strings.ToLower(strings.TrimSpace(c))
		key = strings.TrimSuffix(key, ", indonesia")
		key = strings.TrimSuffix(key, ", id")
		if p, ok := knownCityCenters[key]; ok {
			return p, true
		}
		// 词典中文 → 英文后再查
		if en, ok := zhPlaceLexicon[strings.TrimSpace(c)]; ok {
			if p, ok2 := knownCityCenters[strings.ToLower(en)]; ok2 {
				return p, true
			}
		}
		if en, ok := fuzzyPlaceLexicon(c); ok {
			if p, ok2 := knownCityCenters[strings.ToLower(en)]; ok2 {
				return p, true
			}
		}
		for _, part := range strings.Split(key, ",") {
			part = strings.TrimSpace(part)
			if p, ok := knownCityCenters[part]; ok {
				return p, true
			}
		}
		// Compound locations such as "Menteng, Jakarta" and "central New York"
		// should still use a safe offline anchor when the external geocoder is
		// slow. Prefer the longest matching key so districts beat parent cities.
		bestKey := ""
		for known := range knownCityCenters {
			if len(known) <= len(bestKey) {
				continue
			}
			if strings.Contains(key, known) {
				bestKey = known
			}
		}
		if bestKey != "" {
			return knownCityCenters[bestKey], true
		}
	}
	return GeoPoint{}, false
}

func ensurePointBBox(p *GeoPoint, halfKm float64) {
	if p == nil {
		return
	}
	if p.MinLat != 0 || p.MaxLat != 0 || p.MinLon != 0 || p.MaxLon != 0 {
		return
	}
	if halfKm <= 0 {
		halfKm = 10
	}
	latPad := halfKm / 111.0
	lonPad := latPad
	if c := math.Cos(p.Lat * math.Pi / 180); c > 0.01 {
		lonPad = halfKm / (111.0 * c)
	}
	p.MinLat = p.Lat - latPad
	p.MaxLat = p.Lat + latPad
	p.MinLon = p.Lon - lonPad
	p.MaxLon = p.Lon + lonPad
}
