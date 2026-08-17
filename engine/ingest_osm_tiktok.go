package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const osmTikTokBodyLimit = 16 << 20

// ingestOSMTikTok pulls every OpenStreetMap object tagged contact:tiktok
// (or contact:douyin). The tag is rare enough that a worldwide query often
// finishes; timed-out worlds fall back to the existing country boxes.
func (c *Client) ingestOSMTikTok(ctx context.Context, dir *Directory) IngestStats {
	started := time.Now()
	if c == nil {
		return IngestStats{Source: "osm-tiktok", Took: time.Since(started), Note: "skipped"}
	}
	inserted := 0
	failed := 0
	queries := []struct {
		name  string
		query string
	}{{name: "world", query: osmTikTokWorldQuery()}}
	for _, box := range osmContactBoxes() {
		queries = append(queries, struct {
			name  string
			query string
		}{name: box.city, query: osmTikTokBoxQuery(box)})
	}

	worldOK := false
	for i, q := range queries {
		if err := ctx.Err(); err != nil {
			st := IngestStats{Source: "osm-tiktok", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "osm-tiktok", started, inserted, st.Err)
			return st
		}
		if worldOK && q.name != "world" {
			break
		}
		raw, err := c.fetchOverpassLimit(ctx, q.query, osmTikTokBodyLimit)
		if err != nil {
			failed++
			logIngest("OSM tiktok %s fail: %v", q.name, err)
			time.Sleep(800 * time.Millisecond)
			continue
		}
		n, err := dir.InsertBatch(ctx, parseOverpassShortVideoMerchants(raw, ingestBox{city: q.name}))
		if err != nil {
			st := IngestStats{Source: "osm-tiktok", Rows: inserted, Took: time.Since(started), Err: err.Error()}
			_ = dir.RecordRun(ctx, "osm-tiktok", started, inserted, st.Err)
			return st
		}
		inserted += n
		logIngest("OSM tiktok %s +%d (%d/%d total %d)", q.name, n, i+1, len(queries), inserted)
		if q.name == "world" && n >= 0 && err == nil {
			worldOK = true
		}
		time.Sleep(400 * time.Millisecond)
	}
	st := IngestStats{
		Source: "osm-tiktok",
		Rows:   inserted,
		Took:   time.Since(started),
		Note:   fmt.Sprintf("queries=%d failed=%d world=%v", len(queries), failed, worldOK),
	}
	_ = dir.RecordRun(ctx, "osm-tiktok", started, inserted, st.Note)
	return st
}

func osmTikTokWorldQuery() string {
	return `[out:json][timeout:180];
(
  nwr["contact:tiktok"];
  nwr["tiktok"];
  nwr["contact:douyin"];
  nwr["douyin"];
);
out tags;`
}

func osmTikTokBoxQuery(box ingestBox) string {
	bbox := box.bbox()
	return fmt.Sprintf(`[out:json][timeout:90];
(
  node["contact:tiktok"](%s);
  way["contact:tiktok"](%s);
  relation["contact:tiktok"](%s);
  node["tiktok"](%s);
  way["tiktok"](%s);
  node["contact:douyin"](%s);
  way["contact:douyin"](%s);
);
out tags;`, bbox, bbox, bbox, bbox, bbox, bbox, bbox)
}

func parseOverpassShortVideoMerchants(raw []byte, box ingestBox) []Merchant {
	var doc overpassDoc
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &doc); err != nil {
		return nil
	}
	out := make([]Merchant, 0, len(doc.Elements))
	for _, el := range doc.Elements {
		if el.ID == 0 || el.Tags == nil {
			continue
		}
		kind := el.Type
		if kind == "" {
			kind = "node"
		}
		extID := fmt.Sprintf("osm:%s:%d", kind, el.ID)
		name := strings.TrimSpace(firstNonEmpty(el.Tags["name"], el.Tags["name:en"], el.Tags["operator"]))
		profiles := osmTagProfiles(extID, name, el.Tags)
		var keep []Profile
		for _, p := range profiles {
			if p.Platform == PlatformTikTok || p.Platform == PlatformDouyin {
				keep = append(keep, p)
			}
		}
		if len(keep) == 0 {
			continue
		}
		if name == "" {
			name = firstNonEmpty(keep[0].Handle, keep[0].URL)
		}
		home := firstNonEmpty(el.Tags["website"], el.Tags["contact:website"], keep[0].URL)
		if home != "" && !strings.Contains(home, "://") {
			home = "https://" + strings.TrimPrefix(home, "//")
		}
		out = append(out, Merchant{
			ExtID:    extID,
			Source:   "osm-tiktok",
			Name:     name,
			Shop:     firstNonEmpty(el.Tags["shop"], keep[0].Platform),
			Country:  firstNonEmpty(countryFromOSMTags(el.Tags), box.country),
			City:     firstNonEmpty(el.Tags["addr:city"], el.Tags["addr:town"], box.city),
			Homepage: home,
			Phone:    firstNonEmpty(el.Tags["phone"], el.Tags["contact:phone"]),
			Profiles: keep,
		})
	}
	return out
}
