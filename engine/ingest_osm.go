package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const ingestOverpassBodyLimit = 16 << 20 // 16 MiB; city-wide shop=* is larger than live 2 MiB cap

type ingestBox struct {
	city    string
	country string
	south   float64
	west    float64
	north   float64
	east    float64
	rich    bool
}

func (b ingestBox) bbox() string {
	return fmt.Sprintf("%0.4f,%0.4f,%0.4f,%0.4f", b.south, b.west, b.north, b.east)
}

// 全品类入库用的主要商业城市。公共 Overpass 扛不住全球 shop=*，
// 按城市 bbox 拉 node/way["shop"]["name"] 才能稳定入库。
var ingestShopBoxes = []ingestBox{
	{city: "berlin", country: "DE", south: 52.45, west: 13.25, north: 52.58, east: 13.55},
	{city: "munich", country: "DE", south: 48.08, west: 11.45, north: 48.22, east: 11.70},
	{city: "hamburg", country: "DE", south: 53.50, west: 9.88, north: 53.62, east: 10.12},
	{city: "frankfurt", country: "DE", south: 50.08, west: 8.62, north: 50.16, east: 8.75},
	{city: "cologne", country: "DE", south: 50.90, west: 6.88, north: 50.99, east: 7.05},
	{city: "newyork", country: "US", south: 40.68, west: -74.05, north: 40.82, east: -73.90},
	{city: "losangeles", country: "US", south: 34.00, west: -118.45, north: 34.12, east: -118.22},
	{city: "chicago", country: "US", south: 41.85, west: -87.72, north: 41.95, east: -87.60},
	{city: "london", country: "GB", south: 51.48, west: -0.20, north: 51.56, east: 0.02},
	{city: "paris", country: "FR", south: 48.83, west: 2.28, north: 48.90, east: 2.40},
	{city: "milan", country: "IT", south: 45.44, west: 9.14, north: 45.50, east: 9.24},
	{city: "rome", country: "IT", south: 41.86, west: 12.45, north: 41.93, east: 12.54},
	{city: "madrid", country: "ES", south: 40.39, west: -3.75, north: 40.45, east: -3.65},
	{city: "barcelona", country: "ES", south: 41.37, west: 2.13, north: 41.42, east: 2.20},
	{city: "amsterdam", country: "NL", south: 52.35, west: 4.85, north: 52.40, east: 4.95},
	{city: "rotterdam", country: "NL", south: 51.90, west: 4.44, north: 51.95, east: 4.52},
	{city: "brussels", country: "BE", south: 50.83, west: 4.32, north: 50.87, east: 4.40},
	{city: "vienna", country: "AT", south: 48.18, west: 16.33, north: 48.24, east: 16.42},
	{city: "zurich", country: "CH", south: 47.35, west: 8.50, north: 47.40, east: 8.57},
	{city: "stockholm", country: "SE", south: 59.31, west: 18.02, north: 59.36, east: 18.12},
	{city: "copenhagen", country: "DK", south: 55.66, west: 12.54, north: 55.70, east: 12.60},
	{city: "warsaw", country: "PL", south: 52.21, west: 20.96, north: 52.26, east: 21.05},
	{city: "prague", country: "CZ", south: 50.06, west: 14.39, north: 50.10, east: 14.46},
	{city: "istanbul", country: "TR", south: 41.00, west: 28.94, north: 41.06, east: 29.04},
	{city: "dubai", country: "AE", south: 25.18, west: 55.25, north: 25.28, east: 55.35},
	{city: "hongkong", country: "HK", south: 22.27, west: 114.14, north: 22.33, east: 114.20},
	{city: "tokyo", country: "JP", south: 35.65, west: 139.70, north: 35.72, east: 139.80},
	{city: "osaka", country: "JP", south: 34.66, west: 135.48, north: 34.72, east: 135.54},
	{city: "seoul", country: "KR", south: 37.54, west: 126.96, north: 37.58, east: 127.04},
	{city: "shanghai", country: "CN", south: 31.20, west: 121.44, north: 31.26, east: 121.52},
	{city: "shenzhen", country: "CN", south: 22.52, west: 114.04, north: 22.58, east: 114.12},
	{city: "guangzhou", country: "CN", south: 23.10, west: 113.24, north: 23.16, east: 113.32},
	{city: "sydney", country: "AU", south: -33.90, west: 151.18, north: -33.84, east: 151.24},
	{city: "melbourne", country: "AU", south: -37.84, west: 144.94, north: -37.80, east: 145.00},
	{city: "toronto", country: "CA", south: 43.64, west: -79.42, north: 43.68, east: -79.36},
	{city: "vancouver", country: "CA", south: 49.26, west: -123.14, north: 49.30, east: -123.08},
	{city: "saopaulo", country: "BR", south: -23.58, west: -46.68, north: -23.53, east: -46.62},
	{city: "mexicocity", country: "MX", south: 19.41, west: -99.18, north: 19.45, east: -99.12},
}

