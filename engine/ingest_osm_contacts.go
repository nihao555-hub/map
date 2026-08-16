package engine

import (
	"context"
	"fmt"
	"time"
)

const osmContactBodyLimit = 24 << 20

// ingestOSMContacts pulls worldwide shops that already publish contact:facebook
// (and sister tags) instead of the city-bbox shop=* dump.
func (c *Client) ingestOSMContacts(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil {
		return IngestStats{Source: "osm-contacts", Took: time.Since(started), Note: "skipped"}
	}
	inserted := 0
	failed := 0
	for i, box := range osmContactBoxes() {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: "osm-contacts", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "osm-contacts", started, inserted, st.Err)
			return st
		}
		query := osmContactQuery(box)
		raw, err := c.fetchOverpassLimit(ctx, query, osmContactBodyLimit)
		if err != nil {
			failed++
			logIngest("OSM contacts %s fail: %v", box.city, err)
			time.Sleep(800 * time.Millisecond)
			continue
		}
		n, err := dir.InsertBatch(ctx, parseOverpassMerchants(raw, box))
		if err != nil {
			st := IngestStats{Source: "osm-contacts", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "osm-contacts", started, inserted, st.Err)
			return st
		}
		inserted += n
		logIngest("OSM contacts %s +%d (%d/%d total %d)", box.city, n, i+1, len(osmContactBoxes()), inserted)
		time.Sleep(400 * time.Millisecond)
	}
	st := IngestStats{
		Source: "osm-contacts",
		Rows:   inserted,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("%d boxes, %d failed", len(osmContactBoxes()), failed),
	}
	_ = dir.RecordRun(ctx, "osm-contacts", started, inserted, st.Note)
	return st
}

func osmContactQuery(box ingestBox) string {
	return fmt.Sprintf(`[out:json][timeout:70];(
  node["contact:facebook"](%s);
  way["contact:facebook"](%s);
  node["contact:instagram"](%s);
  way["contact:instagram"](%s);
  node["contact:linkedin"](%s);
  way["contact:linkedin"](%s);
);out tags;`, box.bbox(), box.bbox(), box.bbox(), box.bbox(), box.bbox(), box.bbox())
}

// Country-scale boxes (not city cores) so contact:* tags outside downtown are kept.
func osmContactBoxes() []ingestBox {
	return []ingestBox{
		{city: "sea-th-my-sg", country: "TH", south: 1.1, west: 99.5, north: 20.6, east: 105.8},
		{city: "sea-vn-kh-la", country: "VN", south: 8.3, west: 102.0, north: 23.5, east: 109.6},
		{city: "sea-id-ph", country: "ID", south: -11.2, west: 95.0, north: 19.0, east: 141.1},
		{city: "in-pk-bd", country: "IN", south: 5.5, west: 66.0, north: 36.0, east: 93.0},
		{city: "cn-kr-jp-tw", country: "CN", south: 21.5, west: 100.0, north: 46.0, east: 146.0},
		{city: "au-nz", country: "AU", south: -48.0, west: 112.0, north: -10.0, east: 180.0},
		{city: "me-gulf", country: "AE", south: 12.0, west: 32.0, north: 42.0, east: 60.5},
		{city: "eu-west", country: "DE", south: 36.0, west: -11.0, north: 60.0, east: 18.0},
		{city: "eu-east", country: "PL", south: 35.0, west: 12.0, north: 71.0, east: 42.0},
		{city: "uk-ie", country: "GB", south: 49.8, west: -11.0, north: 61.0, east: 2.1},
		{city: "us-east", country: "US", south: 24.0, west: -88.0, north: 48.0, east: -66.0},
		{city: "us-central", country: "US", south: 25.0, west: -106.0, north: 49.5, east: -88.0},
		{city: "us-west", country: "US", south: 31.0, west: -125.0, north: 49.5, east: -106.0},
		{city: "ca", country: "CA", south: 41.5, west: -141.0, north: 70.0, east: -52.0},
		{city: "latam-north", country: "MX", south: 7.0, west: -118.0, north: 33.0, east: -60.0},
		{city: "latam-south", country: "BR", south: -56.0, west: -82.0, north: 6.0, east: -34.0},
		{city: "africa", country: "ZA", south: -35.5, west: -18.0, north: 38.0, east: 52.0},
	}
}
