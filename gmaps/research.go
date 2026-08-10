package gmaps

import (
	"github.com/gosom/scrapemate"

	"github.com/gosom/google-maps-scraper/enrich/intel"
	"github.com/gosom/google-maps-scraper/exiter"
)

// ResearchOptions controls the background-research (背调) crawl that runs
// against a business's own website after the place data is collected.
type ResearchOptions struct {
	// Enabled turns website research on. When it is off the lighter
	// email-only extraction is used instead.
	Enabled bool
	// MaxPages caps how many pages of the company site are fetched, homepage
	// included. Zero means the job's default budget.
	MaxPages int
	// Enricher runs AfterShip email-verifier, phonenumbers, WHOIS/MX,
	// webanalyze, GLEIF and optional Python sidecars. Nil skips external intel.
	Enricher *intel.Enricher
}

// newWebsiteJob returns the follow-up job that mines a business website:
// full company research when enabled, otherwise email-only extraction.
// Returns nil when the website cannot be mined.
func newWebsiteJob(
	parentID string,
	entry *Entry,
	research ResearchOptions,
	exitMonitor exiter.Exiter,
	writerManagedCompletion bool,
) scrapemate.IJob {
	if !entry.IsWebsiteValidForEmail() {
		return nil
	}

	if !research.Enabled {
		opts := []EmailExtractJobOptions{}

		if exitMonitor != nil {
			opts = append(opts, WithEmailJobExitMonitor(exitMonitor))
		}

		if writerManagedCompletion {
			opts = append(opts, WithEmailJobWriterManagedCompletion())
		}

		return NewEmailJob(parentID, entry, opts...)
	}

	opts := []CompanyResearchJobOptions{WithResearchJobMaxPages(research.MaxPages)}

	if exitMonitor != nil {
		opts = append(opts, WithResearchJobExitMonitor(exitMonitor))
	}

	if writerManagedCompletion {
		opts = append(opts, WithResearchJobWriterManagedCompletion())
	}

	if research.Enricher != nil {
		opts = append(opts, WithResearchJobEnricher(research.Enricher))
	}

	return NewCompanyResearchJob(parentID, entry, opts...)
}