// ingestSEAShopBoxes is a denser Southeast Asia dump: wider metro boxes plus
// Vietnam / Philippines / Cambodia / Laos / Myanmar / Brunei that the first
// 42-city pass skipped. Public Overpass still cannot do a country-wide shop=*.
var ingestSEAShopBoxes = []ingestBox{
	{city: "singapore", country: "SG", south: 1.22, west: 103.60, north: 1.47, east: 104.04},
	{city: "kualalumpur", country: "MY", south: 2.95, west: 101.50, north: 3.28, east: 101.85},
	{city: "penang", country: "MY", south: 5.38, west: 100.28, north: 5.45, east: 100.36},
	{city: "johorbahru", country: "MY", south: 1.45, west: 103.70, north: 1.55, east: 103.80},
	{city: "ipoh", country: "MY", south: 4.58, west: 101.05, north: 4.65, east: 101.12},
	{city: "bangkok", country: "TH", south: 13.65, west: 100.40, north: 13.90, east: 100.70},
	{city: "chiangmai", country: "TH", south: 18.75, west: 98.94, north: 18.85, east: 99.04},
	{city: "pattaya", country: "TH", south: 12.90, west: 100.85, north: 13.00, east: 100.95},
	{city: "phuket", country: "TH", south: 7.84, west: 98.30, north: 7.95, east: 98.40},
	{city: "hatyai", country: "TH", south: 7.00, west: 100.44, north: 7.06, east: 100.52},
	{city: "jakarta", country: "ID", south: -6.40, west: 106.70, north: -6.10, east: 106.98},
	{city: "surabaya", country: "ID", south: -7.32, west: 112.70, north: -7.22, east: 112.80},
	{city: "bandung", country: "ID", south: -6.95, west: 107.57, north: -6.87, east: 107.65},
	{city: "medan", country: "ID", south: 3.55, west: 98.64, north: 3.63, east: 98.72},
	{city: "denpasar", country: "ID", south: -8.72, west: 115.17, north: -8.63, east: 115.26},
	{city: "semarang", country: "ID", south: -7.02, west: 110.38, north: -6.95, east: 110.46},
	{city: "makassar", country: "ID", south: -5.18, west: 119.38, north: -5.10, east: 119.46},
	{city: "hanoi", country: "VN", south: 21.00, west: 105.80, north: 21.08, east: 105.90},
	{city: "hochiminh", country: "VN", south: 10.75, west: 106.65, north: 10.83, east: 106.75},
	{city: "danang", country: "VN", south: 16.03, west: 108.18, north: 16.10, east: 108.25},
	{city: "haiphong", country: "VN", south: 20.84, west: 106.65, north: 20.88, east: 106.72},
	{city: "cantho", country: "VN", south: 10.02, west: 105.75, north: 10.08, east: 105.80},
	{city: "manila", country: "PH", south: 14.55, west: 120.96, north: 14.70, east: 121.10},
	{city: "cebu", country: "PH", south: 10.28, west: 123.85, north: 10.35, east: 123.92},
	{city: "davao", country: "PH", south: 7.05, west: 125.58, north: 7.12, east: 125.65},
	{city: "phnompenh", country: "KH", south: 11.52, west: 104.88, north: 11.60, east: 104.96},
	{city: "siemreap", country: "KH", south: 13.34, west: 103.84, north: 13.40, east: 103.90},
	{city: "vientiane", country: "LA", south: 17.94, west: 102.58, north: 18.00, east: 102.66},
	{city: "yangon", country: "MM", south: 16.76, west: 96.12, north: 16.85, east: 96.20},
	{city: "mandalay", country: "MM", south: 21.94, west: 96.06, north: 22.00, east: 96.13},
	{city: "bandarseri", country: "BN", south: 4.88, west: 114.90, north: 4.95, east: 114.97},
}

