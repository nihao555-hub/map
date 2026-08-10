package enrich_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosom/google-maps-scraper/enrich"
)

func TestSelectFollowUpPagesPrioritisesContactPages(t *testing.T) {
	t.Parallel()

	hrefs := []string{
		"/produkte",
		"/team",
		"/impressum",
		"/kontakt",
		"/ueber-uns",
	}

	selected := enrich.SelectFollowUpPages("https://muster-werkzeugbau.de/", hrefs, 5)

	assert.Equal(t, []string{
		"https://muster-werkzeugbau.de/kontakt",
		"https://muster-werkzeugbau.de/impressum",
		"https://muster-werkzeugbau.de/ueber-uns",
		"https://muster-werkzeugbau.de/team",
		"https://muster-werkzeugbau.de/produkte",
	}, selected)
}

func TestSelectFollowUpPagesRespectsLimit(t *testing.T) {
	t.Parallel()

	hrefs := []string{"/produkte", "/team", "/impressum", "/kontakt"}

	selected := enrich.SelectFollowUpPages("https://muster-werkzeugbau.de/", hrefs, 2)

	assert.Equal(t, []string{
		"https://muster-werkzeugbau.de/kontakt",
		"https://muster-werkzeugbau.de/impressum",
	}, selected)
}

func TestSelectFollowUpPagesFiltersUnusableLinks(t *testing.T) {
	t.Parallel()

	hrefs := []string{
		"/katalog.pdf",                           // asset download
		"https://other-company.com/contact",      // off-site
		"#kontakt",                               // fragment only
		"javascript:void(0)",                     // handler
		"mailto:info@muster-werkzeugbau.de",      // not a page
		"/blog/2026/news",                        // no research value
		"/",                                      // the homepage itself
		"https://muster-werkzeugbau.de/kontakt/", // trailing slash
		"https://muster-werkzeugbau.de/kontakt",  // duplicate
		"/kontakt?utm_source=nav",                // duplicate with tracking
	}

	selected := enrich.SelectFollowUpPages("https://muster-werkzeugbau.de/", hrefs, 10)

	assert.Equal(t, []string{"https://muster-werkzeugbau.de/kontakt"}, selected)
}

func TestSelectFollowUpPagesAcceptsSubdomains(t *testing.T) {
	t.Parallel()

	selected := enrich.SelectFollowUpPages(
		"https://www.acme.co.uk/",
		[]string{"https://shop.acme.co.uk/contact-us"},
		3,
	)

	require.Len(t, selected, 1)
	assert.Equal(t, "https://shop.acme.co.uk/contact-us", selected[0])
}

func TestSelectFollowUpPagesReturnsNothingWithoutBudget(t *testing.T) {
	t.Parallel()

	assert.Empty(t, enrich.SelectFollowUpPages("https://acme.com/", []string{"/contact"}, 0))
}

func TestRegistrableDomain(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"www.acme.com":       "acme.com",
		"shop.acme.com":      "acme.com",
		"acme.co.uk":         "acme.co.uk",
		"shop.acme.co.uk":    "acme.co.uk",
		"deep.shop.acme.com": "acme.com",
		"acme.de":            "acme.de",
		"localhost":          "localhost",
		"":                   "",
	}

	for host, want := range tests {
		t.Run(host, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, want, enrich.RegistrableDomain(host))
		})
	}
}

func TestHostFromURL(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"https://www.acme.com/contact?x=1": "acme.com",
		"http://ACME.com":                  "acme.com",
		"acme.com/path":                    "acme.com",
		"":                                 "",
	}

	for rawURL, want := range tests {
		t.Run(rawURL, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, want, enrich.HostFromURL(rawURL))
		})
	}
}
