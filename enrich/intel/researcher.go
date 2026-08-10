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

// runResearcher asks a gpt-researcher-compatible server for a markdown
// due-diligence report about the company.
func runResearcher(ctx context.Context, client *http.Client, baseURL string, profile *enrich.CompanyProfile) (string, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	query := buildResearchQuery(profile)

	payload := map[string]any{
		"task":          query,
		"report_type":   "research_report",
		"report_source": "web",
		"tone":          "Formal",
		"query_domains": nonEmpty(profile.Domain),
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// gpt-researcher gptr-server exposes POST /report/ or /api/report depending
	// on version. Try the common paths in order.
	for _, path := range []string{"/report/", "/api/report", "/v1/report"} {
		report, err := postResearcher(ctx, client, baseURL+path, raw)
		if err == nil && report != "" {
			return report, nil
		}
	}

	return "", fmt.Errorf("gpt-researcher: no reachable report endpoint")
}

func postResearcher(ctx context.Context, client *http.Client, endpoint string, raw []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	var envelope struct {
		Report  string `json:"report"`
		Content string `json:"content"`
		Result  string `json:"result"`
	}

	if err := json.Unmarshal(body, &envelope); err == nil {
		for _, candidate := range []string{envelope.Report, envelope.Content, envelope.Result} {
			if strings.TrimSpace(candidate) != "" {
				return candidate, nil
			}
		}
	}

	// Some deployments return raw markdown.
	if text := strings.TrimSpace(string(body)); text != "" && text[0] != '{' {
		return text, nil
	}

	return "", fmt.Errorf("empty report")
}

func buildResearchQuery(profile *enrich.CompanyProfile) string {
	name := profile.LegalName
	if name == "" {
		name = profile.Domain
	}

	parts := []string{
		"Write a concise B2B due-diligence brief on " + name + ".",
		"Cover: legal identity, ownership if known, products, export markets,",
		"certifications, key contacts, website tech stack, and any risk flags",
		"(sanctions, shell-company signals, very new domain).",
	}

	if profile.Domain != "" {
		parts = append(parts, "Official website: https://"+profile.Domain)
	}

	if profile.LEI != "" {
		parts = append(parts, "LEI: "+profile.LEI)
	}

	return strings.Join(parts, " ")
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}

	return out
}
