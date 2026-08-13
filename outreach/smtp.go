package outreach

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	mail "github.com/wneessen/go-mail"
)

// relayProbeRecipient is an external address used only for an RCPT TO
// permission check. The session is reset before DATA, so no email is ever
// sent to it.
const relayProbeRecipient = "postmaster@gmail.com"

// mailboxLevelErrorMarkers identify send failures caused by the sender
// account itself (blocked relay, broken auth, suspended mailbox, rejected
// or exhausted API key). These fail for every recipient, so retrying per
// contact only burns the sequence.
var mailboxLevelErrorMarkers = []string{
	"relay access denied",
	"relaying denied",
	"relay not permitted",
	"authentication required",
	"authentication failed",
	"5.8.3",
	"api key rejected",
	"account quota problem",
	"account suspended",
}

// IsMailboxLevelError reports whether the send failure is a sender-account
// problem that would affect every recipient, as opposed to one bad address.
func IsMailboxLevelError(err error) bool {
	if err == nil {
		return false
	}

	text := strings.ToLower(err.Error())
	for _, marker := range mailboxLevelErrorMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}

	return false
}

// unsubscribeMailto builds the mailto List-Unsubscribe target. It is
// supported by most providers and gives recipients an easy opt-out path
// without requiring a public HTTP endpoint.
func unsubscribeMailto(settings *Settings) string {
	values := url.Values{}
	values.Set("subject", "unsubscribe")

	return "mailto:" + settings.EmailAddress + "?" + values.Encode()
}

// bodyWithUnsubscribeFooter appends the compliance footer to the plain-text
// body, identically for every outbound channel.
func bodyWithUnsubscribeFooter(settings *Settings, message *Message) string {
	body := strings.TrimSpace(message.Body)
	if settings.UnsubscribeText != "" {
		body += "\n\n--\n" + strings.TrimSpace(settings.UnsubscribeText)
	}

	return body
}

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

	email.SetListUnsubscribe(unsubscribeMailto(settings))
	email.SetBodyString(mail.TypeTextPlain, bodyWithUnsubscribeFooter(settings, message))

	if err := client.DialAndSendWithContext(ctx, email); err != nil {
		return fmt.Errorf("send SMTP message: %w", err)
	}

	return nil
}

// Test verifies DNS/TLS/authentication without sending a message, then
// checks that the account may deliver to external domains: providers
// sometimes accept the login but reject every outside recipient (e.g.
// "550 Relay access denied" on restricted free mailboxes), which would
// silently break a whole campaign.
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

	return probeExternalRelay(settings)
}

// probeExternalRelay authenticates and issues RCPT TO for one external
// address, then resets the session without sending anything. A rejection
// here means the provider will refuse every real campaign email.
func probeExternalRelay(settings *Settings) error {
	addr := net.JoinHostPort(settings.SMTPHost, strconv.Itoa(settings.SMTPPort))

	var (
		client *smtp.Client
		err    error
	)

	if settings.SMTPTLS == TLSModeSSL {
		conn, dialErr := tls.DialWithDialer(
			&net.Dialer{Timeout: 15 * time.Second},
			"tcp",
			addr,
			&tls.Config{ServerName: settings.SMTPHost, MinVersion: tls.VersionTLS12},
		)
		if dialErr != nil {
			return fmt.Errorf("connect SMTP for relay probe: %w", dialErr)
		}

		client, err = smtp.NewClient(conn, settings.SMTPHost)
	} else {
		client, err = smtp.Dial(addr)
		if err == nil {
			err = client.StartTLS(&tls.Config{ServerName: settings.SMTPHost, MinVersion: tls.VersionTLS12})
		}
	}

	if err != nil {
		return fmt.Errorf("prepare SMTP relay probe: %w", err)
	}

	defer func() { _ = client.Quit() }()

	auth := smtp.PlainAuth("", settings.EmailAddress, settings.Password, settings.SMTPHost)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("authenticate for relay probe: %w", err)
	}

	if err := client.Mail(settings.EmailAddress); err != nil {
		return fmt.Errorf("MAIL FROM during relay probe: %w", err)
	}

	if err := client.Rcpt(relayProbeRecipient); err != nil {
		_ = client.Reset()

		return fmt.Errorf(
			"账号无法向外部域名发信（服务商拒绝: %w）——通常是免费/新邮箱被限制外发或触发了反垃圾封禁，请到邮箱服务商后台申诉解除，或更换正规发信邮箱",
			err,
		)
	}

	return client.Reset()
}

func newSMTPClient(settings *Settings) (*mail.Client, error) {
	options := []mail.Option{
		mail.WithPort(settings.SMTPPort),
		mail.WithUsername(settings.EmailAddress),
		mail.WithPassword(settings.Password),
		mail.WithTimeout(20 * time.Second),
		// go-mail defaults to no authentication even when a username and
		// password are set. Enterprise SMTP relays reject unauthenticated
		// mail, so negotiate the best mechanism the server advertises.
		mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
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
