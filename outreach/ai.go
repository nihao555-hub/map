package outreach

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	aiRequestTimeout = 60 * time.Second
	aiMaxResponse    = 1 << 20 // 1 MiB
)

// AICompleter produces one chat completion. Implemented by AIClient and by
// test doubles.
type AICompleter interface {
	Complete(ctx context.Context, settings *Settings, system, user string) (string, error)
}

// AIClient talks to any OpenAI-compatible chat-completions endpoint
// (DeepSeek, Qwen/DashScope, Kimi/Moonshot, OpenAI, local Ollama, ...).
type AIClient struct {
	httpClient *http.Client
}

// NewAIClient creates an AI client with sane timeouts.
func NewAIClient() *AIClient {
	return &AIClient{
		httpClient: &http.Client{Timeout: aiRequestTimeout},
	}
}

type aiChatRequest struct {
	Model       string          `json:"model"`
	Messages    []aiChatMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens"`
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
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends one system+user prompt pair and returns the assistant text.
func (c *AIClient) Complete(ctx context.Context, settings *Settings, system, user string) (string, error) {
	if !settings.AIConfigured() {
		return "", errors.New("AI is not configured; set base URL, model and OUTREACH_AI_API_KEY")
	}

	payload := aiChatRequest{
		Model: settings.AIModel,
		Messages: []aiChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.6,
		MaxTokens:   900,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode AI request: %w", err)
	}

	endpoint := strings.TrimRight(settings.AIBaseURL, "/") + "/chat/completions"

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build AI request: %w", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+settings.AIAPIKey)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("call AI endpoint: %w", err)
	}
	defer response.Body.Close()

	data, err := io.ReadAll(io.LimitReader(response.Body, aiMaxResponse))
	if err != nil {
		return "", fmt.Errorf("read AI response: %w", err)
	}

	var parsed aiChatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("decode AI response (HTTP %d): %w", response.StatusCode, err)
	}

	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("AI endpoint error (HTTP %d): %s", response.StatusCode, parsed.Error.Message)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 || len(parsed.Choices) == 0 {
		return "", fmt.Errorf("AI endpoint returned HTTP %d without choices", response.StatusCode)
	}

	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// extractJSONObject tolerates models that wrap JSON in prose or code fences.
func extractJSONObject(raw string, target any) error {
	raw = strings.TrimSpace(raw)

	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}

	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("parse AI JSON output: %w", err)
	}

	return nil
}
