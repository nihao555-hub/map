package outreach

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/template"
)

// RenderContext carries the per-contact variables available to sequence
// templates.
type RenderContext struct {
	Name        string
	Category    string
	City        string
	Address     string
	Website     string
	Phone       string
	Rating      string
	ReviewCount string
	FirstLine   string

	ValueProposition string
	Proof            string
	CallToAction     string

	SenderName    string
	SenderCompany string
	SenderEmail   string
}

// GoodRating reports whether the lead has a rating worth mentioning.
func (r *RenderContext) GoodRating() bool {
	rating, err := strconv.ParseFloat(r.Rating, 64)
	if err != nil {
		return false
	}

	count, err := strconv.Atoi(r.ReviewCount)
	if err != nil {
		return false
	}

	return rating >= 4.0 && count > 0
}

// NewRenderContext builds the template context for one contact.
func NewRenderContext(c *Contact, s *Settings, campaigns ...*Campaign) RenderContext {
	ctx := RenderContext{
		Name:          c.Name,
		Category:      c.Category,
		City:          c.City,
		Address:       c.Address,
		Website:       c.Website,
		Phone:         c.Phone,
		Rating:        c.Rating,
		ReviewCount:   strconv.Itoa(c.ReviewCount),
		SenderName:    s.FromName,
		SenderCompany: s.SenderCompany,
		SenderEmail:   s.EmailAddress,
	}

	if ctx.SenderName == "" {
		ctx.SenderName = s.EmailAddress
	}

	if len(campaigns) > 0 && campaigns[0] != nil {
		ctx.ValueProposition = campaigns[0].ValueProposition
		ctx.Proof = campaigns[0].Proof
		ctx.CallToAction = campaigns[0].CallToAction
	}

	if ctx.CallToAction == "" {
		ctx.CallToAction = "Would it be worth a quick 10-minute chat to see if this fits " + ctx.Name + "?"
	}

	firstLine, err := renderTemplate("firstline", defaultFirstLine, &ctx)
	if err == nil {
		ctx.FirstLine = firstLine
	}

	return ctx
}

// RenderStep renders one sequence step for a contact. For follow-up steps the
// subject falls back to "Re: <thread subject>" so the email lands in the same
// conversation.
func RenderStep(step *SequenceStep, ctx *RenderContext, threadSubject string) (subject, body string, err error) {
	if step.Subject != "" {
		subject, err = renderTemplate("subject", step.Subject, ctx)
		if err != nil {
			return "", "", err
		}
	} else {
		subject = threadSubject
		if !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}
	}

	body, err = renderTemplate("body", step.Body, ctx)
	if err != nil {
		return "", "", err
	}

	return subject, body, nil
}

func renderTemplate(name, text string, ctx *RenderContext) (string, error) {
	tmpl, err := template.New(name).Option("missingkey=zero").Parse(text)
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", name, err)
	}

	var buf bytes.Buffer

	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("render template %s: %w", name, err)
	}

	return strings.TrimSpace(buf.String()), nil
}

// ValidateSequenceTemplates parses every step so broken templates are
// rejected at campaign creation time instead of at send time.
func ValidateSequenceTemplates(steps []SequenceStep) error {
	sample := RenderContext{
		Name:             "Example Business",
		Category:         "restaurant",
		City:             "Berlin",
		ValueProposition: "help restaurants get more qualified bookings",
		CallToAction:     "Would a quick conversation be useful?",
	}

	for i := range steps {
		if steps[i].Subject != "" {
			if _, err := renderTemplate("subject", steps[i].Subject, &sample); err != nil {
				return fmt.Errorf("step %d subject: %w", i+1, err)
			}
		}

		if _, err := renderTemplate("body", steps[i].Body, &sample); err != nil {
			return fmt.Errorf("step %d body: %w", i+1, err)
		}
	}

	return nil
}
