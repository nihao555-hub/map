package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type sidecarUser struct {
	Platform      string `json:"platform"`
	Username      string `json:"username"`
	UniqueID      string `json:"uniqueId"`
	Nickname      string `json:"nickname"`
	Signature     string `json:"signature"`
	SecUID        string `json:"secUid"`
	ID            string `json:"id"`
	Verified      bool   `json:"verified"`
	FollowerCount int    `json:"followerCount"`
	Avatar        string `json:"avatar"`
	HomepageURL   string `json:"homepageUrl"`
	Source        string `json:"source"`
}

type sidecarResponse struct {
	Users    []sidecarUser `json:"users"`
	Warnings []string      `json:"warnings"`
	Error    string        `json:"error"`
	Source   string        `json:"source"`
}

const (
	sidecarDefaultCount = 30
	sidecarMinCount     = 5
	sidecarMaxCount     = 50
)

func sidecarCount(limit int) int {
	if limit <= 0 {
		return sidecarDefaultCount
	}
	if limit < sidecarMinCount {
		return sidecarMinCount
	}
	if limit > sidecarMaxCount {
		return sidecarMaxCount
	}
	return limit
}

func (c *Client) searchTikTokAPI(ctx context.Context, keyword string, limit int) ([]Hit, string, error) {
	if c == nil || c.TikTokURL == "" {
		return nil, "", fmt.Errorf("TikTok-Api sidecar URL is empty")
	}

	endpoint := c.TikTokURL + "/search/users?q=" + url.QueryEscape(keyword) + "&count=" + strconv.Itoa(sidecarCount(limit))
	raw, err := c.get(ctx, endpoint, nil)
	if err != nil {
		return nil, "", fmt.Errorf("davidteather/TikTok-Api sidecar: %w", err)
	}

	return parseSidecarUsers(raw, PlatformTikTok, "tiktok-api")
}

func (c *Client) searchF2(ctx context.Context, keyword, platform string, limit int) ([]Hit, string, error) {
	if c == nil || c.F2URL == "" {
		return nil, "", fmt.Errorf("f2 sidecar URL is empty")
	}

	endpoint := c.F2URL + "/search/users?platform=" + url.QueryEscape(platform) +
		"&q=" + url.QueryEscape(keyword) + "&count=" + strconv.Itoa(sidecarCount(limit))
	raw, err := c.get(ctx, endpoint, nil)
	if err != nil {
		return nil, "", fmt.Errorf("Johnserf-Seed/f2 sidecar: %w", err)
	}

	return parseSidecarUsers(raw, platform, "f2")
}

func parseSidecarUsers(raw []byte, defaultPlatform, defaultSource string) ([]Hit, string, error) {
	var resp sidecarResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, "", fmt.Errorf("sidecar json: %w", err)
	}

	if resp.Error != "" && len(resp.Users) == 0 {
		return nil, strings.Join(resp.Warnings, "; "), fmt.Errorf("%s", resp.Error)
	}

	src := firstNonEmpty(resp.Source, defaultSource)
	out := make([]Hit, 0, len(resp.Users))
	for _, u := range resp.Users {
		platform := strings.ToLower(firstNonEmpty(u.Platform, defaultPlatform))
		hit, ok := hitFromSidecarUser(u, platform, src)
		if ok {
			out = append(out, hit)
		}
	}

	return out, strings.Join(resp.Warnings, "; "), nil
}

func hitFromSidecarUser(u sidecarUser, platform, source string) (Hit, bool) {
	handle := firstNonEmpty(u.UniqueID, u.Username)
	sec := strings.TrimSpace(u.SecUID)
	nick := firstNonEmpty(u.Nickname, handle, sec)
	sig := strings.TrimSpace(u.Signature)

	var hit Hit
	switch platform {
	case PlatformDouyin:
		id := firstNonEmpty(sec, handle)
		if id == "" {
			return Hit{}, false
		}

		home := firstNonEmpty(u.HomepageURL, "https://www.douyin.com/user/"+id)
		hit = douyinHit(id, nick, sig, home, source)
	default:
		if handle == "" {
			return Hit{}, false
		}

		hit = tiktokHit(handle, nick, sig, source)
		if u.HomepageURL != "" {
			hit.HomepageURL = u.HomepageURL
			hit.MessageURL = u.HomepageURL
		}
	}

	hit.Score = 90
	if u.Verified || u.FollowerCount > 0 || u.Avatar != "" {
		hit.Extra = map[string]string{}
		if u.Verified {
			hit.Extra["verified"] = "true"
		}

		if u.FollowerCount > 0 {
			hit.Extra["followers"] = strconv.Itoa(u.FollowerCount)
		}

		if u.Avatar != "" {
			hit.Extra["avatar"] = u.Avatar
		}
	}

	return hit, true
}

func (c *Client) searchTikHubDouyin(ctx context.Context, keyword string, limit int) ([]Hit, error) {
	if c == nil || c.TikHubToken == "" {
		return nil, nil
	}

	endpoint := "https://api.tikhub.io/api/v1/douyin/web/fetch_search_user?keyword=" +
		url.QueryEscape(keyword) + "&count=" + strconv.Itoa(sidecarCount(limit))

	raw, err := c.get(ctx, endpoint, map[string]string{"Authorization": "Bearer " + c.TikHubToken})
	if err != nil {
		return nil, err
	}

	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}

	users := flattenTikHubUsers(envelope)
	out := make([]Hit, 0, len(users))
	for _, u := range users {
		sec := firstNonEmpty(asString(u["sec_uid"]), asString(u["secUid"]))
		nick := firstNonEmpty(asString(u["nickname"]), asString(u["nick_name"]))
		sig := firstNonEmpty(asString(u["signature"]), asString(u["bio"]))
		if sec == "" && nick == "" {
			continue
		}

		hit := douyinHit(firstNonEmpty(sec, nick), nick, sig, "", "tikhub")
		hit.Score = 88
		out = append(out, hit)
	}

	return out, nil
}

func flattenTikHubUsers(envelope map[string]any) []map[string]any {
	for _, key := range []string{"data", "user_list", "users"} {
		if arr, ok := asMapSlice(envelope[key]); ok {
			return arr
		}

		if nested, ok := envelope[key].(map[string]any); ok {
			if arr, ok := asMapSlice(nested["user_list"]); ok {
				return arr
			}

			if arr, ok := asMapSlice(nested["users"]); ok {
				return arr
			}

			if arr, ok := asMapSlice(nested["data"]); ok {
				return arr
			}
		}
	}

	return nil
}

func asMapSlice(v any) ([]map[string]any, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}

	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if user, ok := m["user_info"].(map[string]any); ok {
			out = append(out, user)
			continue
		}

		if user, ok := m["user"].(map[string]any); ok {
			out = append(out, user)
			continue
		}

		out = append(out, m)
	}

	return out, len(out) > 0
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}

		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}
