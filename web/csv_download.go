package web

import (
	"net/url"
	"path/filepath"
	"strings"
	"unicode"
)

// csvDownloadFilename builds a safe download filename from the job name.
func csvDownloadFilename(jobName, id string) string {
	name := strings.TrimSpace(jobName)
	if name == "" {
		name = "地图获客结果"
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 32, strings.ContainsRune(`<>:"/\|?*`, r):
			b.WriteByte('_')
		case unicode.IsSpace(r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	name = strings.Trim(b.String(), "._")
	if name == "" {
		name = "地图获客结果"
	}
	runes := []rune(name)
	if len(runes) > 80 {
		name = string(runes[:80])
	}
	if id != "" {
		short := id
		if len(short) > 8 {
			short = short[:8]
		}
		return name + "_" + short + ".csv"
	}
	return name + ".csv"
}

// contentDispositionAttachment returns a Content-Disposition value with ASCII
// fallback and RFC 5987 UTF-8 filename*.
func contentDispositionAttachment(filename string) string {
	base := filepath.Base(filename)
	ascii := make([]byte, 0, len(base))
	for i := 0; i < len(base); i++ {
		c := base[i]
		if c >= 0x20 && c <= 0x7e && c != '"' && c != '\\' {
			ascii = append(ascii, c)
		} else {
			ascii = append(ascii, '_')
		}
	}
	if len(ascii) == 0 || string(ascii) == ".csv" {
		ascii = []byte("results.csv")
	}
	return `attachment; filename="` + string(ascii) + `"; filename*=UTF-8''` + url.PathEscape(base)
}
