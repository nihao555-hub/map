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
	return c.ingestOSMContactBoxes(ctx, dir, osmContactBoxes())
}

func (c *Client) ingestOSMContactGaps(ctx context.Context, dir *Directory) IngestStats {
	return c.ingestOSMContactBoxes(ctx, dir, osmContactGapBoxes())
}

func (c *Client) ingestOSMContactBoxes(ctx context.Context, dir *Directory, boxes []ingestBox) IngestStats {
	started := time.Now()
	if c == nil {
		return IngestStats{Source: "osm-contacts", Took: time.Since(started), Note: "skipped"}
	}
	inserted := 0
	failed := 0
	for i, box := range boxes {
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
		logIngest("OSM contacts %s +%d (%d/%d total %d)", box.city, n, i+1, len(boxes), inserted)
		time.Sleep(400 * time.Millisecond)
	}
	st := IngestStats{
		Source: "osm-contacts",
		Rows:   inserted,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("%d boxes, %d failed", len(boxes), failed),
	}
	_ = dir.RecordRun(ctx, "osm-contacts", started, inserted, st.Note)
	return st
}

func osmContactQuery(box ingestBox) string {
	bbox := box.bbox()
	parts := []string{
		fmt.Sprintf(`node["contact:facebook"](%s);`, bbox),
		fmt.Sprintf(`way["contact:facebook"](%s);`, bbox),
		fmt.Sprintf(`node["contact:instagram"](%s);`, bbox),
		fmt.Sprintf(`way["contact:instagram"](%s);`, bbox),
		fmt.Sprintf(`node["contact:linkedin"](%s);`, bbox),
		fmt.Sprintf(`way["contact:linkedin"](%s);`, bbox),
	}
	if box.rich {
		parts = append(parts,
			fmt.Sprintf(`node["contact:twitter"](%s);`, bbox),
			fmt.Sprintf(`way["contact:twitter"](%s);`, bbox),
			fmt.Sprintf(`node["contact:youtube"](%s);`, bbox),
			fmt.Sprintf(`way["contact:youtube"](%s);`, bbox),
			fmt.Sprintf(`node["contact:tiktok"](%s);`, bbox),
			fmt.Sprintf(`way["contact:tiktok"](%s);`, bbox),
		)
	}
	out := "[out:json][timeout:70];(\n"
	for _, p := range parts {
		out += "  " + p + "\n"
	}
	out += ");out tags;"
	return out
}

// Country-scale boxes for regions that already complete, plus smaller splits
// for EU / India / US-east / Canada / LatAm that timed out as one rectangle.
func osmContactBoxes() []ingestBox {
	out := append([]ingestBox(nil), osmContactCoreBoxes()...)
	return append(out, osmContactGapBoxes()...)
}

func osmContactCoreBoxes() []ingestBox {
	return []ingestBox{
		{city: "sea-th-my-sg", country: "TH", south: 1.1, west: 99.5, north: 20.6, east: 105.8},
		{city: "sea-vn-kh-la", country: "VN", south: 8.3, west: 102.0, north: 23.5, east: 109.6},
		{city: "sea-id-ph", country: "ID", south: -11.2, west: 95.0, north: 19.0, east: 141.1},
		{city: "cn-kr-jp-tw", country: "CN", south: 21.5, west: 100.0, north: 46.0, east: 146.0},
		{city: "au-nz", country: "AU", south: -48.0, west: 112.0, north: -10.0, east: 180.0},
		{city: "me-gulf", country: "AE", south: 12.0, west: 32.0, north: 42.0, east: 60.5},
		{city: "uk-ie", country: "GB", south: 49.8, west: -11.0, north: 61.0, east: 2.1},
		{city: "us-central", country: "US", south: 25.0, west: -106.0, north: 49.5, east: -88.0},
		{city: "us-west", country: "US", south: 31.0, west: -125.0, north: 49.5, east: -106.0},
		{city: "africa", country: "ZA", south: -35.5, west: -18.0, north: 38.0, east: 52.0},
	}
}

