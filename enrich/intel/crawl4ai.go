package intel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gosom/google-maps-scraper/enrich"
)

// companyExtractSchema is the JSON schema crawl4ai's LLMExtractionStrategy
// is asked to fill. Keeping it stable lets us merge results into CompanyProfile
// without per-site prompt engineering.
var companyExtractSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"legal_name":       map[string]any{"type": "string"},
		"description":      map[string]any{"type": "string"},
		"founded_year":     map[string]any{"type": "integer"},
		"employee_range":   map[string]any{"type": "string"},
		"emails":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"phones":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"products":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"markets":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"certifications":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"trade_roles":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"registration_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"people": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"title": map[string]any{"type": "string"},
					"email": map[string]any{"type": "string"},
				},
			},
		},
	},
}

// crawl4AIExtract calls a self-hosted crawl4ai `/crawl` endpoint with an LLM
// extraction strategy. When the sidecar is down the call fails quietly.
func crawl4AIExtract(ctx context.Context, client *http.Client, baseURL, pageURL string) (*enrich.LLMExtract, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	payload := map[string]any{
		"urls": []string{pageURL},
		"crawler_config": map[string]any{
			"type": "CrawlerRunConfig",
			"params": map[string]any{
				"extraction_strategy": map[string]any{
					"type": "LLMExtractionStrategy",
					"params": map[string]any{
						"instruction": "Extract company background-research facts for B2B outreach: " +
							"legal name, short description, founding year, employee range, " +
							"decision-maker names with titles, emails, phones, products, " +
							"export markets, certifications, trade roles " +
							"(manufacturer/wholesaler/distributor/importer/exporter), " +
							"and registration or VAT numbers.",
						"schema":          companyExtractSchema,
						"extraction_type": "schema",
						"input_format":    "markdown",
					},
				},
			},
		},
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/crawl", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("crawl4ai: status %d", resp.StatusCode)
	}

	return parseCrawl4AIExtract(body)
}

func parseCrawl4AIExtract(body []byte) (*enrich.LLMExtract, error) {
	// crawl4ai wraps results as { "results": [ { "extracted_content": "..." } ] }
	// or sometimes returns the extract directly. Tolerate both.
	var envelope struct {
		Results []struct {
			ExtractedContent json.RawMessage `json:"extracted_content"`
			Success          bool            `json:"success"`
		} `json:"results"`
		ExtractedContent json.RawMessage `json:"extracted_content"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}

	candidates := make([]json.RawMessage, 0, 2)
	if len(envelope.ExtractedContent) > 0 {
		candidates = append(candidates, envelope.ExtractedContent)
	}

	for _, r := range envelope.Results {
		if len(r.ExtractedContent) > 0 {
			candidates = append(candidates, r.ExtractedContent)
		}
	}

	for _, raw := range candidates {
		if extracted := decodeLLMExtract(raw); extracted != nil {
			return extracted, nil
		}
	}

	return nil, fmt.Errorf("crawl4ai: no extractable content")
}

func decodeLLMExtract(raw json.RawMessage) *enrich.LLMExtract {
	// extracted_content may itself be a JSON string containing JSON.
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		raw = json.RawMessage(asString)
	}

	// Sometimes it is a one-element array of objects.
	var asArray []enrich.LLMExtract
	if err := json.Unmarshal(raw, &asArray); err == nil && len(asArray) > 0 {
		return &asArray[0]
	}

	var extracted enrich.LLMExtract
	if err := json.Unmarshal(raw, &extracted); err != nil {
		return nil
	}

	return &extracted
}
