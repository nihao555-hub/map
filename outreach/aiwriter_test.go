package outreach_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosom/google-maps-scraper/outreach"
)

type fakeAI struct {
	response string
	err      error
	lastUser string
}

func (f *fakeAI) Complete(_ context.Context, _ *outreach.Settings, _, user string) (string, error) {
	f.lastUser = user

	return f.response, f.err
}

const (
	testSenderName    = "Alex"
	testSenderCompany = "Example Co"
	testSenderEmail   = "alex@example.com"
	testAIModel       = "test-model"
	testAIKey         = "test-key"
)

func aiTestFacts() outreach.EmailFacts {
	settings := outreach.DefaultSettings()
	settings.FromName = testSenderName
	settings.SenderCompany = testSenderCompany
	settings.EmailAddress = testSenderEmail
	settings.AIBaseURL = "https://ai.example.com/v1"
	settings.AIModel = testAIModel
	settings.AIAPIKey = testAIKey

	contact := outreach.Contact{
		Name:        "Acme Dental",
		Category:    "dentist",
		City:        "Berlin",
		Website:     "https://acme.example",
		Rating:      "4.8",
		ReviewCount: 130,
	}

	campaign := outreach.Campaign{
		ValueProposition: "help dental clinics turn website visits into appointments",
		Proof:            "A similar clinic booked 18% more patients in 60 days.",
		CallToAction:     "Would a quick conversation be useful?",
	}

	return outreach.EmailFacts{
		Contact:  &contact,
		Campaign: &campaign,
		Settings: &settings,
		Research: "Title: Acme Dental\nDescription: Family dentistry in Berlin.",
	}
}

func TestGenerateStepEmailParsesAndValidates(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("We looked at your Berlin practice and one idea stood out. ", 3) +
		"Would a quick conversation be useful?\n\nAlex\nExample Co"
	ai := &fakeAI{response: `{"subject": "idea for Acme Dental", "body": ` + jsonString(body) + `}`}

	facts := aiTestFacts()

	email, err := outreach.GenerateStepEmail(context.Background(), ai, &facts, 0)
	if err != nil {
		t.Fatal(err)
	}

	if email.Subject != "idea for Acme Dental" {
		t.Fatalf("unexpected subject %q", email.Subject)
	}

	if !strings.Contains(ai.lastUser, "Acme Dental") || !strings.Contains(ai.lastUser, "Family dentistry") {
		t.Fatal("prompt is missing prospect facts or research")
	}
}

func TestGenerateStepEmailRejectsRobotVoice(t *testing.T) {
	t.Parallel()

	body := "I hope this email finds you well. " + strings.Repeat("Generic filler sentence here. ", 8)
	ai := &fakeAI{response: `{"subject": "hello", "body": ` + jsonString(body) + `}`}

	facts := aiTestFacts()

	if _, err := outreach.GenerateStepEmail(context.Background(), ai, &facts, 0); err == nil {
		t.Fatal("expected robot-voice output to be rejected")
	}
}

func TestClassifyIntentAI(t *testing.T) {
	t.Parallel()

	ai := &fakeAI{response: "```json\n{\"score\": 88, \"label\": \"高意向\", \"reason\": \"客户索要报价\"}\n```"}
	settings := aiTestFacts()

	reply := outreach.Message{Subject: "Re: hi", Body: "Please send your price list and MOQ."}

	intent, err := outreach.ClassifyIntentAI(context.Background(), ai, settings.Settings, &reply)
	if err != nil {
		t.Fatal(err)
	}

	if intent.Score != 88 || intent.Label != "高意向" {
		t.Fatalf("unexpected intent: %+v", intent)
	}
}

func TestEvaluateEmailParsesAndClamps(t *testing.T) {
	t.Parallel()

	ai := &fakeAI{response: "```json\n{\"overall\": 86, \"subject_appeal\": 85, \"relevance\": 90, " +
		"\"personalization\": 80, \"call_to_action\": 120, \"readability\": 88, " +
		"\"suggestion\": \"下次跟进提供更具体的案例或数据\"}\n```"}

	facts := aiTestFacts()

	evaluation, err := outreach.EvaluateEmail(
		context.Background(),
		ai,
		facts.Settings,
		facts.Contact,
		"Quick idea to help Acme Corp",
		"Hi Michael, ...",
	)
	if err != nil {
		t.Fatal(err)
	}

	if evaluation.Overall != 86 || evaluation.Relevance != 90 {
		t.Fatalf("unexpected evaluation: %+v", evaluation)
	}

	if evaluation.CallToAction != 100 {
		t.Fatalf("score above 100 should clamp to 100, got %d", evaluation.CallToAction)
	}

	if evaluation.Grade() != "优秀" {
		t.Fatalf("overall 86 should grade 优秀, got %q", evaluation.Grade())
	}
}

func TestSuggestReplyNeedsInbound(t *testing.T) {
	t.Parallel()

	ai := &fakeAI{response: "Thanks for getting back to me."}
	facts := aiTestFacts()

	if _, err := outreach.SuggestReply(context.Background(), ai, &facts); err == nil {
		t.Fatal("expected error when the contact never replied")
	}

	facts.Thread = []outreach.Message{
		{Direction: outreach.DirectionOut, Subject: "hi", Body: "opener"},
		{Direction: outreach.DirectionIn, Subject: "Re: hi", Body: "What is your price?"},
	}
	ai.response = "Thanks for the quick reply. Our pricing starts at ... Would Tuesday work?\n\nAlex\nExample Co"

	draft, err := outreach.SuggestReply(context.Background(), ai, &facts)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(draft, "Tuesday") {
		t.Fatalf("unexpected draft: %q", draft)
	}
}

func TestAIClientTalksToOpenAICompatibleEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)

			return
		}

		if r.Header.Get("Authorization") != "Bearer "+testAIKey {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	t.Cleanup(server.Close)

	settings := outreach.DefaultSettings()
	settings.AIBaseURL = server.URL + "/v1"
	settings.AIModel = testAIModel
	settings.AIAPIKey = testAIKey

	client := outreach.NewAIClient()

	answer, err := client.Complete(context.Background(), &settings, "system", "ping")
	if err != nil {
		t.Fatal(err)
	}

	if answer != "pong" {
		t.Fatalf("unexpected answer %q", answer)
	}
}

func TestAIClientSurfacesEndpointErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	t.Cleanup(server.Close)

	settings := outreach.DefaultSettings()
	settings.AIBaseURL = server.URL
	settings.AIModel = testAIModel
	settings.AIAPIKey = testAIKey

	client := outreach.NewAIClient()

	_, err := client.Complete(context.Background(), &settings, "system", "ping")
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected endpoint error, got %v", err)
	}
}

func jsonString(value string) string {
	var b strings.Builder

	b.WriteByte('"')

	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}

	b.WriteByte('"')

	return b.String()
}
