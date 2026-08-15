package engine

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestExtractContactsFromHTML(t *testing.T) {
	html := []byte(`
<html><body>
  <a href="https://www.linkedin.com/in/jane-doe">Jane Doe | LinkedIn</a>
  <p>Contact jane.doe@bosch-tools.com for power tools.</p>
  <a href="https://wa.me/6281234567890">Chat on WhatsApp</a>
  <p>Ignore noreply@example.com and logo@cdn.google.com</p>
</body></html>
`)
	hits := extractContactsFromHTML(html, "duckduckgo")
	var sawMail, sawWA bool
	for _, h := range hits {
		if h.Channel == ChannelEmail && h.Contact == "jane.doe@bosch-tools.com" {
			sawMail = true
			if !strings.Contains(h.MessageURL, "mailto:") {
				t.Fatalf("mailto %s", h.MessageURL)
			}
		}
		if h.Channel == ChannelWhatsApp && strings.Contains(h.Contact, "6281234567890") {
			sawWA = true
			if !strings.Contains(h.MessageURL, "wa.me/6281234567890") {
				t.Fatalf("wa %s", h.MessageURL)
			}
		}
		if strings.Contains(h.Contact, "example.com") || strings.Contains(h.Contact, "google.com") {
			t.Fatalf("skipped host leaked %+v", h)
		}
	}
	if !sawMail || !sawWA {
		t.Fatalf("mail=%v wa=%v hits=%+v", sawMail, sawWA, hits)
	}
}

func TestSearchMarketingDisablePublic(t *testing.T) {
	c := &Client{DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "power tools", Kind: KindMarketing})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindMarketing {
		t.Fatalf("kind=%s", res.Kind)
	}
	if len(res.Hits) != 0 {
		t.Fatalf("hits=%+v", res.Hits)
	}
	if !strings.Contains(res.Note, "不会代发") {
		t.Fatalf("note=%s", res.Note)
	}
}

func TestMarketingSearchQueriesCapped(t *testing.T) {
	wanted := map[string]bool{PlatformFacebook: true, PlatformLinkedIn: true, PlatformInstagram: true, PlatformX: true}
	q := marketingSearchQueries("power tools", wanted)
	if len(q) > 5 {
		t.Fatalf("too many queries %v", q)
	}
}

func TestSearchModeMarketingRoutesKind(t *testing.T) {
	c := &Client{DisablePublic: true}
	res, err := c.Search(context.Background(), Query{Keyword: "power tools", Mode: ModeMarketing})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindMarketing {
		t.Fatalf("kind=%s", res.Kind)
	}
	if !strings.Contains(res.Note, "不会代发") {
		t.Fatalf("note=%s", res.Note)
	}
}

func TestSearchMarketingFromPublicHTML(t *testing.T) {
	html := `<html><body>` + strings.Repeat("<!-- pad -->", 40) + `
<a href="https://www.linkedin.com/in/jane-doe">Jane Doe | Power Tools</a>
<p>Contact jane.doe@bosch-tools.com</p>
<a href="https://wa.me/6281234567890">WhatsApp</a>
</body></html>`
	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(html)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
	}

	res, err := c.Search(context.Background(), Query{
		Keyword: "power tools",
		Mode:    ModeMarketing,
		Channel: ChannelEmail,
		Limit:   10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) == 0 {
		t.Fatalf("hits empty warnings=%v", res.Warnings)
	}
	for _, h := range res.Hits {
		if h.Channel != ChannelEmail {
			t.Fatalf("channel filter leaked %+v", h)
		}
		if h.Contact == "" || !strings.Contains(h.MessageURL, "mailto:") {
			t.Fatalf("incomplete email hit %+v", h)
		}
	}
}

func TestSearchMarketingHarvestsLinkedPages(t *testing.T) {
	serp := `<html><body>` + strings.Repeat("<!-- pad -->", 40) + `
<a href="https://factory-tools.com/about">Factory Tools | Power tools manufacturer</a>
</body></html>`
	page := `<html><body>
<title>Contact Factory Tools</title>
<p>Email sales@factory-tools.com</p>
<a href="https://wa.me/14155552671">WhatsApp</a>
</body></html>`

	c := &Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := serp
			if strings.Contains(req.URL.Host, "factory-tools.com") {
				body = page
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    req,
			}, nil
		})},
	}

	res, err := c.Search(context.Background(), Query{
		Keyword: "power tools",
		Kind:    KindMarketing,
		Limit:   10,
	})
	if err != nil {
		t.Fatal(err)
	}

	var sawMail, sawWA bool
	for _, h := range res.Hits {
		if h.Contact == "sales@factory-tools.com" {
			sawMail = true
		}
		if strings.Contains(h.Contact, "14155552671") {
			sawWA = true
		}
	}
	if !sawMail || !sawWA {
		t.Fatalf("harvest mail=%v wa=%v hits=%+v warnings=%v", sawMail, sawWA, res.Hits, res.Warnings)
	}
}
