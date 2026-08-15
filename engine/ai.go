package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultAIBaseURL = "http://127.0.0.1:11434/v1"
	defaultAIModel   = "qwen2.5:7b"
	aiProbeTimeout   = 250 * time.Millisecond
	aiExpandTimeout  = 1500 * time.Millisecond
	aiSkipFor        = 5 * time.Minute
	maxAITerms       = 8
)

var aiSkipUntil atomic.Int64

type chatCompletionRequest struct {
	Model    string              `json:"model"`
	Messages []map[string]string `json:"messages"`
	Stream   bool                `json:"stream"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// ExpandSearchTerms is original + glossary + optional local OpenAI-compatible model
// (Ollama at :11434 by default, same machine as 地图获客). Slow/offline AI is skipped.
func ExpandSearchTerms(ctx context.Context, c *Client, keyword, country, role string) []string {
	base := LocalSearchTerms(keyword, country)
	extra := expandTermsWithAI(ctx, c, keyword, country, role)
	out := uniqueFoldedStrings(append(base, extra...))

	return clipTerms(out, maxLocalTerms+2)
}

func expandTermsWithAI(ctx context.Context, c *Client, keyword, country, role string) []string {
	base, model, ok := aiEndpoint(c)
	if !ok {
		return nil
	}

	aiCtx, cancel := context.WithTimeout(ctx, aiExpandTimeout)
	defer cancel()

	lang := LangForCountry(country)
	market := LookupCountry(country).Label
	if market == "" || market == "不限" {
		market = country
	}
	prompt := fmt.Sprintf(
		"Product keyword: %s\nTarget market: %s\nLocal language code: %s\nRole: %s\n"+
			"Return ONLY a JSON array of 4 to 8 short search phrases locals would type on Facebook or LinkedIn to find %s of this product. Include the local script when it is not English. No commentary.",
		keyword, market, firstNonEmpty(lang, "en"), NormalizeRole(role), NormalizeRole(role)+"s",
	)

	body, err := json.Marshal(chatCompletionRequest{
		Model: model,
		Messages: []map[string]string{
			{"role": "system", "content": "You expand B2B product keywords into local search phrases."},
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return nil
	}

	req, err := http.NewRequestWithContext(aiCtx, http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := aiHTTPClient(c).Do(req)
	if err != nil {
		if os.Getenv("ENGINE_AI_BASE_URL") == "" {
			setCooldown(&aiSkipUntil, aiSkipFor)
		}
		return nil
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil || resp.StatusCode >= 400 {
		return nil
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Choices) == 0 {
		return nil
	}

	return clipTerms(parseAITermList(parsed.Choices[0].Message.Content), maxAITerms)
}

func parseAITermList(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	if i := strings.Index(content, "["); i >= 0 {
		if j := strings.LastIndex(content, "]"); j > i {
			content = content[i : j+1]
		}
	}
	var arr []string
	if err := json.Unmarshal([]byte(content), &arr); err == nil {
		return uniqueFoldedStrings(arr)
	}

	return nil
}

func aiEndpoint(c *Client) (base, model string, ok bool) {
	if cooldownActive(&aiSkipUntil) && strings.TrimSpace(os.Getenv("ENGINE_AI_BASE_URL")) == "" {
		return "", "", false
	}
	base = strings.TrimSpace(os.Getenv("ENGINE_AI_BASE_URL"))
	if c != nil && strings.TrimSpace(c.AIBaseURL) != "" {
		base = strings.TrimSpace(c.AIBaseURL)
	}
	if base == "" {
		base = defaultAIBaseURL
	}
	model = strings.TrimSpace(os.Getenv("ENGINE_AI_MODEL"))
	if c != nil && strings.TrimSpace(c.AIModel) != "" {
		model = strings.TrimSpace(c.AIModel)
	}
	if model == "" {
		model = defaultAIModel
	}
	if !aiReachable(c, base) {
		if strings.TrimSpace(os.Getenv("ENGINE_AI_BASE_URL")) == "" {
			setCooldown(&aiSkipUntil, aiSkipFor)
		}
		return "", "", false
	}

	return strings.TrimRight(base, "/"), model, true
}

func aiReachable(c *Client, base string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), aiProbeTimeout)
	defer cancel()

	url := strings.TrimRight(base, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := aiHTTPClient(c).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))

	return resp.StatusCode > 0 && resp.StatusCode < 500
}

func aiHTTPClient(c *Client) *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}

	return &http.Client{Timeout: aiExpandTimeout}
}
