package outreach

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	aiMinBodyChars    = 120
	aiMaxBodyChars    = 1800
	aiMaxSubjectChars = 78
	aiThreadTailSize  = 6
)

// expertSystemPrompt is the persona used for every writing task. The rules
// target the two failure modes that kill cold-email replies: sounding like a
// template robot and sounding like an LLM.
const expertSystemPrompt = `You are a senior international-trade business development manager with 15+ years of B2B export experience. You have personally closed deals across Europe, North America and Asia, and you write all outreach yourself.

How you write:
- Short, direct, warm but businesslike. You respect the reader's time.
- You reference one or two concrete facts about the prospect's business and connect them to a benefit for THEM. You never flatter emptily.
- Plain text only. No markdown, no bullet lists, no emojis, no ALL CAPS, no exclamation marks.
- One low-friction question as the call to action. Never more than one ask.
- You never fabricate numbers, clients, certifications or claims. If no proof is provided, you simply do not mention proof.
- You never mention AI, tools, scraping, databases, or how you found their contact details beyond a natural phrase like coming across their business.

Strictly forbidden phrases and habits (they scream mass-mail or AI): "I hope this email finds you well", "I trust this finds you", "delve", "furthermore", "moreover", "in today's fast-paced world", "unlock", "elevate", "seamless", "leverage", "cutting-edge", "revolutionize", "game-changer", "I came across your esteemed company", "Dear Sir/Madam", "To whom it may concern", em-dash chains, three-part parallel slogans, and any sentence over 30 words.

Write like a busy human expert typed it in two minutes: small natural variations, contractions where natural, and one clear thought per sentence.`

// EmailFacts carries everything the model may use for one contact.
type EmailFacts struct {
	Contact  *Contact
	Campaign *Campaign
	Settings *Settings
	Research string
	Thread   []Message
}

// GeneratedEmail is a validated AI writing result.
type GeneratedEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// GenerateStepEmail writes one sequence step for one contact. Callers must
// treat errors as a signal to fall back to the deterministic templates.
func GenerateStepEmail(
	ctx context.Context,
	ai AICompleter,
	facts *EmailFacts,
	stepIndex int,
) (GeneratedEmail, error) {
	if ai == nil {
		return GeneratedEmail{}, errors.New("AI writer is not available")
	}

	prompt := buildStepPrompt(facts, stepIndex)

	raw, err := ai.Complete(ctx, facts.Settings, expertSystemPrompt, prompt)
	if err != nil {
		return GeneratedEmail{}, err
	}

	var email GeneratedEmail
	if err := extractJSONObject(raw, &email); err != nil {
		return GeneratedEmail{}, err
	}

	email.Subject = strings.TrimSpace(email.Subject)
	email.Body = strings.TrimSpace(email.Body)

	if err := validateGeneratedEmail(&email, stepIndex == 0); err != nil {
		return GeneratedEmail{}, err
	}

	return email, nil
}

