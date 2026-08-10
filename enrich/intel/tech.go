package intel

import (
	"bytes"
	"embed"
	"net/http"
	"strings"

	"github.com/rverton/webanalyze"
)

//go:embed technologies.json
var techFS embed.FS

type techFingerprinter struct {
	wa *webanalyze.WebAnalyzer
}

func (e *Enricher) fingerprint(pageURL string, body []byte) (stack []string, platform string, ecommerce bool) {
	fp, err := e.techClient()
	if err != nil || fp == nil {
		return nil, "", false
	}

	job := webanalyze.NewOfflineJob(pageURL, string(body), nil)
	result, _ := fp.wa.Process(job)

	seen := make(map[string]bool, len(result.Matches))
	stack = make([]string, 0, len(result.Matches))

	for _, match := range result.Matches {
		name := strings.TrimSpace(match.AppName)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}

		seen[strings.ToLower(name)] = true
		stack = append(stack, name)

		lower := strings.ToLower(name)
		if platform == "" {
			platform = mapTechToPlatform(lower)
		}

		if isEcommerceTech(lower) {
			ecommerce = true
		}
	}

	return stack, platform, ecommerce
}

func (e *Enricher) techClient() (*techFingerprinter, error) {
	e.techOnce.Do(func() {
		raw, err := techFS.ReadFile("technologies.json")
		if err != nil {
			e.techErr = err

			return
		}

		wa, err := webanalyze.NewWebAnalyzer(bytes.NewReader(raw), http.DefaultClient)
		if err != nil {
			e.techErr = err

			return
		}

		e.tech = &techFingerprinter{wa: wa}
	})

	return e.tech, e.techErr
}

func mapTechToPlatform(lower string) string {
	switch {
	case strings.Contains(lower, "shopify"):
		return "shopify"
	case strings.Contains(lower, "woocommerce"):
		return "woocommerce"
	case strings.Contains(lower, "magento"):
		return "magento"
	case strings.Contains(lower, "prestashop"):
		return "prestashop"
	case strings.Contains(lower, "bigcommerce"):
		return "bigcommerce"
	case strings.Contains(lower, "shopware"):
		return "shopware"
	case strings.Contains(lower, "wordpress"):
		return "wordpress"
	case strings.Contains(lower, "squarespace"):
		return "squarespace"
	case strings.Contains(lower, "wix"):
		return "wix"
	case strings.Contains(lower, "webflow"):
		return "webflow"
	case strings.Contains(lower, "drupal"):
		return "drupal"
	case strings.Contains(lower, "joomla"):
		return "joomla"
	case strings.Contains(lower, "typo3"):
		return "typo3"
	default:
		return ""
	}
}

func isEcommerceTech(lower string) bool {
	for _, needle := range []string{
		"shopify", "woocommerce", "magento", "prestashop", "bigcommerce",
		"shopware", "opencart", "salesforce commerce", "demandware",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}

	return false
}
