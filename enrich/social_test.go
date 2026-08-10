package enrich_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosom/google-maps-scraper/enrich"
)

func TestExtractSocialsRecognisesProfiles(t *testing.T) {
	t.Parallel()

	hrefs := []string{
		"https://www.linkedin.com/company/acme-tools/",
		"https://linkedin.com/in/jane-doe-12345",
		"https://www.facebook.com/AcmeTools",
		"https://instagram.com/acmetools?utm_source=site",
		"https://x.com/acmetools",
		"https://www.youtube.com/@AcmeTools",
		"https://wa.me/4915112345678",
		"https://www.alibaba.com/company/acme-tools.html",
		"/products",
		"https://acme-tools.com/about",
	}

	socials := enrich.ExtractSocials(hrefs, "https://acme-tools.com/")

	byNetwork := map[string][]enrich.Social{}
	for _, social := range socials {
		byNetwork[social.Network] = append(byNetwork[social.Network], social)
	}

	require.Len(t, byNetwork[enrich.NetworkLinkedIn], 2)
	assert.Equal(t, "https://www.linkedin.com/company/acme-tools", byNetwork[enrich.NetworkLinkedIn][0].URL)
	assert.False(t, byNetwork[enrich.NetworkLinkedIn][0].IsPersonProfile)
	assert.True(t, byNetwork[enrich.NetworkLinkedIn][1].IsPersonProfile)

	require.Len(t, byNetwork[enrich.NetworkInstagram], 1)
	assert.Equal(t, "https://instagram.com/acmetools", byNetwork[enrich.NetworkInstagram][0].URL,
		"tracking parameters must be stripped so the same profile dedupes")
	assert.Equal(t, "acmetools", byNetwork[enrich.NetworkInstagram][0].Handle)

	require.Len(t, byNetwork[enrich.NetworkTwitter], 1)
	require.Len(t, byNetwork[enrich.NetworkWhatsApp], 1)
	require.Len(t, byNetwork[enrich.NetworkAlibaba], 1)

	require.Len(t, byNetwork[enrich.NetworkYouTube], 1)
	assert.Equal(t, "AcmeTools", byNetwork[enrich.NetworkYouTube][0].Handle)
}

func TestExtractSocialsDropsShareWidgetsAndBareDomains(t *testing.T) {
	t.Parallel()

	hrefs := []string{
		"https://www.facebook.com/sharer/sharer.php?u=https://acme-tools.com",
		"https://twitter.com/intent/tweet?url=https://acme-tools.com",
		"https://www.linkedin.com/",
		"https://www.facebook.com/plugins/like.php",
		"https://www.instagram.com/accounts/login/",
	}

	assert.Empty(t, enrich.ExtractSocials(hrefs, "https://acme-tools.com/"))
}

func TestExtractSocialsResolvesProtocolRelativeLinks(t *testing.T) {
	t.Parallel()

	socials := enrich.ExtractSocials([]string{"//www.linkedin.com/company/acme"}, "https://acme-tools.com/")

	require.Len(t, socials, 1)
	assert.Equal(t, enrich.NetworkLinkedIn, socials[0].Network)
}