func buildStepPrompt(facts *EmailFacts, stepIndex int) string {
	var b strings.Builder

	b.WriteString("Write ")

	switch stepIndex {
	case 0:
		b.WriteString("the FIRST cold email (60-120 words) to this prospect. It opens a new thread, so also write a subject line: lowercase-feeling, specific, under 60 characters, no clickbait, no brackets.")
	case 1:
		b.WriteString("a SHORT polite follow-up (40-80 words) in the same thread. The prospect did not answer the first email. Add one small new angle or detail; do not guilt-trip and do not repeat the first email.")
	default:
		b.WriteString("the FINAL brief break-up email (40-80 words) in the same thread. Politely close the loop, make it easy to say no, and leave the door open to reply later. No passive aggression.")
	}

	b.WriteString("\n\nReturn ONLY a JSON object: {\"subject\": \"...\", \"body\": \"...\"}. For follow-ups the subject field may be an empty string. The body must be plain text with real line breaks, a greeting using the business name, and this exact sign-off on its own lines: ")
	b.WriteString(fmt.Sprintf("%q", signatureFor(facts.Settings)))

	b.WriteString("\n\n## Prospect facts (the only facts you may use)\n")
	writeFact(&b, "Business name", facts.Contact.Name)
	writeFact(&b, "Business type", facts.Contact.Category)
	writeFact(&b, "City", facts.Contact.City)
	writeFact(&b, "Address", facts.Contact.Address)
	writeFact(&b, "Website", facts.Contact.Website)
	writeFact(&b, "Google rating", ratingLine(facts.Contact))

	if facts.Research != "" {
		b.WriteString("\n## Notes from their website (verbatim extract, may be messy)\n")
		b.WriteString(facts.Research)
		b.WriteString("\n")
	}

	b.WriteString("\n## What we offer them\n")
	writeFact(&b, "Sender", facts.Settings.FromName)
	writeFact(&b, "Company", facts.Settings.SenderCompany)
	writeFact(&b, "Outcome we deliver", facts.Campaign.ValueProposition)
	writeFact(&b, "Verifiable proof (optional, use only if natural)", facts.Campaign.Proof)
	writeFact(&b, "Preferred call to action", facts.Campaign.CallToAction)

	if thread := threadTranscript(facts.Thread, facts.Settings.EmailAddress); thread != "" {
		b.WriteString("\n## Emails already exchanged in this thread (oldest first)\n")
		b.WriteString(thread)
	}

	return b.String()
}

// SuggestReply drafts a human-quality answer to the customer's latest reply.
// The result is a draft for the operator to review, never sent automatically.
func SuggestReply(
	ctx context.Context,
	ai AICompleter,
	facts *EmailFacts,
) (string, error) {
	if ai == nil {
		return "", errors.New("AI writer is not available")
	}

	lastInbound := lastInboundMessage(facts.Thread)
	if lastInbound == nil {
		return "", errors.New("this contact has no reply to answer yet")
	}

	var b strings.Builder

	b.WriteString("The prospect below replied to our outreach. Draft the next reply for me to review.\n")
	b.WriteString("Rules: answer their actual questions first; be honest about anything we do not know; propose exactly one concrete next step (a time, a document, or a clarifying question); keep it under 150 words; plain text only.\n")
	b.WriteString("Write the reply in the same language the prospect used. End with this sign-off on its own lines: ")
	b.WriteString(fmt.Sprintf("%q", signatureFor(facts.Settings)))
	b.WriteString("\nReturn ONLY the email body text, no JSON, no subject, no commentary.\n")

	b.WriteString("\n## Prospect\n")
	writeFact(&b, "Business name", facts.Contact.Name)
	writeFact(&b, "Business type", facts.Contact.Category)
	writeFact(&b, "City", facts.Contact.City)

	b.WriteString("\n## What we offer\n")
	writeFact(&b, "Outcome we deliver", facts.Campaign.ValueProposition)
	writeFact(&b, "Verifiable proof", facts.Campaign.Proof)

	if thread := threadTranscript(facts.Thread, facts.Settings.EmailAddress); thread != "" {
		b.WriteString("\n## Full thread (oldest first)\n")
		b.WriteString(thread)
	}

	raw, err := ai.Complete(ctx, facts.Settings, expertSystemPrompt, b.String())
	if err != nil {
		return "", err
	}

	reply := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "`"))
	if len(reply) < 20 {
		return "", errors.New("AI returned an empty reply draft")
	}

	if err := rejectRobotVoice(reply); err != nil {
		return "", err
	}

	return reply, nil
}

// aiIntentPrompt asks for machine-readable output; the visible labels stay in
// Chinese because that is what the operator reads in the workspace.
const aiIntentPrompt = `You are a veteran export sales manager reviewing a reply from a cold-email prospect. Classify the buying intent.

Return ONLY a JSON object:
{"score": <0-100 integer>, "label": "<高意向|中意向|低意向|无意向>", "reason": "<one short Chinese sentence quoting the decisive signal>"}

Scoring guide:
- 80-100 高意向: asks about price, quotation, MOQ, samples, catalogs, lead time, or proposes a call/meeting.
- 55-79 中意向: engaged questions or a request to be contacted later at a specific time.
- 25-54 低意向: polite brush-off, vague interest, wrong person but gave a pointer.
- 0-24 无意向: explicit not interested, unsubscribe, hostility, or pure auto-reply with no signal.`

