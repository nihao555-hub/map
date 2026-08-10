// Package intel enriches a CompanyProfile with third-party open-source
// intelligence: email verification (AfterShip/email-verifier), phone
// normalization (nyaruka/phonenumbers), domain WHOIS + MX (likexian/whois),
// tech fingerprints (rverton/webanalyze), GLEIF LEI ownership, and optional
// HTTP sidecars for crawl4ai, gpt-researcher and SpiderFoot.
package intel

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gosom/google-maps-scraper/enrich"
)

// Options controls which external providers run and how they are reached.
// Every provider is optional and fail-open: a timeout or outage leaves the
// profile at whatever page crawling already produced.
type Options struct {
	// EnableSMTP runs live SMTP mailbox probes. Off by default — many hosts
	// rate-limit or tarpit bulk probes, which would dominate scrape latency.
	EnableSMTP bool
	// DefaultRegion is the ISO region hint for national phone numbers (E.164).
	DefaultRegion string

	// GLEIF enables Legal Entity Identifier lookup (free, no API key).
	GLEIF bool
	// TechFingerprint enables webanalyze against the homepage HTML.
	TechFingerprint bool
	// DomainIntel enables WHOIS + MX provider detection.
	DomainIntel bool
	// VerifyEmails enables AfterShip/email-verifier on discovered addresses.
	VerifyEmails bool
	// NormalizePhones enables libphonenumber E.164 normalization.
	NormalizePhones bool

	// Crawl4AIURL is the base URL of a crawl4ai Docker server
	// (e.g. http://localhost:11235). Empty disables the sidecar.
	Crawl4AIURL string
	// ResearcherURL is the base URL of a gpt-researcher server.
	ResearcherURL string
	// SpiderFootURL is the base URL of a SpiderFoot instance.
	SpiderFootURL string

	// HTTPClient is shared by all outbound providers. Nil uses a sane default.
	HTTPClient *http.Client
	// Timeout bounds each individual provider call.
	Timeout time.Duration
}

// Enricher runs the configured providers against a profile.
type Enricher struct {
	opts   Options
	client *http.Client

	emailOnce sync.Once
	emails    *emailVerifier

	techOnce sync.Once
	tech     *techFingerprinter
	techErr  error
}

// New builds an Enricher. Providers that need process-wide state (email
// verifier, webanalyze apps) are initialised lazily on first use.
func New(opts Options) *Enricher {
	if opts.Timeout <= 0 {
		opts.Timeout = 12 * time.Second
	}

	if opts.DefaultRegion == "" {
		opts.DefaultRegion = "US"
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: opts.Timeout}
	}

	return &Enricher{opts: opts, client: client}
}

// DefaultOptions enables every in-process Go provider and leaves the Python
// sidecars off until their URLs are configured.
func DefaultOptions() Options {
	return Options{
		GLEIF:           true,
		TechFingerprint: true,
		DomainIntel:     true,
		VerifyEmails:    true,
		NormalizePhones: true,
		Timeout:         12 * time.Second,
		DefaultRegion:   "US",
	}
}

