package outreach

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	mail "github.com/wneessen/go-mail"
)

// MailSender is implemented by SMTPMailer and by test doubles.
type MailSender interface {
	Send(context.Context, *Settings, *Message) error
	Test(context.Context, *Settings) error
}

// SMTPMailer sends RFC-compliant plain-text email over authenticated SMTP.
type SMTPMailer struct{}

// NewMessageID creates an RFC 5322 message identifier using the sender's
// domain. Keeping our own IDs is necessary to match IMAP replies and preserve
// a sequence in one thread.
func NewMessageID(senderAddress string) string {
	domain := "localhost"
	if at := strings.LastIndex(senderAddress, "@"); at >= 0 && at < len(senderAddress)-1 {
		domain = senderAddress[at+1:]
	}

	return normalizeMessageID(uuid.NewString() + "@" + domain)
}

// Send sends one message. It intentionally creates only a text/plain body and
// no open pixels or click-tracking links: those are common spam signals and
// misleading for privacy-conscious recipients.
func (s *SMTPMailer) Send(ctx context.Context, settings *Settings, message *Message) error {
	if !settings.SMTPConfigured() {
		return errors.New("SMTP is not configured; set server, account and OUTREACH_SMTP_PASSWORD")
	}

	if message.ToEmail == "" {
		return errors.New("missing recipient email")
	}

	client, err := newSMTPClient(settings)
	if err != nil {
		return err
	}

	email := mail.NewMsg()
	if settings.FromName != "" {
		if err := email.FromFormat(settings.FromName, settings.EmailAddress); err != nil {
			return fmt.Errorf("set sender: %w", err)
		}
	} else if err := email.From(settings.EmailAddress); err != nil {
		return fmt.Errorf("set sender: %w", err)
	}

	if err := email.To(message.ToEmail); err != nil {
		return fmt.Errorf("set recipient: %w", err)
	}

	email.Subject(message.Subject)
	email.SetDateWithValue(message.CreatedAt)

	messageID := normalizeMessageID(message.MessageID)
	if messageID == "" {
		messageID = NewMessageID(settings.EmailAddress)
	}

	email.SetMessageIDWithValue(strings.Trim(messageID, "<>"))

	if message.InReplyTo != "" {
		reference := normalizeMessageID(message.InReplyTo)
		email.SetGenHeader(mail.HeaderInReplyTo, reference)
		email.SetGenHeader(mail.HeaderReferences, reference)
	}

	// A mailto List-Unsubscribe header is supported by most providers and
	// gives recipients another easy opt-out path without requiring a public
	// HTTP endpoint.
	unsubscribeURI := "mailto:" + settings.EmailAddress
	values := url.Values{}
	values.Set("subject", "unsubscribe")
	unsubscribeURI += "?" + values.Encode()
	email.SetListUnsubscribe(unsubscribeURI)

	body := strings.TrimSpace(message.Body)
	if settings.UnsubscribeText != "" {
		body += "\n\n--\n" + strings.TrimSpace(settings.UnsubscribeText)
	}

	email.SetBodyString(mail.TypeTextPlain, body)

	if err := client.DialAndSendWithContext(ctx, email); err != nil {
		return fmt.Errorf("send SMTP message: %w", err)
	}

	return nil
}

// Test verifies DNS/TLS/authentication without sending a message.
func (s *SMTPMailer) Test(ctx context.Context, settings *Settings) error {
	if !settings.SMTPConfigured() {
		return errors.New("SMTP is not configured; set server, account and OUTREACH_SMTP_PASSWORD")
	}

	client, err := newSMTPClient(settings)
	if err != nil {
		return err
	}

	testCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := client.DialWithContext(testCtx); err != nil {
		return fmt.Errorf("connect/authenticate SMTP: %w", err)
	}

	if err := client.Close(); err != nil {
		return fmt.Errorf("close SMTP test connection: %w", err)
	}

	return nil
}

func newSMTPClient(settings *Settings) (*mail.Client, error) {
	options := []mail.Option{
		mail.WithPort(settings.SMTPPort),
		mail.WithUsername(settings.EmailAddress),
		mail.WithPassword(settings.Password),
		mail.WithTimeout(20 * time.Second),
	}

	switch settings.SMTPTLS {
	case TLSModeSSL:
		options = append(options, mail.WithSSL())
	case TLSModeStartTLS, "":
		options = append(options, mail.WithTLSPolicy(mail.TLSMandatory))
	default:
		return nil, fmt.Errorf("unsupported SMTP TLS mode %q", settings.SMTPTLS)
	}

	client, err := mail.NewClient(settings.SMTPHost, options...)
	if err != nil {
		return nil, fmt.Errorf("configure SMTP client: %w", err)
	}

	return client, nil
}
