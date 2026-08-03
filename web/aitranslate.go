package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// AI 翻译配置：密钥只从环境变量读取，绝不写进仓库/前端。
//   GRSAI_API_KEY   必填才启用 AI 翻译
//   GRSAI_API_HOST  默认 https://grsaiapi.com
//   GRSAI_MODEL     默认 gemini-3.1-flash-lite
const (
	defaultGRSAIHost  = "https://grsaiapi.com"
	defaultGRSAIModel = "gemini-3.1-flash-lite"
)

type aiChatRequest struct {
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Messages []aiChatMessage `json:"messages"`
}

type aiChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type aiChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func grsaiAPIKey() string {
	return strings.TrimSpace(os.Getenv("GRSAI_API_KEY"))
}

func grsaiHost() string {
	if h := strings.TrimSpace(os.Getenv("GRSAI_API_HOST")); h != "" {
		return strings.TrimRight(h, "/")
	}

	return defaultGRSAIHost
}

func grsaiModel() string {
	if m := strings.TrimSpace(os.Getenv("GRSAI_MODEL")); m != "" {
		return m
	}

	return defaultGRSAIModel
}

// AITranslateEnabled 前端据此决定是否展示「AI 翻译」能力
func AITranslateEnabled() bool {
	return grsaiAPIKey() != ""
}

// AITranslateKeyword 把中文获客关键词译成目标国家常用搜索词（仅返回译文）
func AITranslateKeyword(ctx context.Context, text, countryName, langCode string) (string, error) {
	key := grsaiAPIKey()
	if key == "" {
		return "", fmt.Errorf("AI translate disabled: set GRSAI_API_KEY")
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("empty text")
	}

	countryName = strings.TrimSpace(countryName)
	langCode = strings.TrimSpace(langCode)
	if countryName == "" {
		countryName = langCode
	}

	system := "You translate Chinese business search keywords into the local language " +
		"people use on Google Maps in the target country. " +
		"Reply with ONLY the translated keyword phrase, no quotes, no explanation."
	user := fmt.Sprintf("Target country: %s (lang=%s).\nTranslate this keyword for Google Maps search:\n%s",
		countryName, langCode, text)

	body, err := json.Marshal(aiChatRequest{
		Model:  grsaiModel(),
		Stream: false,
		Messages: []aiChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", err
	}

	url := grsaiHost() + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 45 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai translate request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ai translate status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var parsed aiChatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("ai translate decode: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("ai translate empty choices")
	}

	out := strings.TrimSpace(parsed.Choices[0].Message.Content)
	out = strings.Trim(out, "\"'`")
	out = strings.Split(out, "\n")[0]
	out = strings.TrimSpace(out)

	if out == "" || containsChinese(out) {
		return "", fmt.Errorf("ai translate returned unusable text: %q", out)
	}

	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n] + "…"
}
