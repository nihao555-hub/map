package enrich_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosom/google-maps-scraper/enrich"
)

func TestExtractEmailsClassifiesByLocalPart(t *testing.T) {
	t.Parallel()

	body := []byte(`
		<p>Sales: sales@acme-tools.com</p>
		<p>General: info@acme-tools.com</p>
		<p>Support: support@acme-tools.com</p>
		<p>Contact: john.smith@acme-tools.com</p>
		<p>Agency: hello@some-agency.io</p>
	`)

	emails := enrich.ExtractEmails(body, "acme-tools.com", "https://acme-tools.com/")

	byAddress := map[string]enrich.Email{}
	for _, email := range emails {
		byAddress[email.Address] = email
	}

	require.Len(t, byAddress, 5)

	assert.Equal(t, enrich.EmailKindRole, byAddress["sales@acme-tools.com"].Kind)
	assert.Equal(t, enrich.EmailKindGeneric, byAddress["info@acme-tools.com"].Kind)
	assert.Equal(t, enrich.EmailKindSupport, byAddress["support@acme-tools.com"].Kind)
	assert.Equal(t, enrich.EmailKindPersonal, byAddress["john.smith@acme-tools.com"].Kind)

	assert.True(t, byAddress["sales@acme-tools.com"].OnDomain)
	assert.False(t, byAddress["hello@some-agency.io"].OnDomain, "third-party agency address must not count as on-domain")
}

func TestExtractEmailsTreatsSubdomainAsOnDomain(t *testing.T) {
	t.Parallel()

	emails := enrich.ExtractEmails([]byte("export@mail.acme.co.uk"), "acme.co.uk", "")

	require.Len(t, emails, 1)
	assert.True(t, emails[0].OnDomain)
}

func TestExtractEmailsDeobfuscates(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want string
	}{
		"at and dot words":  {"write to export (at) acme-tools (dot) com today", "export@acme-tools.com"},
		"bracketed at":      {"purchasing[at]acme-tools.com", "purchasing@acme-tools.com"},
		"spaced around at":  {"sales @ acme-tools.com", "sales@acme-tools.com"},
		"curly brace style": {"info{at}acme-tools{dot}com", "info@acme-tools.com"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			emails := enrich.ExtractEmails([]byte(tt.body), "acme-tools.com", "")

			addresses := make([]string, 0, len(emails))
			for _, email := range emails {
				addresses = append(addresses, email.Address)
			}

			assert.Contains(t, addresses, tt.want)
		})
	}
}

func TestExtractEmailsDecodesCloudflareObfuscation(t *testing.T) {
	t.Parallel()

	// "sales@acme.com" XORed with key 0x7a, prefixed by the key itself.
	plain := "sales@acme.com"
	const key = byte(0x7a)

	encoded := "7a"
	for i := 0; i < len(plain); i++ {
		encoded += string("0123456789abcdef"[(plain[i]^key)>>4]) + string("0123456789abcdef"[(plain[i]^key)&0x0f])
	}

	body := []byte(`<a href="/cdn-cgi/l/email-protection" data-cfemail="` + encoded + `">email</a>`)

	emails := enrich.ExtractEmails(body, "acme.com", "")

	require.Len(t, emails, 1)
	assert.Equal(t, "sales@acme.com", emails[0].Address)
}

func TestExtractEmailsDropsNoise(t *testing.T) {
	t.Parallel()

	body := []byte(`
		<img src="logo@2x.png">
		<span>you@example.com</span>
		<span>someone@yourdomain.com</span>
		<script>Sentry.init({dsn:"https://abc@o123.ingest.sentry.io/1"})</script>
		<span>1a2b3c4d5e6f7a8b@acme.com</span>
	`)

	emails := enrich.ExtractEmails(body, "acme.com", "")

	assert.Empty(t, emails)
}

func TestExtractMailtoEmails(t *testing.T) {
	t.Parallel()

	hrefs := []string{
		"mailto:Export@Acme-Tools.com?subject=Quote%20request",
		"mailto:a@acme-tools.com,b@acme-tools.com",
		"mailto:%69%6e%66%6f@acme-tools.com",
		"/contact",
		"tel:+4930123456",
	}

	emails := enrich.ExtractMailtoEmails(hrefs, "acme-tools.com", "https://acme-tools.com/")

	addresses := make([]string, 0, len(emails))
	for _, email := range emails {
		addresses = append(addresses, email.Address)
	}

	assert.ElementsMatch(t, []string{
		"export@acme-tools.com",
		"a@acme-tools.com",
		"b@acme-tools.com",
		"info@acme-tools.com",
	}, addresses)
}
