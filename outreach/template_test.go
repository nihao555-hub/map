package outreach_test

import (
	"strings"
	"testing"

	"github.com/gosom/google-maps-scraper/outreach"
)

func TestDefaultSequenceRendersPersonalizedThread(t *testing.T) {
	t.Parallel()

	settings := outreach.DefaultSettings()
	settings.FromName = "Alex"
	settings.SenderCompany = "Example Co"
	settings.EmailAddress = "alex@example.com"

	contact := outreach.Contact{
		Name:        "Acme Dental",
		Category:    "dental",
		City:        "Berlin",
		Rating:      "4.8",
		ReviewCount: 130,
	}
	campaign := outreach.Campaign{
		ValueProposition: "help dental clinics turn more website visits into booked appointments",
		CallToAction:     "Would a quick conversation be useful?",
	}

	context := outreach.NewRenderContext(&contact, &settings, &campaign)
	sequence := outreach.DefaultSequence()

	subject, body, err := outreach.RenderStep(&sequence[0], &context, "")
	if err != nil {
		t.Fatal(err)
	}

	if subject != "Quick question about Acme Dental" {
		t.Fatalf("unexpected subject: %q", subject)
	}

	if !strings.Contains(body, "4.8 rating from 130 reviews") {
		t.Fatalf("body lacks rating personalization: %q", body)
	}

	followUpSubject, _, err := outreach.RenderStep(&sequence[1], &context, subject)
	if err != nil {
		t.Fatal(err)
	}

	if followUpSubject != "Re: "+subject {
		t.Fatalf("follow-up subject = %q", followUpSubject)
	}
}
