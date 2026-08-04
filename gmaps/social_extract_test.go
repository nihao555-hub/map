//nolint:testpackage // exercises unexported extractSocialFromHTML
package gmaps

import "testing"

func TestExtractSocialFromHTML(t *testing.T) {
	html := []byte(`
		<a href="https://facebook.com/shop">fb</a>
		<a href="https://www.linkedin.com/company/acme">li</a>
		<a href="https://instagram.com/acme">ig</a>
		<a href="https://tiktok.com/@acme">tt</a>
	`)
	got := extractSocialFromHTML(html)
	if got.Facebook == "" || got.LinkedIn == "" || got.Instagram == "" || got.TikTok == "" {
		t.Fatalf("incomplete social extract: %+v", got)
	}
}
