package enrich_test

import (
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosom/google-maps-scraper/enrich"
)

const homepageURL = "https://muster-werkzeugbau.de/"

func loadFixture(t *testing.T, name string) enrich.Page {
	t.Helper()

	body, err := os.ReadFile("testdata/" + name)
	require.NoError(t, err)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	require.NoError(t, err)

	return enrich.Page{URL: homepageURL, HTML: body, Doc: doc}
}

func TestAnalyzePageIdentity(t *testing.T) {
	t.Parallel()

	profile := enrich.AnalyzePage(loadFixture(t, "company_home.html"), "muster-werkzeugbau.de")

	assert.Equal(t, "Muster Werkzeugbau Gesellschaft mit beschränkter Haftung", profile.LegalName,
		"JSON-LD legalName is more specific than og:site_name and must win")
	assert.Contains(t, profile.Description, "German manufacturer of precision cutting tools")
	assert.Equal(t, 1978, profile.FoundedYear)
	assert.Equal(t, "120-180", profile.EmployeeRange)
	assert.Equal(t, "wordpress", profile.Platform)
	assert.Equal(t, []string{homepageURL}, profile.PagesCrawled)
}

func TestAnalyzePageContacts(t *testing.T) {
	t.Parallel()

	profile := enrich.AnalyzePage(loadFixture(t, "company_home.html"), "muster-werkzeugbau.de")
	profile.Finalize()

	byAddress := map[string]enrich.Email{}
	for _, email := range profile.Emails {
		byAddress[email.Address] = email
	}

	require.Contains(t, byAddress, "vertrieb@muster-werkzeugbau.de")
	require.Contains(t, byAddress, "info@muster-werkzeugbau.de")
	require.Contains(t, byAddress, "export@muster-werkzeugbau.de",
		"obfuscated '(at)' address must be recovered")

	assert.True(t, byAddress["vertrieb@muster-werkzeugbau.de"].OnDomain)
	assert.False(t, byAddress["hello@agency.example.org"].OnDomain,
		"the web agency's own address must not be treated as first-party")

	// On-domain, role-style addresses must sort ahead of the agency's address.
	assert.True(t, profile.Emails[0].OnDomain)

	var whatsapp bool

	for _, phone := range profile.Phones {
		if phone.WhatsApp {
			whatsapp = true

			assert.Equal(t, "+4915112345678", phone.E164)
		}
	}

	assert.True(t, whatsapp, "wa.me link must be recorded as a WhatsApp number")
}

func TestAnalyzePageSocials(t *testing.T) {
	t.Parallel()

	profile := enrich.AnalyzePage(loadFixture(t, "company_home.html"), "muster-werkzeugbau.de")

	networks := map[string]string{}
	for _, social := range profile.Socials {
		networks[social.Network] = social.URL
	}

	assert.Equal(t, "https://www.linkedin.com/company/muster-werkzeugbau", networks[enrich.NetworkLinkedIn])
	assert.Contains(t, networks, enrich.NetworkFacebook)
	assert.Contains(t, networks, enrich.NetworkInstagram)
	assert.Contains(t, networks, enrich.NetworkYouTube)

	for _, social := range profile.Socials {
		assert.NotContains(t, social.URL, "sharer", "share widgets must be filtered out")
	}
}

func TestAnalyzePageCommercialSignals(t *testing.T) {
	t.Parallel()

	profile := enrich.AnalyzePage(loadFixture(t, "company_home.html"), "muster-werkzeugbau.de")

	assert.Subset(t, profile.TradeRoles, []string{"manufacturer", "wholesaler", "exporter"})
	assert.Subset(t, profile.Certifications, []string{"ISO 9001", "ISO 14001", "CE", "RoHS"})
	assert.Subset(t, profile.Markets, []string{"Germany", "France", "United States", "United Arab Emirates"})
	assert.ElementsMatch(t, []string{"de", "en", "fr"}, profile.Languages)
	assert.Contains(t, profile.ProductKeywords, "carbide end mills")
}

func TestAnalyzePageRegistrationIDs(t *testing.T) {
	t.Parallel()

	profile := enrich.AnalyzePage(loadFixture(t, "company_home.html"), "muster-werkzeugbau.de")

	kinds := map[string]bool{}
	for _, id := range profile.RegistrationIDs {
		kinds[id.Kind] = true

		if id.Kind == "vat" {
			assert.Equal(t, "DE", id.Country)
		}
	}

	assert.True(t, kinds["vat"], "USt-IdNr must be captured as a VAT number")
	assert.True(t, kinds["registration"], "Handelsregister number must be captured")
}

func TestAnalyzePagePeople(t *testing.T) {
	t.Parallel()

	profile := enrich.AnalyzePage(loadFixture(t, "company_home.html"), "muster-werkzeugbau.de")
	profile.Finalize()

	byName := map[string]enrich.Person{}
	for _, person := range profile.People {
		byName[person.Name] = person
	}

	require.Contains(t, byName, "Klaus Weber")
	assert.Equal(t, "Managing Director", byName["Klaus Weber"].Title)
	assert.Equal(t, enrich.SeniorityExecutive, byName["Klaus Weber"].Seniority)

	require.Contains(t, byName, "Sabine Fischer")
	assert.Equal(t, enrich.SeniorityManagement, byName["Sabine Fischer"].Seniority)

	require.Contains(t, byName, "Heinrich Muster", "JSON-LD founder must be included")

	// Executives sort first so the most senior contact is the obvious one.
	assert.Equal(t, enrich.SeniorityExecutive, profile.People[0].Seniority)
}

func TestAnalyzePageIsHighlyComplete(t *testing.T) {
	t.Parallel()

	profile := enrich.AnalyzePage(loadFixture(t, "company_home.html"), "muster-werkzeugbau.de")
	profile.Finalize()

	assert.GreaterOrEqual(t, profile.Completeness, 90,
		"a site publishing contacts, people, socials and registration data should score near the top")
	assert.False(t, profile.IsEmpty())
	assert.False(t, profile.ResearchedAt.IsZero())
}

func TestAnalyzePageHandlesMissingDocument(t *testing.T) {
	t.Parallel()

	profile := enrich.AnalyzePage(enrich.Page{URL: homepageURL}, "")

	assert.True(t, profile.IsEmpty())
}

func TestVisibleTextExcludesScripts(t *testing.T) {
	t.Parallel()

	text := enrich.VisibleText(loadFixture(t, "company_home.html").Doc)

	assert.Contains(t, text, "Precision Carbide End Mills")
	assert.NotContains(t, text, "application/ld+json")
	assert.NotContains(t, text, "wp-content")
}
