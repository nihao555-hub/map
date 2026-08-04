package gmaps_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gosom/google-maps-scraper/gmaps"
)

// buildSearchResponse 拼出搜索接口的外层结构：data[0][1][i][14] 是每条商家记录。
func buildSearchResponse(businesses ...[]any) []byte {
	items := []any{"header"}
	for _, b := range businesses {
		record := make([]any, 15)
		record[14] = b
		items = append(items, record)
	}

	raw, err := json.Marshal([]any{[]any{"", items}})
	if err != nil {
		panic(err)
	}

	return raw
}

// businessRecord 造一条最小可用记录，下标对齐真实响应。
func businessRecord(title, ftid, placeID string) []any {
	b := make([]any, 200)
	b[10] = ftid
	b[11] = title
	b[78] = placeID
	b[9] = []any{nil, nil, -6.2088, 106.8456}

	return b
}

func Test_ParseSearchResults_FillsIdentifiers(t *testing.T) {
	raw := buildSearchResponse(businessRecord(
		"Lucky Cat Coffee & Kitchen",
		"0x2e69f3f6ce7041f5:0x4ffd7a3b776d57f7",
		"ChIJ9UFwzvbzaS4R91dtdzt6_U8",
	))

	entries, err := gmaps.ParseSearchResults(raw)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	entry := entries[0]
	require.Equal(t, "Lucky Cat Coffee & Kitchen", entry.Title)
	require.Equal(t, "ChIJ9UFwzvbzaS4R91dtdzt6_U8", entry.PlaceID)
	require.Equal(t, "0x2e69f3f6ce7041f5:0x4ffd7a3b776d57f7", entry.DataID)
	// 0x4ffd7a3b776d57f7 的十进制形式。
	require.Equal(t, "5763897493929416695", entry.Cid)
	require.Equal(t, "https://www.google.com/maps/place/?q=place_id:ChIJ9UFwzvbzaS4R91dtdzt6_U8", entry.Link)
}

func Test_ParseSearchResults_RecoversShiftedIdentifiers(t *testing.T) {
	// Google 挪动下标时，仍应扫描记录把标识找回来。
	b := make([]any, 200)
	b[11] = "Giyanti Coffee Roastery"
	b[9] = []any{nil, nil, -6.2088, 106.8456}
	b[131] = []any{"0x2e69f4151ceb7767:0x79ac645c2aef736f"}
	b[147] = []any{[]any{"ChIJZ3frHBX0aS4Rb3PvKlxkrHk"}}

	entries, err := gmaps.ParseSearchResults(buildSearchResponse(b))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.Equal(t, "0x2e69f4151ceb7767:0x79ac645c2aef736f", entries[0].DataID)
	require.Equal(t, "ChIJZ3frHBX0aS4Rb3PvKlxkrHk", entries[0].PlaceID)
	require.NotEmpty(t, entries[0].Cid)
}

func Test_ParseSearchResults_FallsBackToCoordinateLink(t *testing.T) {
	b := make([]any, 200)
	b[11] = "Warung Tanpa ID"
	b[9] = []any{nil, nil, -6.2088, 106.8456}

	entries, err := gmaps.ParseSearchResults(buildSearchResponse(b))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	entry := entries[0]
	require.Empty(t, entry.PlaceID)
	require.Empty(t, entry.Cid)
	require.Contains(t, entry.Link, "https://www.google.com/maps/search/")
	require.Contains(t, entry.Link, "-6.208800")
}

func Test_ParseSearchResults_IgnoresNonPlaceIDTokens(t *testing.T) {
	// 评论 ID 之类的长 token 不能被当成 Place ID。
	b := make([]any, 200)
	b[11] = "Kopi Random Token"
	b[9] = []any{nil, nil, -6.2088, 106.8456}
	b[78] = "XNFwaqi7IfCrhvcP79jhqQY"

	entries, err := gmaps.ParseSearchResults(buildSearchResponse(b))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Empty(t, entries[0].PlaceID)
}