// ClassifyIntentAI scores a reply with the model, falling back to an error
// that callers should replace with HeuristicIntent.
func ClassifyIntentAI(
	ctx context.Context,
	ai AICompleter,
	settings *Settings,
	reply *Message,
) (Intent, error) {
	if ai == nil {
		return Intent{}, errors.New("AI classifier is not available")
	}

	user := fmt.Sprintf("Subject: %s\n\nReply body:\n%s", reply.Subject, clipRunes(reply.Body, 2500))

	raw, err := ai.Complete(ctx, settings, aiIntentPrompt, user)
	if err != nil {
		return Intent{}, err
	}

	var intent Intent
	if err := extractJSONObject(raw, &intent); err != nil {
		return Intent{}, err
	}

	if intent.Score < 0 || intent.Score > 100 || !validIntentLabel(intent.Label) {
		return Intent{}, fmt.Errorf("AI returned invalid intent: %+v", intent)
	}

	intent.Reason = clipRunes(strings.TrimSpace(intent.Reason), 120)

	return intent, nil
}

var robotVoiceTells = []string{
	"i hope this email finds you well",
	"i trust this finds you",
	"as an ai", "language model",
	"to whom it may concern",
	"dear sir/madam", "dear sir or madam",
	"esteemed company",
	"in today's fast-paced",
	"{{", "}}", "[insert", "[your", "```",
}

func rejectRobotVoice(text string) error {
	lower := strings.ToLower(text)

	for _, tell := range robotVoiceTells {
		if strings.Contains(lower, tell) {
			return fmt.Errorf("AI output rejected: contains mass-mail tell %q", tell)
		}
	}

	return nil
}

func validateGeneratedEmail(email *GeneratedEmail, needSubject bool) error {
	if needSubject {
		if email.Subject == "" {
			return errors.New("AI output rejected: missing subject")
		}

		if len(email.Subject) > aiMaxSubjectChars {
			return errors.New("AI output rejected: subject too long")
		}

		if err := rejectRobotVoice(email.Subject); err != nil {
			return err
		}
	}

	if len(email.Body) < aiMinBodyChars || len(email.Body) > aiMaxBodyChars {
		return fmt.Errorf("AI output rejected: body length %d out of range", len(email.Body))
	}

	return rejectRobotVoice(email.Body)
}

func signatureFor(settings *Settings) string {
	name := settings.FromName
	if name == "" {
		name = settings.EmailAddress
	}

	if settings.SenderCompany != "" {
		return name + "\n" + settings.SenderCompany
	}

	return name
}

func writeFact(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	b.WriteString("- ")
	b.WriteString(label)
	b.WriteString(": ")
	b.WriteString(clipRunes(value, 400))
	b.WriteString("\n")
}

func ratingLine(contact *Contact) string {
	if contact.Rating == "" || contact.ReviewCount == 0 {
		return ""
	}

	return fmt.Sprintf("%s from %d reviews", contact.Rating, contact.ReviewCount)
}

func threadTranscript(thread []Message, senderEmail string) string {
	if len(thread) == 0 {
		return ""
	}

	if len(thread) > aiThreadTailSize {
		thread = thread[len(thread)-aiThreadTailSize:]
	}

	var b strings.Builder

	for i := range thread {
		message := &thread[i]

		role := "PROSPECT"
		if message.Direction == DirectionOut || strings.EqualFold(message.FromEmail, senderEmail) {
			role = "US"
		}

		fmt.Fprintf(&b, "[%s] %s\n%s\n\n", role, message.Subject, clipRunes(message.Body, 900))
	}

	return b.String()
}

func lastInboundMessage(thread []Message) *Message {
	for i := len(thread) - 1; i >= 0; i-- {
		if thread[i].Direction == DirectionIn {
			return &thread[i]
		}
	}

	return nil
}

func clipRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}

	return string(runes[:limit]) + "…"
}
