package outreach

// DefaultSequence returns the built-in three-step cold email sequence.
//
// The copy follows well-documented cold-email practices that drive replies:
//   - short (under ~120 words), plain text, no images or link farms
//   - a personalized first line built from the scraped business data
//   - talks about the recipient's problem, not the sender's company
//   - one specific, low-friction call to action (a question, not a pitch)
//   - polite follow-ups in the same thread with 3-4 day gaps
//   - a "breakup" final email, which historically gets the best reply rate
//     of the whole sequence
//
// All strings are Go text/template snippets rendered per contact. Available
// variables: {{.Name}} {{.FirstLine}} {{.Category}} {{.City}} {{.Address}}
// {{.Website}} {{.Phone}} {{.SenderName}} {{.SenderCompany}} {{.SenderEmail}}.
func DefaultSequence() []SequenceStep {
	return []SequenceStep{
		{
			Subject: "Quick question about {{.Name}}",
			Body: `Hi {{.Name}} team,

{{.FirstLine}}

We {{.ValueProposition}}.

{{if .Proof}}{{.Proof}}

{{end}}{{.CallToAction}} If you are the wrong person for this, I'd appreciate a pointer to the right one.

Best regards,
{{.SenderName}}
{{.SenderCompany}}`,
			DelayDays: 0,
		},
		{
			Body: `Hi again,

I know things get busy, so just floating this back to the top of your inbox.

The short version: we {{.ValueProposition}}, and I think there may be a fit for {{.Name}}{{if .City}} in {{.City}}{{end}}.

Would a quick chat this week work?

Best,
{{.SenderName}}`,
			DelayDays: 3,
		},
		{
			Body: `Hi,

I haven't heard back, so I'll assume the timing isn't right and stop emailing you about this.

If growing {{.Name}} becomes a priority later, just reply to this email — happy to share a couple of ideas specific to your business, no strings attached.

Wishing you a great season,
{{.SenderName}}
{{.SenderCompany}}`,
			DelayDays: 4,
		},
	}
}

// firstLineTemplates are used to build a personalized opening line from the
// scraped data when the operator does not write one manually.
const defaultFirstLine = `I came across {{.Name}}{{if .City}} in {{.City}}{{end}} on Google Maps` +
	`{{if .GoodRating}} — a {{.Rating}} rating from {{.ReviewCount}} reviews really stands out{{end}}.`
