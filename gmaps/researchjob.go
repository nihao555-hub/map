package gmaps

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
	"github.com/gosom/scrapemate"
	"golang.org/x/sync/errgroup"

	"github.com/gosom/google-maps-scraper/enrich"
	"github.com/gosom/google-maps-scraper/enrich/intel"
	"github.com/gosom/google-maps-scraper/exiter"
)

// Company research crawl budget. The homepage arrives through the normal
// scrapemate fetcher; the follow-up pages are fetched inside Process so the
// whole profile is written as a single result. The cap keeps the tail latency
// of a research job comparable to the email job it replaces.
const (
	defaultResearchMaxPages    = 4
	defaultResearchPageTimeout = 12 * time.Second
	// researchFetchConcurrency bounds parallel requests against one company
	// site. Small sites sit behind modest hosting, so staying polite here also
	// avoids the rate limiting that would cost us the whole profile.
	researchFetchConcurrency = 2
	// maxResearchBodyBytes caps how much of a page we read. Company pages that
	// matter are far smaller; the limit protects against multi-megabyte
	// single-page-app bundles.
	maxResearchBodyBytes = 4 << 20
)

// Follow-up page fetches fail for mundane reasons. They are reported as
// sentinel errors so the crawl can skip the page and keep the rest of the
// profile instead of failing the whole job.
var (
	errUnexpectedStatus = errors.New("unexpected status code")
	errNotHTML          = errors.New("response is not html")
	errTooManyRedirects = errors.New("too many redirects")
)

// CompanyResearchJobOptions configures a CompanyResearchJob.
type CompanyResearchJobOptions func(*CompanyResearchJob)

// CompanyResearchJob builds a background-research profile (背调) for one
// business from its own website: contact channels, decision-maker candidates,
// registration identifiers, certifications and trade-role signals.
//
// It supersedes EmailExtractJob when company research is enabled: the profile
// includes every address the email job would have found, plus the context that
// makes an address worth using.
type CompanyResearchJob struct {
	scrapemate.Job

	Entry                   *Entry
	ExitMonitor             exiter.Exiter
	WriterManagedCompletion bool

	// MaxPages caps how many pages (homepage included) are fetched.
	MaxPages int
	// PageTimeout bounds each follow-up request.
	PageTimeout time.Duration

	// Enricher runs AfterShip email-verifier, phonenumbers, WHOIS/MX,
	// webanalyze, GLEIF and optional Python sidecars against the profile.
	Enricher *intel.Enricher

	// client is injected by tests; production uses a lazily built default.
	client *http.Client
}

// NewCompanyResearchJob creates a research job for entry's website.
func NewCompanyResearchJob(parentID string, entry *Entry, opts ...CompanyResearchJobOptions) *CompanyResearchJob {
	const (
		defaultPrio       = scrapemate.PriorityHigh
		defaultMaxRetries = 0
	)

	job := CompanyResearchJob{
		Job: scrapemate.Job{
			ID:         uuid.New().String(),
			ParentID:   parentID,
			Method:     http.MethodGet,
			URL:        normalizeGoogleURL(entry.WebSite),
			MaxRetries: defaultMaxRetries,
			Priority:   defaultPrio,
		},
		Entry:       entry,
		MaxPages:    defaultResearchMaxPages,
		PageTimeout: defaultResearchPageTimeout,
	}

	for _, opt := range opts {
		opt(&job)
	}

	return &job
}

// WithResearchJobExitMonitor reports completion to the exit monitor.
func WithResearchJobExitMonitor(exitMonitor exiter.Exiter) CompanyResearchJobOptions {
	return func(j *CompanyResearchJob) {
		j.ExitMonitor = exitMonitor
	}
}

// WithResearchJobWriterManagedCompletion leaves completion accounting to the
// writer, matching the email job's behaviour.
func WithResearchJobWriterManagedCompletion() CompanyResearchJobOptions {
	return func(j *CompanyResearchJob) {
		j.WriterManagedCompletion = true
	}
}

// WithResearchJobMaxPages overrides the crawl budget. Values below 1 are
// ignored so a misconfiguration cannot disable research entirely.
func WithResearchJobMaxPages(pages int) CompanyResearchJobOptions {
	return func(j *CompanyResearchJob) {
		if pages > 0 {
			j.MaxPages = pages
		}
	}
}

// WithResearchJobEnricher attaches the external-intel enricher (email-verifier,
// phonenumbers, WHOIS/MX, webanalyze, GLEIF, optional sidecars).
func WithResearchJobEnricher(enricher *intel.Enricher) CompanyResearchJobOptions {
	return func(j *CompanyResearchJob) {
		j.Enricher = enricher
	}
}

// WithResearchJobClient injects the HTTP client used for follow-up pages.
func WithResearchJobClient(client *http.Client) CompanyResearchJobOptions {
	return func(j *CompanyResearchJob) {
		j.client = client
	}
}

// ProcessOnFetchError keeps the entry in the output even when its website is
// unreachable — a lead without a working site is still a lead.
func (j *CompanyResearchJob) ProcessOnFetchError() bool {
	return true
}

