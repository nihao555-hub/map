package gmaps_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gosom/scrapemate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosom/google-maps-scraper/enrich"
	"github.com/gosom/google-maps-scraper/gmaps"
)

const researchHomepage = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta name="description" content="Nordic Marine Supplies AS supplies deck equipment to shipyards.">
</head>
<body>
  <a href="/contact">Contact</a>
  <a href="/about">About us</a>
  <a href="/catalogue.pdf">Catalogue</a>
  <a href="https://www.linkedin.com/company/nordic-marine">LinkedIn</a>
  <p>We are a wholesale distributor and export to Norway, Sweden and Denmark.</p>
  <p>ISO 9001 certified. Established in 1996.</p>
</body>
</html>`

const researchContactPage = `<!DOCTYPE html>
<html lang="en">
<body>
  <h1>Contact</h1>
  <p>Purchasing: <a href="mailto:purchasing@nordic-marine.example">purchasing@nordic-marine.example</a></p>
  <p>General: <a href="mailto:post@nordic-marine.example">post@nordic-marine.example</a></p>
  <p>Phone: <a href="tel:+4722334455">+47 22 33 44 55</a></p>
  <p>WhatsApp: <a href="https://wa.me/4791234567">chat</a></p>
  <div class="member"><h3>Ingrid Larsen</h3><p>Managing Director</p></div>
</body>
</html>`

const researchAboutPage = `<!DOCTYPE html>
<html lang="en">
<body>
  <h1>About us</h1>
  <p>Organisasjonsnummer / VAT No: NO 912345678 MVA</p>
  <p>We employ 40 - 60 employees at our Bergen warehouse.</p>
