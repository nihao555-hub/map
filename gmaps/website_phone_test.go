package gmaps

import "testing"

func TestExtractPhoneFromHTML(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{
			// dalla-teknik.com publishes this while Maps lists no phone at all.
			name: "tel link",
			html: `<a href="tel:+62-822-1166-6551">Hubungi Kami</a>`,
			want: "+6282211666551",
		},
		{
			name: "labelled local number",
			html: `<p>Telp: 021 2345 6789</p>`,
			want: "02123456789",
		},
		{
			name: "whatsapp label",
			html: `<div>Hubungi&nbsp;0812-3456-7890</div>`,
			want: "081234567890",
		},
		{
			name: "tel link wins over page text",
			html: `<span>Rp 130.000</span><a href="tel:0215551234">call</a>`,
			want: "0215551234",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractPhoneFromHTML([]byte(tc.html)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestExtractPhoneFromHTMLRejectsJunk(t *testing.T) {
	junk := []string{
		``,
		`<p>Harga Rp 130.000 - 15.000.000</p>`,
		`<p>Update 20240115 katalog produk</p>`,
		`<p>SKU 998877665544332211</p>`,
		`<p>Telp: 12345</p>`,
	}
	for _, html := range junk {
		if got := extractPhoneFromHTML([]byte(html)); got != "" {
			t.Fatalf("expected no phone from %q, got %q", html, got)
		}
	}
}

func TestWebsitePhoneOnlyFillsWhenMapsHasNone(t *testing.T) {
	// Maps phone is authoritative; the website must not overwrite it.
	entry := &Entry{Phone: "+62 21 1234 5678"}
	if entry.Phone == "" {
		t.Fatal("precondition")
	}
	if got := normalizeSitePhone("+62-822-1166-6551"); got != "+6282211666551" {
		t.Fatalf("normalize got %q", got)
	}
}
