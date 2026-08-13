package outreach_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosom/google-maps-scraper/outreach"
)

func brevoSettings(key string) outreach.Settings {
	settings := outreach.DefaultSettings()
	settings.EmailAddress = testSenderEmail
	settings.FromName = testSenderName
	settings.SendVia = outreach.SendViaAPI
	settings.SendAPIKey = key

	return settings
}

func TestBrevoMailerSendAdoptsProviderMessageID(t *testing.T) {
	t.Parallel()

	var got map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v3/smtp/email" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		if r.Header.Get("api-key") != "xkeysib-test" {
			t.Errorf("api key header missing, got %q", r.Header.Get("api-key"))
		}

		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode payload: %v", err)
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"messageId":"<202608@smtp-relay.mailin.fr>"}`))
	}))
	t.Cleanup(server.Close)

	mailer := outreach.NewBrevoMailerForTest(server.URL)
	settings := brevoSettings("xkeysib-test")

	message := outreach.Message{
		ToEmail:   "buyer@example.org",
		Subject:   "Quick question",
		Body:      "Hello there",
		MessageID: "<local@huali.freeqiye.com>",
		InReplyTo: "<root@huali.freeqiye.com>",
	}

	if err := mailer.Send(context.Background(), &settings, &message); err != nil {
		t.Fatalf("send: %v", err)
	}

	if message.MessageID != "<202608@smtp-relay.mailin.fr>" {
		t.Fatalf("provider message id not adopted: %q", message.MessageID)
	}

	headers, ok := got["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers missing from payload: %v", got)
	}

	if headers["In-Reply-To"] != "<root@huali.freeqiye.com>" {
		t.Fatalf("threading headers not forwarded: %v", headers)
	}

	if _, ok := headers["List-Unsubscribe"]; !ok {
		t.Fatalf("List-Unsubscribe header missing: %v", headers)
	}

	text, _ := got["textContent"].(string)
	if !strings.Contains(text, "unsubscribe") {
		t.Fatalf("unsubscribe footer missing from body: %q", text)
	}

	replyTo, _ := got["replyTo"].(map[string]any)
	if replyTo["email"] != testSenderEmail {
		t.Fatalf("replyTo should stay the operator mailbox: %v", got["replyTo"])
	}
}

func TestBrevoMailerUnauthorizedIsMailboxLevel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthorized","message":"Key not found"}`))
	}))
	t.Cleanup(server.Close)

	mailer := outreach.NewBrevoMailerForTest(server.URL)
	settings := brevoSettings("bad-key")
	message := outreach.Message{ToEmail: "buyer@example.org", Subject: "s", Body: "b"}

	err := mailer.Send(context.Background(), &settings, &message)
	if err == nil {
		t.Fatal("expected error for unauthorized key")
	}

	if !outreach.IsMailboxLevelError(err) {
		t.Fatalf("unauthorized API key must be a mailbox-level error: %v", err)
	}
}

func TestRoutingSenderPicksChannelFromSettings(t *testing.T) {
	t.Parallel()

	smtp := &fakeSender{}
	api := &fakeSender{}
	router := &outreach.RoutingSender{SMTP: smtp, API: api}

	settings := brevoSettings("xkeysib-test")
	message := outreach.Message{ToEmail: "a@example.org"}

	if err := router.Send(context.Background(), &settings, &message); err != nil {
		t.Fatal(err)
	}

	if len(api.messages) != 1 || len(smtp.messages) != 0 {
		t.Fatalf("api channel not used: api=%d smtp=%d", len(api.messages), len(smtp.messages))
	}

	settings.SendVia = outreach.SendViaSMTP

	if err := router.Send(context.Background(), &settings, &message); err != nil {
		t.Fatal(err)
	}

	if len(smtp.messages) != 1 {
		t.Fatalf("smtp channel not used after switching back: smtp=%d", len(smtp.messages))
	}
}
