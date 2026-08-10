package enrich_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosom/google-maps-scraper/enrich"
)

func TestDetectTradeRoles(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text string
		want []string
	}{
		"factory": {
			"We are a leading manufacturer with our own factory in Ningbo and offer OEM services.",
			[]string{"manufacturer"},
		},
		"trading company": {
			"Wholesale supplier and distributor for the Benelux region.",
			[]string{"wholesaler", "distributor"},
		},
		"exporter": {
			"We export to more than 40 countries.",
			[]string{"exporter"},
		},
		"chinese factory": {
			"我们是专业的生产厂家，支持批发",
			[]string{"manufacturer", "wholesaler"},
		},
		"no claim": {
			"Welcome to our website. Read our latest news.",
			nil,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.ElementsMatch(t, tt.want, enrich.DetectTradeRoles(tt.text))
		})
	}
}

func TestDetectCertifications(t *testing.T) {
	t.Parallel()

	text := "Our plant is ISO 9001 and ISO-14001 certified, products carry CE marking and are FDA approved. RoHS compliant."

	certifications := enrich.DetectCertifications(text)

	assert.ElementsMatch(t, []string{"ISO 9001", "ISO 14001", "CE", "FDA", "RoHS"}, certifications)
}

func TestDetectCertificationsDoesNotMatchSubstrings(t *testing.T) {
	t.Parallel()

	// "CE" must not fire on the word "CERTIFICATE" alone, and "GS" must not
	// fire on "GSA".
	assert.Empty(t, enrich.DetectCertifications("Download our CERTIFICATE archive. GSA listed."))
}

func TestDetectFoundedYear(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text string
		want int
	}{
		"established":  {"Established in 1987 in Stuttgart", 1987},
		"since":        {"Serving customers since 2004", 2004},
		"german":       {"Gegründet 1962 als Familienbetrieb", 1962},
		"spanish":      {"Empresa fundada en 1998", 1998},
		"chinese":      {"公司成立于2011年", 2011},
		"future year":  {"Established in 2999", 0},
		"street value": {"Suite 1200, Main Street", 0},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, enrich.DetectFoundedYear(tt.text))
		})
	}
}

func TestDetectEmployeeRange(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text string
		want string
	}{
		"range":    {"We have 50 - 100 employees across two sites", "50-100"},
		"over":     {"more than 1,200 employees worldwide", "1200+"},
		"plain":    {"Our 85 employees are our greatest asset", "85+"},
		"chinese":  {"员工200余人", "200+"},
		"no claim": {"We are a small team", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, enrich.DetectEmployeeRange(tt.text))
		})
	}
}

func TestExtractRegistrationIDsFromText(t *testing.T) {
	t.Parallel()

	text := "VAT No: DE 123456789 | Handelsregister: HRB 45678 Amtsgericht Köln | Tax ID: 12-3456789"

	ids := enrich.ExtractRegistrationIDsFromText(text, "https://acme.de/impressum")

	kinds := map[string]string{}
	for _, id := range ids {
		kinds[id.Kind] = id.Value
	}

	require.Contains(t, kinds, "vat")
	assert.Equal(t, "DE 123456789", kinds["vat"])

	for _, id := range ids {
		if id.Kind == "vat" {
			assert.Equal(t, "DE", id.Country)
			assert.Equal(t, "https://acme.de/impressum", id.Source)
		}
	}

	assert.Contains(t, kinds, "registration")
}

func TestExtractRegistrationIDsRejectsShortFragments(t *testing.T) {
	t.Parallel()

	assert.Empty(t, enrich.ExtractRegistrationIDsFromText("Order No: 12", ""))
}

func TestDetectPlatform(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		html          string
		wantPlatform  string
		wantEcommerce bool
	}{
		"shopify": {
			`<script src="https://cdn.shopify.com/s/files/x.js"></script>`,
			"shopify", true,
		},
		"woocommerce beats wordpress": {
			`<link href="/wp-content/plugins/woocommerce/assets/css/woocommerce.css">`,
			"woocommerce", true,
		},
		"plain wordpress": {
			`<link href="/wp-content/themes/acme/style.css">`,
			"wordpress", false,
		},
		"unknown cms with cart": {
			`<button>Add to cart</button>`,
			"", true,
		},
		"nothing": {
			`<html><body><h1>Acme</h1></body></html>`,
			"", false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			platform, ecommerce := enrich.DetectPlatform(tt.html)

			assert.Equal(t, tt.wantPlatform, platform)
			assert.Equal(t, tt.wantEcommerce, ecommerce)
		})
	}
}

func TestDetectMarketsRequiresTradeContext(t *testing.T) {
	t.Parallel()

	withContext := "We export to Germany, France and the United Arab Emirates."
	assert.ElementsMatch(t,
		[]string{"Germany", "France", "United Arab Emirates"},
		enrich.DetectMarkets(withContext),
	)

	// A plain office address is not a market claim.
	assert.Empty(t, enrich.DetectMarkets("Head office: Musterstrasse 1, Germany"))
}

func TestDetectMarketsMatchesOnWordBoundaries(t *testing.T) {
	t.Parallel()

	// "Oman" must not be found inside "Romania".
	markets := enrich.DetectMarkets("Our markets include Romania and Bulgaria.")

	assert.ElementsMatch(t, []string{"Romania", "Bulgaria"}, markets)
}

func TestClassifySeniority(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"CEO":                  enrich.SeniorityExecutive,
		"Managing Director":    enrich.SeniorityExecutive,
		"Co-Founder & CTO":     enrich.SeniorityExecutive,
		"Head of Purchasing":   enrich.SeniorityManagement,
		"Export Sales Manager": enrich.SeniorityManagement,
		"Sales Representative": enrich.SeniorityStaff,
		"Quality Engineer":     enrich.SeniorityStaff,
		"":                     "",
	}

	for title, want := range tests {
		t.Run(title, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, want, enrich.ClassifySeniority(title))
		})
	}
}

func TestExtractPeopleFromText(t *testing.T) {
	t.Parallel()

	text := "Our team: Jane Doe, Export Sales Manager. Managing Director: Klaus Müller. " +
		"Read more about our history and values."

	people := enrich.ExtractPeopleFromText(text, "https://acme.de/team")

	byName := map[string]enrich.Person{}
	for _, person := range people {
		byName[person.Name] = person
	}

	require.Contains(t, byName, "Jane Doe")
	assert.Equal(t, "Export Sales Manager", byName["Jane Doe"].Title)
	assert.Equal(t, enrich.SeniorityManagement, byName["Jane Doe"].Seniority)

	require.Contains(t, byName, "Klaus Müller")
	assert.Equal(t, enrich.SeniorityExecutive, byName["Klaus Müller"].Seniority)
}

func TestExtractPeopleFromTextIgnoresProse(t *testing.T) {
	t.Parallel()

	text := "Privacy Policy, Terms and Conditions. Read More about Our Company - Contact Us"

	assert.Empty(t, enrich.ExtractPeopleFromText(text, ""))
}
