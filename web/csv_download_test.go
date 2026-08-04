package web

import (
	"strings"
	"testing"
)

func TestCSVDownloadFilename(t *testing.T) {
	got := csvDownloadFilename("雅加达 · 食品进口商", "abcdef12-3456-7890")
	if !strings.HasPrefix(got, "雅加达_·_食品进口商_abcdef12.csv") && !strings.Contains(got, "食品进口商") {
		t.Fatalf("got %q", got)
	}
	if !strings.HasSuffix(got, ".csv") {
		t.Fatalf("suffix: %q", got)
	}

	got = csvDownloadFilename(`bad<>:"/\|?*name`, "id1")
	if strings.ContainsAny(got, `<>:"/\|?*`) {
		t.Fatalf("unsanitized: %q", got)
	}

	got = csvDownloadFilename("", "xxxxxxxx-yyyy")
	if got != "地图获客结果_xxxxxxxx.csv" {
		t.Fatalf("empty name: %q", got)
	}
}

func TestContentDispositionAttachment(t *testing.T) {
	got := contentDispositionAttachment("地图获客结果_abcdef12.csv")
	if !strings.Contains(got, `filename="`) {
		t.Fatalf("missing ascii filename: %q", got)
	}
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Fatalf("missing utf8 filename: %q", got)
	}
	if !strings.Contains(got, "%E5%9C%B0") { // 地
		t.Fatalf("expected percent-encoded chinese: %q", got)
	}
}