// Enrich mutates profile in place. It is safe to call with a nil Enricher
// (no-op) so callers can pass one through optionally.
func (e *Enricher) Enrich(ctx context.Context, profile *enrich.CompanyProfile, homepageHTML []byte) {
	if e == nil || profile == nil {
		return
	}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	run := func(enabled bool, fn func(context.Context) error) {
		if !enabled {
			return
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			pctx, cancel := context.WithTimeout(ctx, e.opts.Timeout)
			defer cancel()

			_ = fn(pctx)
		}()
	}

	run(e.opts.VerifyEmails, func(pctx context.Context) error {
		results := e.verifyEmails(pctx, profile.Emails)

		mu.Lock()
		profile.VerifiedEmails = mergeVerified(profile.VerifiedEmails, results)
		e.applyVerificationToEmails(profile)
		mu.Unlock()

		return nil
	})

	run(e.opts.NormalizePhones, func(_ context.Context) error {
		normalized := normalizePhones(profile.Phones, e.opts.DefaultRegion)

		mu.Lock()
		profile.Phones = normalized
		mu.Unlock()

		return nil
	})

	run(e.opts.DomainIntel && profile.Domain != "", func(pctx context.Context) error {
		info := lookupDomain(pctx, profile.Domain)

		mu.Lock()
		if info.MailProvider != "" {
			profile.MailProvider = info.MailProvider
		}

		if len(info.MXHosts) > 0 {
			profile.MXHosts = info.MXHosts
		}

		if info.CreatedAt != "" {
			profile.DomainCreatedAt = info.CreatedAt
		}

		if info.AgeDays > 0 {
			profile.DomainAgeDays = info.AgeDays
		}

		if info.Registrar != "" {
			profile.DomainRegistrar = info.Registrar
		}
		mu.Unlock()

		return nil
	})

	run(e.opts.TechFingerprint && len(homepageHTML) > 0 && profile.HomepageURL != "", func(_ context.Context) error {
		stack, platform, ecommerce := e.fingerprint(profile.HomepageURL, homepageHTML)

		mu.Lock()
		if len(stack) > 0 {
			profile.TechStack = enrich.MergeStringLists(profile.TechStack, stack)
		}

		if platform != "" && profile.Platform == "" {
			profile.Platform = platform
		}

		if ecommerce {
			profile.HasEcommerce = true
		}
		mu.Unlock()

		return nil
	})

	run(e.opts.GLEIF, func(pctx context.Context) error {
		name := profile.LegalName
		if name == "" {
			return nil
		}

		entity, direct, ultimate := lookupGLEIF(pctx, e.client, name)

		mu.Lock()
		if entity != nil {
			profile.LegalEntity = entity
			profile.LEI = entity.LEI

			if profile.LegalName == "" {
				profile.LegalName = entity.LegalName
			}

			profile.RegistrationIDs = append(profile.RegistrationIDs, enrich.RegistrationID{
				Kind:    "lei",
				Value:   entity.LEI,
				Country: entity.Country,
				Source:  "gleif",
			})
		}

		profile.DirectParent = direct
		profile.UltimateParent = ultimate
		mu.Unlock()

		return nil
	})

	run(e.opts.Crawl4AIURL != "" && profile.HomepageURL != "", func(pctx context.Context) error {
		extracted, err := crawl4AIExtract(pctx, e.client, e.opts.Crawl4AIURL, profile.HomepageURL)
		if err != nil || extracted == nil {
			return err
		}

		mu.Lock()
		profile.LLMExtract = extracted
		applyLLMExtract(profile, extracted)
		mu.Unlock()

		return nil
	})

	run(e.opts.ResearcherURL != "" && profile.LegalName != "", func(pctx context.Context) error {
		report, err := runResearcher(pctx, e.client, e.opts.ResearcherURL, profile)
		if err != nil {
			return err
		}

		mu.Lock()
		profile.AIReport = report
		mu.Unlock()

		return nil
	})

	run(e.opts.SpiderFootURL != "" && profile.Domain != "", func(pctx context.Context) error {
		findings, err := runSpiderFoot(pctx, e.client, e.opts.SpiderFootURL, profile.Domain)
		if err != nil {
			return err
		}

		mu.Lock()
		profile.OSINTFindings = append(profile.OSINTFindings, findings...)
		mu.Unlock()

		return nil
	})

	wg.Wait()
}

func applyLLMExtract(profile *enrich.CompanyProfile, extracted *enrich.LLMExtract) {
	if extracted.LegalName != "" {
		if len(extracted.LegalName) > len(profile.LegalName) {
			profile.LegalName = extracted.LegalName
		}
	}

	if extracted.Description != "" && len(extracted.Description) > len(profile.Description) {
		profile.Description = extracted.Description
	}

	if profile.FoundedYear == 0 {
		profile.FoundedYear = extracted.FoundedYear
	}

	if profile.EmployeeRange == "" {
		profile.EmployeeRange = extracted.EmployeeRange
	}

	profile.TradeRoles = enrich.MergeStringLists(profile.TradeRoles, extracted.TradeRoles)
	profile.Certifications = enrich.MergeStringLists(profile.Certifications, extracted.Certifications)
	profile.Markets = enrich.MergeStringLists(profile.Markets, extracted.Markets)
	profile.ProductKeywords = enrich.MergeStringLists(profile.ProductKeywords, extracted.Products)

	for _, address := range extracted.Emails {
		profile.Emails = append(profile.Emails, enrich.Email{Address: address, Source: "crawl4ai"})
	}

	profile.People = append(profile.People, extracted.People...)
}
