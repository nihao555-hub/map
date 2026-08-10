package runner

import (
	"os"

	"github.com/gosom/google-maps-scraper/enrich/intel"
)

// NewResearchEnricher builds the shared external-intel enricher from Config.
// Returns nil when company research is disabled so callers can pass it through
// unconditionally.
func NewResearchEnricher(cfg *Config) *intel.Enricher {
	if cfg == nil || !cfg.CompanyResearch {
		return nil
	}

	opts := intel.DefaultOptions()
	opts.EnableSMTP = cfg.EmailSMTPVerify
	opts.DefaultRegion = cfg.PhoneRegion
	opts.Crawl4AIURL = firstNonEmpty(cfg.Crawl4AIURL, os.Getenv("CRAWL4AI_URL"))
	opts.ResearcherURL = firstNonEmpty(cfg.ResearcherURL, os.Getenv("RESEARCHER_URL"))
	opts.SpiderFootURL = firstNonEmpty(cfg.SpiderFootURL, os.Getenv("SPIDERFOOT_URL"))
	opts.GLEIF = !cfg.DisableGLEIF
	opts.TechFingerprint = !cfg.DisableTechFingerprint
	opts.DomainIntel = !cfg.DisableDomainIntel
	opts.VerifyEmails = !cfg.DisableEmailVerify

	return intel.New(opts)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
