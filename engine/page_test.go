package engine

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAssertPublicHTTPURL(t *testing.T) {
	if err := assertPublicHTTPURL("http://127.0.0.1/secret"); err == nil {
		t.Fatal("loopback")
	}
	if err := assertPublicHTTPURL("http://localhost/x"); err == nil {
		t.Fatal("localhost")
	}
	if err := assertPublicHTTPURL("file:///etc/passwd"); err == nil {
		t.Fatal("file")
	}
	if err := assertPublicHTTPURL("https://factory-tools.com/about"); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewSoftFailsOnBlockedPage(t *testing.T) {
	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Body:       io.NopCloser(strings.NewReader("denied")),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
	}
	prev, err := c.Preview(context.Background(), "https://acme-powertools.com/in")
	if err != nil {
		t.Fatal(err)
	}
	if prev.Embeddable || prev.Note == "" {
		t.Fatalf("%+v", prev)
	}
}

func TestPreviewExtractsMetaAndBlocksFrame(t *testing.T) {
	html := `<html><head>
<title>Ignore</title>
<meta property="og:title" content="Acme Power Tools">
<meta property="og:description" content="Factory contact page">
</head><body><p>Write to hello@acme-powertools.com</p></body></html>`

	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			h := make(http.Header)
			h.Set("X-Frame-Options", "DENY")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(html)),
				Header:     h,
				Request:    req,
			}, nil
		})},
	}

	prev, err := c.Preview(context.Background(), "https://acme-powertools.com/contact")
	if err != nil {
		t.Fatal(err)
	}
	if prev.Title != "Acme Power Tools" || prev.Embeddable {
		t.Fatalf("%+v", prev)
	}
	if len(prev.Contacts) == 0 || prev.Contacts[0].Contact != "hello@acme-powertools.com" {
		t.Fatalf("contacts=%+v", prev.Contacts)
	}
}

func TestCandidatePagesFromHTML(t *testing.T) {
	html := []byte(`<a href="https://html.duckduckgo.com/l/?uddg=https%3A%2F%2Ffactory-tools.com%2Fabout">Factory</a>
<a href="https://r.bing.com/rp/foo">junk</a>
<a href="https://outlook.live.com/mail">junk2</a>`)
	pages := candidatePagesFromHTML(html)
	if len(pages) != 1 || !strings.Contains(pages[0], "factory-tools.com") {
		t.Fatalf("pages=%v", pages)
	}
}
