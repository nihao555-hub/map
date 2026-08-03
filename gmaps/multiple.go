package gmaps

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	olc "github.com/google/open-location-code/go"
)

func ParseSearchResults(raw []byte) ([]*Entry, error) {
	var data []any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty JSON data")
	}

	container, ok := data[0].([]any)
	if !ok || len(container) == 0 {
		return nil, fmt.Errorf("invalid business list structure")
	}

	items := getNthElementAndCast[[]any](container, 1)
	if len(items) < 2 {
		return nil, fmt.Errorf("empty business list")
	}

	entries := make([]*Entry, 0, len(items)-1)

	for i := 1; i < len(items); i++ {
		arr, ok := items[i].([]any)
		if !ok {
			continue
		}

		business := getNthElementAndCast[[]any](arr, 14)

		var entry Entry

		entry.ID = getNthElementAndCast[string](business, 0)
		entry.Title = getNthElementAndCast[string](business, 11)
		entry.Categories = toStringSlice(getNthElementAndCast[[]any](business, 13))
		entry.WebSite = getNthElementAndCast[string](business, 7, 0)

		entry.ReviewRating = getNthElementAndCast[float64](business, 4, 7)
		entry.ReviewCount = int(getNthElementAndCast[float64](business, 4, 8))

		fullAddress := getNthElementAndCast[[]any](business, 2)

		entry.Address = func() string {
			sb := strings.Builder{}

			for i, part := range fullAddress {
				if i > 0 {
					sb.WriteString(", ")
				}

				sb.WriteString(fmt.Sprintf("%v", part))
			}

			return sb.String()
		}()

		entry.Latitude = getNthElementAndCast[float64](business, 9, 2)
		entry.Longtitude = getNthElementAndCast[float64](business, 9, 3)
		entry.Phone = strings.ReplaceAll(getNthElementAndCast[string](business, 178, 0, 0), " ", "")
		entry.OpenHours = getHours(business)
		entry.Status = getNthElementAndCast[string](business, 34, 4, 4)
		entry.Timezone = getNthElementAndCast[string](business, 30)
		entry.DataID = getNthElementAndCast[string](business, 10)

		fillSearchIdentifiers(&entry, business)

		entry.PlusCode = olc.Encode(entry.Latitude, entry.Longtitude, 10)

		// 快速搜索结果也拆出社媒（website 常是 Instagram/Facebook）
		entry.PromoteSocialFromMapsFields()

		entries = append(entries, &entry)
	}

	return entries, nil
}

// ftidPattern 匹配 Google 内部要素 ID，形如 0x2e69f3f6ce7041f5:0x4ffd7a3b776d57f7。
var ftidPattern = regexp.MustCompile(`^0x[0-9a-f]+:0x[0-9a-f]+$`)

// placeIDPattern 匹配 Maps Place ID，形如 ChIJ9UFwzvbzaS4R91dtdzt6_U8。
var placeIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)

// fillSearchIdentifiers 补齐快速模式的 place_id / data_id / cid / link。
//
// 搜索接口的响应里带着这些标识，但下标会随 Google 调整而漂移，所以先查已知位置，
// 未命中再在记录里扫描。没有它们的话，快速模式的结果无法回链到 Google Maps，
// 也拿不到可用于去重和背调的稳定主键。
func fillSearchIdentifiers(entry *Entry, business []any) {
	const (
		dataIDIdx  = 10
		placeIDIdx = 78
	)

	if !ftidPattern.MatchString(entry.DataID) {
		entry.DataID = findStringInRecord(business, dataIDIdx, ftidPattern.MatchString)
	}

	if entry.PlaceID == "" {
		entry.PlaceID = findStringInRecord(business, placeIDIdx, isPlaceID)
	}

	if entry.Cid == "" {
		entry.Cid = cidFromDataID(entry.DataID)
	}

	if entry.Link == "" {
		entry.Link = mapsLink(entry)
	}
}

func isPlaceID(s string) bool {
	// Place ID 目前都以 ChIJ/GhIJ 开头；限定前缀可避免把评论 ID 之类的 token 认成主键。
	if !strings.HasPrefix(s, "ChIJ") && !strings.HasPrefix(s, "GhIJ") {
		return false
	}

	return placeIDPattern.MatchString(s)
}

// findStringInRecord 先看 preferred 下标，未命中再深度遍历记录找第一个满足 match 的字符串。
func findStringInRecord(business []any, preferred int, match func(string) bool) string {
	if v := getNthElementAndCast[string](business, preferred); match(v) {
		return v
	}

	var walk func(node any) string

	walk = func(node any) string {
		switch typed := node.(type) {
		case string:
			if match(typed) {
				return typed
			}
		case []any:
			for _, child := range typed {
				if found := walk(child); found != "" {
					return found
				}
			}
		}

		return ""
	}

	return walk(business)
}

// cidFromDataID 把要素 ID 的后半段十六进制转成十进制 CID。
func cidFromDataID(dataID string) string {
	if !ftidPattern.MatchString(dataID) {
		return ""
	}

	_, hexPart, ok := strings.Cut(dataID, ":")
	if !ok {
		return ""
	}

	cid, err := strconv.ParseUint(strings.TrimPrefix(hexPart, "0x"), 16, 64)
	if err != nil {
		return ""
	}

	return strconv.FormatUint(cid, 10)
}

// mapsLink 按标识可用性挑一个能回到 Google Maps 的链接。
func mapsLink(entry *Entry) string {
	switch {
	case entry.PlaceID != "":
		return "https://www.google.com/maps/place/?q=place_id:" + entry.PlaceID
	case entry.Cid != "":
		return "https://maps.google.com/?cid=" + entry.Cid
	case entry.Latitude != 0 || entry.Longtitude != 0:
		return fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%s",
			url.QueryEscape(fmt.Sprintf("%s %.6f,%.6f", entry.Title, entry.Latitude, entry.Longtitude)))
	default:
		return ""
	}
}

func toStringSlice(arr []any) []string {
	ans := make([]string, 0, len(arr))
	for _, v := range arr {
		ans = append(ans, fmt.Sprintf("%v", v))
	}

	return ans
}
