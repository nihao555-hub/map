//nolint:testpackage // shares the internal web test package with web_test.go
package web

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestGeoAnchor(t *testing.T) {
	tests := []struct {
		name     string
		lat, lon string
		want     string
	}{
		{"空坐标", "", "", ""},
		{"表单默认 0,0", "0", "0", ""},
		{"非法值", "abc", "100", ""},
		{"有效锚定", "13.756331", "100.501765", "13.756331, 100.501765"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := JobData{Lat: tt.lat, Lon: tt.lon}
			if got := d.GeoAnchor(); got != tt.want {
				t.Fatalf("GeoAnchor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJobRowTemplateShowsGeoAnchor(t *testing.T) {
	srv := newTestServer(t, t.TempDir())

	tmpl, ok := srv.tmpl["static/templates/job_row.html"]
	if !ok {
		t.Fatal("missing job_row template")
	}

	job := Job{
		ID:     "11111111-1111-1111-1111-111111111111",
		Name:   "咖啡店 / Bangkok",
		Date:   time.Now().UTC(),
		Status: StatusPending,
		Data:   JobData{Lat: "13.756331", Lon: "100.501765"},
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, job); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// 任务卡通过 data-lat/data-lon 把锚定坐标交给地图同步（不再展示文案行）
	if !strings.Contains(buf.String(), `data-lat="13.756331"`) ||
		!strings.Contains(buf.String(), `data-lon="100.501765"`) {
		t.Fatalf("expected anchored coords in card data attrs, got:\n%s", buf.String())
	}

	job.Data.Lat = "0"
	job.Data.Lon = "0"

	buf.Reset()

	if err := tmpl.Execute(&buf, job); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !strings.Contains(buf.String(), `data-lat="0"`) || !strings.Contains(buf.String(), `data-lon="0"`) {
		t.Fatalf("expected default 0,0 coords in data attrs, got:\n%s", buf.String())
	}
}
