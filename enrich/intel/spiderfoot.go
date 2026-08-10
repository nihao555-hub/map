package intel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gosom/google-maps-scraper/enrich"
)

// runSpiderFoot starts a lightweight scan against a self-hosted SpiderFoot
// instance and collects a capped set of findings. SpiderFoot's REST surface
// varies by version; this client follows the documented /api endpoints used
// by recent MIT builds.
func runSpiderFoot(ctx context.Context, client *http.Client, baseURL, domain string) ([]enrich.OSINTFinding, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	scanID, err := spiderfootStart(ctx, client, baseURL, domain)
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(minDuration(ctx, 45*time.Second))

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}

		status, err := spiderfootStatus(ctx, client, baseURL, scanID)
		if err != nil {
			continue
		}

		if status == "FINISHED" || status == "ABORT-REQUESTED" || status == "ABORTED" {
			break
		}
	}

	return spiderfootEvents(ctx, client, baseURL, scanID)
}

func spiderfootStart(ctx context.Context, client *http.Client, baseURL, domain string) (string, error) {
	form := []byte("scanname=gmaps-" + domain +
		"&scantarget=" + domain +
		"&modulelist=" +
		"&typelist=" +
		"&usecase=Passive")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/startscan", bytes.NewReader(form))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("spiderfoot start: status %d", resp.StatusCode)
	}

	var envelope struct {
		ID     string `json:"id"`
		ScanID string `json:"scanid"`
	}

	if err := json.Unmarshal(body, &envelope); err == nil {
		if envelope.ID != "" {
			return envelope.ID, nil
		}

		if envelope.ScanID != "" {
			return envelope.ScanID, nil
		}
	}

	// Some builds return a bare id string.
	id := strings.Trim(strings.TrimSpace(string(body)), "\"")
	if id != "" {
		return id, nil
	}

	return "", fmt.Errorf("spiderfoot: empty scan id")
}

func spiderfootStatus(ctx context.Context, client *http.Client, baseURL, scanID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/scanstatus?id="+scanID, nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var envelope struct {
		Status string `json:"status"`
	}

	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Status != "" {
		return strings.ToUpper(envelope.Status), nil
	}

	// Legacy builds return a list: [id, name, target, created, started, ended, status]
	var legacy []any
	if err := json.Unmarshal(body, &legacy); err == nil && len(legacy) >= 7 {
		if status, ok := legacy[6].(string); ok {
			return strings.ToUpper(status), nil
		}
	}

	return "", fmt.Errorf("spiderfoot: unknown status payload")
}

func spiderfootEvents(ctx context.Context, client *http.Client, baseURL, scanID string) ([]enrich.OSINTFinding, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/scaneventresults?id="+scanID, nil)
	if err != nil {
		return nil, err
	}

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
		return nil, fmt.Errorf("spiderfoot events: status %d", resp.StatusCode)
	}

	return parseSpiderFootEvents(body)
}

func parseSpiderFootEvents(body []byte) ([]enrich.OSINTFinding, error) {
	// Modern JSON objects.
	var objects []struct {
		Type   string `json:"type"`
		Data   string `json:"data"`
		Module string `json:"module"`
		Source string `json:"source"`
	}

	if err := json.Unmarshal(body, &objects); err == nil && len(objects) > 0 {
		out := make([]enrich.OSINTFinding, 0, min(len(objects), 50))
		for i, o := range objects {
			if i >= 50 {
				break
			}

			out = append(out, enrich.OSINTFinding{
				Type:   o.Type,
				Data:   o.Data,
				Module: o.Module,
				Source: o.Source,
			})
		}

		return out, nil
	}

	// Legacy rows: [generated, dataType, data, module, source]
	var rows [][]any
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}

	out := make([]enrich.OSINTFinding, 0, min(len(rows), 50))

	for i, row := range rows {
		if i >= 50 || len(row) < 4 {
			continue
		}

		finding := enrich.OSINTFinding{}
		if v, ok := row[1].(string); ok {
			finding.Type = v
		}

		if v, ok := row[2].(string); ok {
			finding.Data = v
		}

		if v, ok := row[3].(string); ok {
			finding.Module = v
		}

		if len(row) > 4 {
			if v, ok := row[4].(string); ok {
				finding.Source = v
			}
		}

		out = append(out, finding)
	}

	return out, nil
}

func minDuration(ctx context.Context, fallback time.Duration) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < fallback {
			return remaining
		}
	}

	return fallback
}