</body>
</html>`

// newResearchServer serves a small company site. It records which paths were
// requested so tests can assert on the crawl budget.
func newResearchServer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()

	var requested []string

	mux := http.NewServeMux()

	serve := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			requested = append(requested, r.URL.Path)

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(body))
		}
	}

	mux.HandleFunc("/", serve(researchHomepage))
	mux.HandleFunc("/contact", serve(researchContactPage))
	mux.HandleFunc("/about", serve(researchAboutPage))
	mux.HandleFunc("/catalogue.pdf", func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4"))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, &requested
}

// runResearch drives the job the way scrapemate would: the homepage response is
// supplied by the fetcher, the rest is fetched by the job itself.
func runResearch(t *testing.T, entry *gmaps.Entry, homepageURL string, opts ...gmaps.CompanyResearchJobOptions) *gmaps.Entry {
	t.Helper()

	res, err := http.Get(homepageURL) //nolint:gosec,noctx // test server URL
	require.NoError(t, err)

	defer func() {
		_ = res.Body.Close()
	}()

	var body strings.Builder

	doc, err := goquery.NewDocumentFromReader(io.TeeReader(res.Body, &body))
	require.NoError(t, err)

	job := gmaps.NewCompanyResearchJob("parent-id", entry, opts...)

	result, next, err := job.Process(context.Background(), &scrapemate.Response{
		URL:      homepageURL,
		Body:     []byte(body.String()),
		Document: doc,
	})
	require.NoError(t, err)
	assert.Empty(t, next, "research must complete inside one job so the entry is written once")

	got, ok := result.(*gmaps.Entry)
	require.True(t, ok)

	return got
}

func TestCompanyResearchJobBuildsProfileFromMultiplePages(t *testing.T) {
	t.Parallel()

	srv, requested := newResearchServer(t)

	entry := &gmaps.Entry{Title: "Nordic Marine Supplies", WebSite: srv.URL}

	got := runResearch(t, entry, srv.URL)

	profile := got.CompanyProfile
	require.NotNil(t, profile)

	assert.Contains(t, *requested, "/contact")
	assert.Contains(t, *requested, "/about")
	assert.NotContains(t, *requested, "/catalogue.pdf", "asset downloads must not consume the crawl budget")

	assert.Contains(t, profile.Description, "deck equipment")
	assert.Equal(t, 1996, profile.FoundedYear)
	assert.Equal(t, "40-60", profile.EmployeeRange)
	assert.Subset(t, profile.TradeRoles, []string{"wholesaler", "distributor", "exporter"})
	assert.Contains(t, profile.Certifications, "ISO 9001")
	assert.Subset(t, profile.Markets, []string{"Norway", "Sweden", "Denmark"})

	addresses := profile.EmailAddresses()
	assert.Contains(t, addresses, "purchasing@nordic-marine.example")
	assert.Contains(t, addresses, "post@nordic-marine.example")

	assert.Equal(t, "purchasing@nordic-marine.example", got.BestEmail(),
		"the purchasing mailbox outranks the general one")
	assert.Equal(t, addresses, got.Emails,
		"the flat emails column must stay populated for existing consumers")

	var found bool

	for _, person := range profile.People {
		if person.Name == "Ingrid Larsen" {
			found = true

			assert.Equal(t, enrich.SeniorityExecutive, person.Seniority)
		}
	}

	assert.True(t, found, "decision maker from the contact page must be captured")

	var vat string

	for _, id := range profile.RegistrationIDs {
		if id.Kind == "vat" {
			vat = id.Value
		}
	}

	assert.Contains(t, vat, "912345678")

	assert.Greater(t, profile.Completeness, 50)
	assert.Len(t, profile.PagesCrawled, 3)
}

func TestCompanyResearchJobRespectsPageBudget(t *testing.T) {
	t.Parallel()

	srv, requested := newResearchServer(t)

	entry := &gmaps.Entry{Title: "Nordic Marine Supplies", WebSite: srv.URL}

	// A budget of 2 means homepage plus exactly one follow-up page.
	got := runResearch(t, entry, srv.URL, gmaps.WithResearchJobMaxPages(2))

	require.NotNil(t, got.CompanyProfile)
	assert.Len(t, got.CompanyProfile.PagesCrawled, 2)

	assert.Contains(t, *requested, "/contact", "the contact page has the highest research value")
	assert.NotContains(t, *requested, "/about")
}

func TestCompanyResearchJobHomepageOnly(t *testing.T) {
	t.Parallel()

	srv, requested := newResearchServer(t)

	entry := &gmaps.Entry{Title: "Nordic Marine Supplies", WebSite: srv.URL}

	got := runResearch(t, entry, srv.URL, gmaps.WithResearchJobMaxPages(1))

	require.NotNil(t, got.CompanyProfile)
	assert.Len(t, got.CompanyProfile.PagesCrawled, 1)
	assert.Equal(t, []string{"/"}, *requested)
}

func TestCompanyResearchJobKeepsEntryOnFetchError(t *testing.T) {
	t.Parallel()

	entry := &gmaps.Entry{Title: "Unreachable Ltd", WebSite: "https://unreachable.invalid"}

	job := gmaps.NewCompanyResearchJob("parent-id", entry)

	assert.True(t, job.ProcessOnFetchError())

	result, next, err := job.Process(context.Background(), &scrapemate.Response{
		URL:   "https://unreachable.invalid",
		Error: assert.AnError,
	})

	require.NoError(t, err, "an unreachable website must not fail the job")
	assert.Empty(t, next)

	got, ok := result.(*gmaps.Entry)
	require.True(t, ok)
	assert.Equal(t, "Unreachable Ltd", got.Title)
	assert.Nil(t, got.CompanyProfile)
}

func TestCompanyResearchJobFallsBackToGuessedPaths(t *testing.T) {
	t.Parallel()

	// A homepage whose navigation is rendered client-side exposes no links.
	var requested []string

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)

		w.Header().Set("Content-Type", "text/html")

		if r.URL.Path == "/contact" {
			_, _ = w.Write([]byte(researchContactPage))

			return
		}

		_, _ = w.Write([]byte(`<html><body><div id="app"></div></body></html>`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	entry := &gmaps.Entry{Title: "SPA Corp", WebSite: srv.URL}

	got := runResearch(t, entry, srv.URL, gmaps.WithResearchJobMaxPages(2))

	require.NotNil(t, got.CompanyProfile)
	assert.Contains(t, requested, "/contact")
	assert.Contains(t, got.CompanyProfile.EmailAddresses(), "purchasing@nordic-marine.example")
}
