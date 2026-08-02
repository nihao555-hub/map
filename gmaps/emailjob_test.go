package gmaps

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/gosom/scrapemate"
	"github.com/stretchr/testify/require"
)

func TestFilterEmailsRemovesJunk(t *testing.T) {
	t.Parallel()

	in := []string{
		"info@shop.com",
		"abc123@sentry.io",
		"user@domain.com",
		"photo@cdn.com.png",
		"deadbeefdeadbeefdeadbeefdeadbeef@analytics.test",
		"INFO@Shop.com",
	}

	out := filterEmails(in)
	require.Equal(t, []string{"info@shop.com"}, out)
}

func TestObfuscatedEmailExtractor(t *testing.T) {
	t.Parallel()

	body := []byte(`contact us at hello [at] example-shop [dot] com`)
	obf := obfuscatedEmailExtractor(body)
	require.Contains(t, obf, "hello@example-shop.com")
}

func TestDecodeCloudflareEmail(t *testing.T) {
	t.Parallel()

	// Encode "info@shop.com" with key 0x4e
	plain := []byte("info@shop.com")
	key := byte(0x4e)
	encoded := make([]byte, 0, 1+len(plain))
	encoded = append(encoded, key)

	for _, b := range plain {
		encoded = append(encoded, b^key)
	}

	require.Equal(t, "info@shop.com", decodeCloudflareEmail(hex.EncodeToString(encoded)))
}

func TestDiscoverEmailFollowURLs(t *testing.T) {
	t.Parallel()

	html := `<html><body>
		<a href="/menu">Menu</a>
		<a href="/contact-us">Contact Us</a>
		<a href="https://other.com/contact">Other</a>
		<a href="/about">About</a>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	resp := &scrapemate.Response{Document: doc}
	urls := discoverEmailFollowURLs("https://shop.example/", resp)
	require.NotEmpty(t, urls)

	joined := strings.Join(urls, ",")
	require.Contains(t, joined, "https://shop.example/contact-us")
	require.Contains(t, joined, "https://shop.example/about")
	require.NotContains(t, joined, "other.com")
	require.LessOrEqual(t, len(urls), emailMaxFollowPages)
}

func TestIsWebsiteValidForEmailDenylist(t *testing.T) {
	t.Parallel()

	cases := []struct {
		url  string
		want bool
	}{
		{"https://shop.example", true},
		{"https://facebook.com/shop", false},
		{"https://www.instagram.com/shop", false},
		{"https://order.toasttab.com/shop", false},
		{"http://foo.mobile-webview4.com/", false},
	}

	for _, tc := range cases {
		e := &Entry{WebSite: tc.url}
		require.Equal(t, tc.want, e.IsWebsiteValidForEmail(), tc.url)
	}
}