// Process analyses the homepage, then fetches the highest-value follow-up pages
// and merges everything into Entry.CompanyProfile.
func (j *CompanyResearchJob) Process(ctx context.Context, resp *scrapemate.Response) (any, []scrapemate.IJob, error) {
	defer func() {
		resp.Document = nil
		resp.Body = nil
	}()

	defer func() {
		if j.ExitMonitor != nil && !j.WriterManagedCompletion {
			j.ExitMonitor.IncrPlacesCompleted(1)
		}
	}()

	log := scrapemate.GetLoggerFromContext(ctx)

	log.Info("Processing company research job", "url", j.URL)

	if resp.Error != nil {
		return j.Entry, nil, nil
	}

	doc, ok := resp.Document.(*goquery.Document)
	if !ok {
		return j.Entry, nil, nil
	}

	homepageURL := j.homepageURL(resp)
	companyDomain := enrich.RegistrableDomain(enrich.HostFromURL(homepageURL))

	profile := enrich.AnalyzePage(enrich.Page{
		URL:  homepageURL,
		HTML: resp.Body,
		Doc:  doc,
	}, companyDomain)

	profile.HomepageURL = homepageURL
	profile.Domain = companyDomain

	for _, page := range j.fetchFollowUpPages(ctx, homepageURL, doc, companyDomain) {
		profile.Merge(page)
	}

	if profile.LegalName == "" {
		profile.LegalName = j.Entry.Title
	}

	// External intel: email-verifier, phonenumbers, WHOIS/MX, webanalyze,
	// GLEIF, and optional crawl4ai / gpt-researcher / SpiderFoot sidecars.
	if j.Enricher != nil {
		j.Enricher.Enrich(ctx, profile, resp.Body)
	}

	profile.Finalize()

	j.Entry.CompanyProfile = profile

	// Keep the flat `emails` column populated so existing exports, the web UI
	// and downstream integrations keep working unchanged.
	if addresses := profile.EmailAddresses(); len(addresses) > 0 {
		j.Entry.Emails = addresses
	}

	return j.Entry, nil, nil
}

// homepageURL prefers the URL the fetcher actually landed on, so redirects to
// a canonical host or locale are reflected in the profile.
func (j *CompanyResearchJob) homepageURL(resp *scrapemate.Response) string {
	if resp.URL != "" {
		return resp.URL
	}

	return j.GetURL()
}

func (j *CompanyResearchJob) fetchFollowUpPages(
	ctx context.Context,
	homepageURL string,
	doc *goquery.Document,
	companyDomain string,
) []*enrich.CompanyProfile {
	budget := j.MaxPages - 1
	if budget <= 0 {
		return nil
	}

	var hrefs []string

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			hrefs = append(hrefs, href)
		}
	})

	targets := enrich.SelectFollowUpPages(homepageURL, hrefs, budget)
	if len(targets) == 0 {
		targets = j.guessFollowUpPages(homepageURL, budget)
	}

	if len(targets) == 0 {
		return nil
	}

	var (
		mu       sync.Mutex
		profiles []*enrich.CompanyProfile
	)

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(researchFetchConcurrency)

	for _, target := range targets {
		group.Go(func() error {
			page, err := j.fetchPage(groupCtx, target)
			if err != nil {
				// A missing sub-page is normal, not a job failure: research
				// reports whatever the company actually published.
				return nil
			}

			analyzed := enrich.AnalyzePage(*page, companyDomain)

			mu.Lock()
			profiles = append(profiles, analyzed)
			mu.Unlock()

			return nil
		})
	}

	// The goroutines above never return an error, so Wait cannot fail.
	_ = group.Wait()

	return profiles
}

// guessFollowUpPages covers sites whose navigation is rendered client-side and
// therefore exposes no crawlable links in the initial HTML.
func (j *CompanyResearchJob) guessFollowUpPages(homepageURL string, budget int) []string {
	base := strings.TrimRight(homepageURL, "/")
	if base == "" {
		return nil
	}

	guesses := make([]string, 0, budget)

	for _, path := range enrich.CommonContactPaths {
		if len(guesses) >= budget {
			break
		}

		guesses = append(guesses, base+path)
	}

	return guesses
}

func (j *CompanyResearchJob) fetchPage(ctx context.Context, target string) (*enrich.Page, error) {
	reqCtx, cancel := context.WithTimeout(ctx, j.pageTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", researchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en;q=0.9,*;q=0.5")

	resp, err := j.httpClient().Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, errUnexpectedStatus
	}

	if contentType := resp.Header.Get("Content-Type"); contentType != "" && !isHTMLContentType(contentType) {
		return nil, errNotHTML
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResearchBodyBytes))
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	finalURL := target
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return &enrich.Page{URL: finalURL, HTML: body, Doc: doc}, nil
}

func (j *CompanyResearchJob) pageTimeout() time.Duration {
	if j.PageTimeout > 0 {
		return j.PageTimeout
	}

	return defaultResearchPageTimeout
}

func (j *CompanyResearchJob) httpClient() *http.Client {
	if j.client != nil {
		return j.client
	}

	return defaultResearchClient()
}

// researchUserAgent identifies the crawler as a normal desktop browser: many
// small-business hosts block unknown agents outright, which would leave the
// profile empty.
const researchUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// maxResearchRedirects bounds redirect chains; marketing stacks occasionally
// build loops between locale variants.
const maxResearchRedirects = 5

var (
	researchClientOnce sync.Once
	researchClient     *http.Client
)

func defaultResearchClient() *http.Client {
	researchClientOnce.Do(func() {
		researchClient = &http.Client{
			Timeout: defaultResearchPageTimeout,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= maxResearchRedirects {
					return errTooManyRedirects
				}

				return nil
			},
		}
	})

	return researchClient
}

func isHTMLContentType(contentType string) bool {
	lower := strings.ToLower(contentType)

	return strings.Contains(lower, "text/html") ||
		strings.Contains(lower, "application/xhtml") ||
		strings.Contains(lower, "text/plain")
}
