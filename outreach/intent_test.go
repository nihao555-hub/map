package outreach_test

import (
	"strings"
	"testing"

	"github.com/gosom/google-maps-scraper/outreach"
)

func TestHeuristicIntent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    string
		subject string
		body    string
		want    string
	}{
		{
			name: "price question is high intent",
			kind: outreach.InboundKindReply,
			body: "Hi, could you send your price list and MOQ?",
			want: outreach.IntentHigh,
		},
		{
			name: "chinese quote request is high intent",
			kind: outreach.InboundKindReply,
			body: "你好，请发一下报价和样品信息",
			want: outreach.IntentHigh,
		},
		{
			name: "explicit rejection is no intent",
			kind: outreach.InboundKindReply,
			body: "Not interested, please remove me.",
			want: outreach.IntentNone,
		},
		{
			name:    "out of office stays pending",
			kind:    outreach.InboundKindReply,
			subject: "Automatic reply: out of office",
			body:    "I am on vacation until Monday.",
			want:    outreach.IntentPending,
		},
		{
			name: "bounce marks invalid",
			kind: outreach.InboundKindBounce,
			want: outreach.IntentInvalid,
		},
		{
			name: "generic reply needs human review",
			kind: outreach.InboundKindReply,
			body: "Who gave you this address?",
			want: outreach.IntentMedium,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			intent := outreach.HeuristicIntent(test.kind, test.subject, test.body)
			if intent.Label != test.want {
				t.Fatalf("HeuristicIntent() label = %q, want %q (intent %+v)", intent.Label, test.want, intent)
			}
		})
	}
}

func TestSummarizeHTML(t *testing.T) {
	t.Parallel()

	html := `<html><head><title>Acme Dental Berlin</title>
<meta name="description" content="Family dentistry &amp; implants since 1999">
<style>body{color:red}</style></head>
<body><script>evil()</script><h1>Welcome to Acme</h1><p>We serve 10,000 patients.</p></body></html>`

	summary := outreach.SummarizeHTML(html)

	for _, want := range []string{
		"Title: Acme Dental Berlin",
		"Description: Family dentistry & implants since 1999",
		"Welcome to Acme",
		"10,000 patients",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}

	if strings.Contains(summary, "evil()") || strings.Contains(summary, "color:red") {
		t.Fatalf("summary leaked script/style content:\n%s", summary)
	}
}