func osmContactGapBoxes() []ingestBox {
	return []ingestBox{
		{city: "de-north", country: "DE", south: 51.0, west: 5.8, north: 55.1, east: 15.1, rich: true},
		{city: "de-south", country: "DE", south: 47.2, west: 5.8, north: 51.0, east: 15.1, rich: true},
		{city: "fr-north", country: "FR", south: 46.5, west: -5.2, north: 51.2, east: 8.3, rich: true},
		{city: "fr-south", country: "FR", south: 42.3, west: -1.8, north: 46.5, east: 7.8, rich: true},
		{city: "es", country: "ES", south: 35.9, west: -9.4, north: 43.9, east: 3.4, rich: true},
		{city: "it-north", country: "IT", south: 43.7, west: 6.6, north: 47.1, east: 13.9, rich: true},
		{city: "it-south", country: "IT", south: 36.6, west: 8.1, north: 43.7, east: 18.6, rich: true},
		{city: "nl-be-lu", country: "NL", south: 49.4, west: 2.3, north: 53.7, east: 7.3, rich: true},
		{city: "at-ch", country: "AT", south: 45.8, west: 5.9, north: 49.1, east: 17.2, rich: true},
		{city: "pt", country: "PT", south: 36.9, west: -9.6, north: 42.2, east: -6.1, rich: true},
		{city: "ie", country: "IE", south: 51.3, west: -10.6, north: 55.5, east: -5.9, rich: true},
		{city: "pl", country: "PL", south: 49.0, west: 14.1, north: 54.9, east: 24.2, rich: true},
		{city: "cz-sk-hu", country: "CZ", south: 45.7, west: 12.1, north: 51.1, east: 22.9, rich: true},
		{city: "ro-bg", country: "RO", south: 41.2, west: 20.2, north: 48.3, east: 29.8, rich: true},
		{city: "gr", country: "GR", south: 34.8, west: 19.3, north: 41.8, east: 28.3, rich: true},
		{city: "se-no", country: "SE", south: 55.2, west: 4.5, north: 71.2, east: 31.3, rich: true},
		{city: "fi-balt", country: "FI", south: 53.8, west: 20.8, north: 70.1, east: 31.6, rich: true},
		{city: "ua", country: "UA", south: 44.3, west: 22.1, north: 52.4, east: 40.3, rich: true},

		{city: "in-north", country: "IN", south: 23.5, west: 69.0, north: 35.6, east: 89.0, rich: true},
		{city: "in-west", country: "IN", south: 8.0, west: 68.0, north: 24.8, east: 78.5, rich: true},
		{city: "in-south", country: "IN", south: 8.0, west: 76.0, north: 16.6, east: 80.9, rich: true},
		{city: "in-east", country: "IN", south: 16.5, west: 80.0, north: 27.6, east: 97.4, rich: true},
		{city: "pk", country: "PK", south: 23.6, west: 60.8, north: 37.1, east: 77.9, rich: true},
		{city: "bd", country: "BD", south: 20.7, west: 88.0, north: 26.7, east: 92.8, rich: true},

		{city: "us-ne", country: "US", south: 40.4, west: -80.6, north: 47.5, east: -66.8, rich: true},
		{city: "us-midatl", country: "US", south: 36.5, west: -83.7, north: 41.4, east: -74.0, rich: true},
		{city: "us-se", country: "US", south: 30.1, west: -91.7, north: 37.1, east: -75.4, rich: true},
		{city: "us-fl", country: "US", south: 24.4, west: -87.7, north: 31.1, east: -79.8, rich: true},
		{city: "ca-east", country: "CA", south: 41.6, west: -84.0, north: 52.0, east: -52.6, rich: true},
		{city: "ca-west", country: "CA", south: 48.0, west: -141.0, north: 60.1, east: -88.0, rich: true},

		{city: "mx", country: "MX", south: 14.5, west: -118.4, north: 32.8, east: -86.6, rich: true},
		{city: "cam-carib", country: "GT", south: 7.0, west: -92.4, north: 22.0, east: -59.8, rich: true},
		{city: "co-ve-pe", country: "CO", south: -18.4, west: -81.4, north: 12.6, east: -59.8, rich: true},
		{city: "br-se", country: "BR", south: -25.8, west: -53.2, north: -14.2, east: -34.7, rich: true},
		{city: "br-rest", country: "BR", south: -33.8, west: -74.1, north: 5.3, east: -34.7, rich: true},
		{city: "ar-cl-uy", country: "AR", south: -56.0, west: -76.0, north: -21.7, east: -53.5, rich: true},
	}
}
