package outreach

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	researchTimeout   = 12 * time.Second
	researchMaxFetch  = 512 * 1024
	researchMaxLength = 1500
	// researchTTL controls how long cached research stays fresh.
	researchTTL = 30 * 24 * time.Hour
)

var (
	scriptStylePattern = regexp.MustCompile(`(?is)<(script|style|noscript|svg|head)[^>]*>.*?</\s*(script|style|noscript|svg|head)\s*>`)
	tagPattern         = regexp.MustCompile(`(?s)<[^>]*>`)
	titlePattern       = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	metaDescPattern    = regexp.MustCompile(`(?is)<meta[^>]+name=["']description["'][^>]+content=["']([^"']+)["']`)
	metaDescPatternAlt = regexp.MustCompile(`(?is)<meta[^>]+content=["']([^"']+)["'][^>]+name=["']description["']`)
)

// Researcher builds a short factual summary of a lead's website, which is the
// raw material for personalized first lines. Implemented by WebsiteResearcher
// and by test doubles.
type Researcher interface {
	Research(ctx context.Context, websiteURL string) (string, error)
}

// WebsiteResearcher fetches the homepage and extracts title, description and
// the first visible text.
type WebsiteResearcher struct {
	httpClient *http.Client
}

// NewWebsiteResearcher creates a researcher with conservative limits.
func NewWebsiteResearcher() *WebsiteResearcher {
	return &WebsiteResearcher{
		httpClient: &http.Client{Timeout: researchTimeout},
	}
}

// Research downloads the site and produces a plain-text extract.
func (r *WebsiteResearcher) Research(ctx context.Context, websiteURL string) (string, error) {
	websiteURL = strings.TrimSpace(websiteURL)
	if websiteURL == "" {
		return "", nil
	}

	if !strings.HasPrefix(websiteURL, "http://") && !strings.HasPrefix(websiteURL, "https://") {
		websiteURL = "https://" + websiteURL
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, websiteURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("build research request: %w", err)
	}

	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; business-inquiry)")
	request.Header.Set("Accept-Language", "en,zh;q=0.8")

	response, err := r.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch website: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("website returned HTTP %d", response.StatusCode)
	}

	contentType := response.Header.Get("Content-Type")
	if contentType != "" && !strings.Contains(contentType, "html") && !strings.Contains(contentType, "text") {
		return "", fmt.Errorf("website returned non-text content (%s)", contentType)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, researchMaxFetch))
	if err != nil {
		return "", fmt.Errorf("read website: %w", err)
	}

	return SummarizeHTML(string(data)), nil
}

// SummarizeHTML turns raw HTML into a compact factual snippet: title, meta
// description, then the first visible body text.
func SummarizeHTML(html string) string {
	var parts []string

	if match := titlePattern.FindStringSubmatch(html); len(match) > 1 {
		if title := collapseSpace(decodeBasicEntities(match[1])); title != "" {
			parts = append(parts, "Title: "+title)
		}
	}

	description := ""
	if match := metaDescPattern.FindStringSubmatch(html); len(match) > 1 {
		description = collapseSpace(decodeBasicEntities(match[1]))
	} else if match := metaDescPatternAlt.FindStringSubmatch(html); len(match) > 1 {
		description = collapseSpace(decodeBasicEntities(match[1]))
	}

	if description != "" {
		parts = append(parts, "Description: "+description)
	}

	body := scriptStylePattern.ReplaceAllString(html, " ")
	body = tagPattern.ReplaceAllString(body, " ")
	body = decodeBasicEntities(body)

	if text := collapseSpace(body); text != "" {
		parts = append(parts, "Page text: "+text)
	}

	return clipRunes(strings.Join(parts, "\n"), researchMaxLength)
}

func collapseSpace(value string) string {
	var (
		b        strings.Builder
		lastRune rune
	)

	for _, r := range value {
		if unicode.IsSpace(r) {
			r = ' '
		}

		if r == ' ' && lastRune == ' ' {
			continue
		}

		b.WriteRune(r)
		lastRune = r
	}

	return strings.TrimSpace(b.String())
}

func decodeBasicEntities(value string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
		"&nbsp;", " ",
	)

	return replacer.Replace(value)
}
