package outreach

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// brevoBaseURL is the production endpoint of the Brevo v3 API. The free plan
// includes 300 transactional emails per day with full API access.
const brevoBaseURL = "https://api.brevo.com"

// ipAllowlistRetries retries requests rejected by Brevo's authorised-IP
// feature. Deployments behind rotating NAT pools would otherwise fail
// randomly even when some of the pool addresses are authorised; a 401 at
// this stage never sends email, so retrying is safe.
const ipAllowlistRetries = 4

// BrevoMailer sends email through the Brevo transactional HTTP API instead
// of the mailbox's own SMTP relay. The From/Reply-To address stays the
// operator's mailbox, so customer replies keep arriving over IMAP and the
// whole reply/bounce/intent pipeline works unchanged.
type BrevoMailer struct {
	httpClient *http.Client
	baseURL    string
}

// NewBrevoMailer returns a Brevo API sender for the production endpoint.
func NewBrevoMailer() *BrevoMailer {
	return &BrevoMailer{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    brevoBaseURL,
	}
}

// NewBrevoMailerForTest returns a sender pointed at a custom base URL.
func NewBrevoMailerForTest(baseURL string) *BrevoMailer {
	return &BrevoMailer{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    baseURL,
	}
}

// Send delivers one message via POST /v3/smtp/email. On success the
// provider-assigned Message-ID replaces message.MessageID so stored threads
// match the In-Reply-To/References headers of future customer replies.
func (b *BrevoMailer) Send(ctx context.Context, settings *Settings, message *Message) error {
	if !settings.APISendConfigured() {
		return fmt.Errorf("brevo API is not configured; set the API key and sender address")
	}

	if message.ToEmail == "" {
		return fmt.Errorf("missing recipient email")
	}

	sender := map[string]string{"email": settings.EmailAddress}
	if settings.FromName != "" {
		sender["name"] = settings.FromName
	}

	headers := map[string]string{
		"List-Unsubscribe": unsubscribeMailto(settings),
	}

	if message.InReplyTo != "" {
		reference := normalizeMessageID(message.InReplyTo)
		headers["In-Reply-To"] = reference
		headers["References"] = reference
	}

	payload := map[string]any{
		"sender":      sender,
		"to":          []map[string]string{{"email": message.ToEmail}},
		"subject":     message.Subject,
		"textContent": bodyWithUnsubscribeFooter(settings, message),
		"replyTo":     map[string]string{"email": settings.EmailAddress},
		"headers":     headers,
	}

	status, body, err := b.request(ctx, settings, http.MethodPost, "/v3/smtp/email", payload)
	if err != nil {
		return err
	}

	if status != http.StatusCreated && status != http.StatusAccepted {
		return brevoError("send via brevo API", status, body)
	}

	var reply struct {
		MessageID string `json:"messageId"`
	}

	if err := json.Unmarshal(body, &reply); err == nil && reply.MessageID != "" {
		message.MessageID = normalizeMessageID(reply.MessageID)
	}

	return nil
}

// Test verifies the API key against GET /v3/account without sending email.
func (b *BrevoMailer) Test(ctx context.Context, settings *Settings) error {
	if !settings.APISendConfigured() {
		return fmt.Errorf("brevo API is not configured; set the API key and sender address")
	}

	status, body, err := b.request(ctx, settings, http.MethodGet, "/v3/account", nil)
	if err != nil {
		return err
	}

	if status != http.StatusOK {
		return brevoError("verify brevo API key", status, body)
	}

	return nil
}

func (b *BrevoMailer) request(
	ctx context.Context,
	settings *Settings,
	method, path string,
	payload any,
) (status int, body []byte, err error) {
	var encoded []byte

	if payload != nil {
		encoded, err = json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("encode brevo request: %w", err)
		}
	}

	for attempt := 0; ; attempt++ {
		status, body, err = b.doRequest(ctx, settings, method, path, encoded)
		if err != nil {
			return 0, nil, err
		}

		if status == http.StatusUnauthorized &&
			attempt < ipAllowlistRetries &&
			bytes.Contains(body, []byte("unrecognised IP")) {
			select {
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			case <-time.After(time.Second):
			}

			continue
		}

		return status, body, nil
	}
}

func (b *BrevoMailer) doRequest(
	ctx context.Context,
	settings *Settings,
	method, path string,
	encoded []byte,
) (status int, body []byte, err error) {
	var reader io.Reader
	if encoded != nil {
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, b.baseURL+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("build brevo request: %w", err)
	}

	req.Header.Set("api-key", settings.SendAPIKey)
	req.Header.Set("accept", "application/json")

	if encoded != nil {
		req.Header.Set("content-type", "application/json")
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("call brevo API: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("read brevo response: %w", err)
	}

	return resp.StatusCode, body, nil
}

// brevoError converts an API failure into an error whose text lets
// IsMailboxLevelError distinguish account-level problems (bad key, blocked
// account, exhausted quota) from per-recipient ones.
func brevoError(action string, status int, body []byte) error {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 220 {
		snippet = snippet[:220]
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s: api key rejected (unauthorized, HTTP %d): %s", action, status, snippet)
	case http.StatusPaymentRequired, http.StatusTooManyRequests:
		return fmt.Errorf("%s: account quota problem (HTTP %d, daily limit or credits): %s", action, status, snippet)
	default:
		return fmt.Errorf("%s: HTTP %d: %s", action, status, snippet)
	}
}

// RoutingSender picks the outbound channel per the current settings, so the
// operator can switch between direct SMTP and the Brevo API without a
// restart.
type RoutingSender struct {
	SMTP MailSender
	API  MailSender
}

// NewRoutingSender wires the production SMTP and Brevo implementations.
func NewRoutingSender() *RoutingSender {
	return &RoutingSender{SMTP: &SMTPMailer{}, API: NewBrevoMailer()}
}

// Send routes to the channel selected in the settings.
func (r *RoutingSender) Send(ctx context.Context, settings *Settings, message *Message) error {
	if settings.SendVia == SendViaAPI {
		return r.API.Send(ctx, settings, message)
	}

	return r.SMTP.Send(ctx, settings, message)
}

// Test verifies the currently selected channel.
func (r *RoutingSender) Test(ctx context.Context, settings *Settings) error {
	if settings.SendVia == SendViaAPI {
		return r.API.Test(ctx, settings)
	}

	return r.SMTP.Test(ctx, settings)
}