func allIngestShopBoxes() []ingestBox {
	out := make([]ingestBox, 0, len(ingestShopBoxes)+len(ingestSEAShopBoxes))
	out = append(out, ingestShopBoxes...)
	out = append(out, ingestSEAShopBoxes...)
	return out
}

// SEAIngestBoxes returns the Southeast Asia city dump used by -sea ingest.
func SEAIngestBoxes() []ingestBox {
	return append([]ingestBox(nil), ingestSEAShopBoxes...)
}

func (c *Client) ingestOSMAllShops(ctx context.Context, dir *Directory, opt IngestOptions) IngestStats {
	started := time.Now()
	boxes := opt.OSMBoxes
	if len(boxes) == 0 {
		boxes = ingestShopBoxes
	}
	limit := opt.OSMLimitPerCity
	if limit <= 0 {
		limit = 1500
	}
	inserted := 0
	failed := 0
	for i, box := range boxes {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: "osm", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "osm", started, inserted, st.Err)
			return st
		}
		query := fmt.Sprintf(`[out:json][timeout:45];(node["shop"]["name"](%s);way["shop"]["name"](%s););out tags %d;`,
			box.bbox(), box.bbox(), limit)
		raw, err := c.fetchOverpassLimit(ctx, query, ingestOverpassBodyLimit)
		if err != nil {
			failed++
			logIngest("OSM %s fail: %v", box.city, err)
			continue
		}
		n, err := dir.InsertBatch(ctx, parseOverpassMerchants(raw, box))
		if err != nil {
			st := IngestStats{Source: "osm", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "osm", started, inserted, st.Err)
			return st
		}
		inserted += n
		logIngest("OSM %s +%d (city %d/%d, total %d)", box.city, n, i+1, len(boxes), inserted)
	}
	note := fmt.Sprintf("%d cities, %d failed", len(boxes), failed)
	st := IngestStats{Source: "osm", Rows: inserted, Took: time.Since(started), Note: note}
	_ = dir.RecordRun(ctx, "osm", started, inserted, note)
	return st
}

func parseOverpassMerchants(raw []byte, box ingestBox) []Merchant {
	var doc overpassDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	out := make([]Merchant, 0, len(doc.Elements))
	for _, el := range doc.Elements {
		if el.ID == 0 || el.Tags == nil {
			continue
		}
		name := strings.TrimSpace(firstNonEmpty(el.Tags["name"], el.Tags["name:en"], el.Tags["operator"]))
		if name == "" {
			continue
		}
		kind := el.Type
		if kind == "" {
			kind = "node"
		}
		home := firstNonEmpty(el.Tags["website"], el.Tags["contact:website"], el.Tags["url"])
		if home == "" {
			home = fmt.Sprintf("https://www.openstreetmap.org/%s/%d", kind, el.ID)
		} else if !strings.Contains(home, "://") {
			home = "https://" + strings.TrimPrefix(home, "//")
		}
		extID := fmt.Sprintf("osm:%s:%d", kind, el.ID)
		out = append(out, Merchant{
			ExtID:    extID,
			Source:   "osm",
			Name:     name,
			Shop:     strings.TrimSpace(el.Tags["shop"]),
			Country:  firstNonEmpty(countryFromOSMTags(el.Tags), box.country),
			City:     firstNonEmpty(el.Tags["addr:city"], el.Tags["addr:town"], box.city),
			Homepage: home,
			Phone:    firstNonEmpty(el.Tags["phone"], el.Tags["contact:phone"]),
			Profiles: osmTagProfiles(extID, name, el.Tags),
		})
	}
	return out
}

func countryFromOSMTags(tags map[string]string) string {
	if v := strings.TrimSpace(tags["addr:country"]); v != "" {
		return strings.ToUpper(v)
	}
	if v := strings.TrimSpace(tags["is_in:country_code"]); v != "" {
		return strings.ToUpper(v)
	}
	return ""
}
