package outreach

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	messagemail "github.com/emersion/go-message/mail"
)

const (
	maxInboxBatch  = 200
	maxInboundBody = 128 * 1024
)

// InboundEmail is a normalized message fetched from IMAP.
type InboundEmail struct {
	UID        uint32
	MessageID  string
	InReplyTo  []string
	References []string
	FromEmail  string
	ToEmail    string
	Subject    string
	Body       string
	Date       time.Time
}

// Inbox polls incoming mail and supports a non-destructive connection test.
type Inbox interface {
	Poll(context.Context, *Settings, uint32) ([]InboundEmail, uint32, error)
	Test(context.Context, *Settings) error
}

// IMAPInbox reads replies without marking them as seen.
type IMAPInbox struct{}

// Test verifies TLS, authentication and INBOX access without reading messages.
func (i *IMAPInbox) Test(_ context.Context, settings *Settings) error {
	if !settings.IMAPConfigured() {
		return errors.New("IMAP is not configured; set server, account and OUTREACH_SMTP_PASSWORD")
	}

	client, err := connectIMAP(settings)
	if err != nil {
		return err
	}
	defer client.Close()

	if _, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return fmt.Errorf("select IMAP inbox: %w", err)
	}

	if err := client.Logout().Wait(); err != nil {
		return fmt.Errorf("logout IMAP: %w", err)
	}

	return nil
}

// Poll returns messages with UIDs after afterUID. On the first poll it scans
// only the previous 30 days, avoiding an expensive fetch of an old mailbox.
func (i *IMAPInbox) Poll(
	_ context.Context,
	settings *Settings,
	afterUID uint32,
) ([]InboundEmail, uint32, error) {
	if !settings.IMAPConfigured() {
		return nil, afterUID, nil
	}

	client, err := connectIMAP(settings)
	if err != nil {
		return nil, afterUID, err
	}
	defer client.Close()

	if _, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return nil, afterUID, fmt.Errorf("select IMAP inbox: %w", err)
	}

	criteria := &imap.SearchCriteria{}

	if afterUID > 0 {
		var uidSet imap.UIDSet

		uidSet.AddRange(imap.UID(afterUID+1), 0) // zero is IMAP's "*" wildcard.

		criteria.UID = append(criteria.UID, uidSet)
	} else {
		criteria.Since = time.Now().UTC().AddDate(0, 0, -30)
	}

	searchData, err := client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, afterUID, fmt.Errorf("search IMAP inbox: %w", err)
	}

	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		if err := client.Logout().Wait(); err != nil {
			return nil, afterUID, fmt.Errorf("logout IMAP: %w", err)
		}

		return nil, afterUID, nil
	}

	sort.Slice(uids, func(a, b int) bool {
		return uids[a] < uids[b]
	})

	if len(uids) > maxInboxBatch {
		uids = uids[len(uids)-maxInboxBatch:]
	}

	section := &imap.FetchItemBodySection{Peek: true}
	options := &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		InternalDate: true,
		BodySection:  []*imap.FetchItemBodySection{section},
	}

	fetched, err := client.Fetch(imap.UIDSetNum(uids...), options).Collect()
	if err != nil {
		return nil, afterUID, fmt.Errorf("fetch IMAP messages: %w", err)
	}

	messages := make([]InboundEmail, 0, len(fetched))
	lastUID := afterUID

	for _, item := range fetched {
		if uint32(item.UID) > lastUID {
			lastUID = uint32(item.UID)
		}

		message := inboundFromEnvelope(item.UID, item.Envelope, item.InternalDate)
		raw := item.FindBodySection(section)

		if len(raw) > 0 {
			parseInboundRaw(raw, &message)
		}

		messages = append(messages, message)
	}

	sort.Slice(messages, func(a, b int) bool {
		return messages[a].UID < messages[b].UID
	})

	if err := client.Logout().Wait(); err != nil {
		return nil, afterUID, fmt.Errorf("logout IMAP: %w", err)
	}

	return messages, lastUID, nil
}

func connectIMAP(settings *Settings) (*imapclient.Client, error) {
	address := net.JoinHostPort(settings.IMAPHost, strconv.Itoa(settings.IMAPPort))
	options := &imapclient.Options{
		Dialer: &net.Dialer{Timeout: 15 * time.Second},
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: settings.IMAPHost,
		},
	}

	client, err := imapclient.DialTLS(address, options)
	if err != nil {
		return nil, fmt.Errorf("connect IMAP: %w", err)
	}

	if err := client.Login(settings.EmailAddress, settings.Password).Wait(); err != nil {
		_ = client.Close()

		return nil, fmt.Errorf("authenticate IMAP: %w", err)
	}

	return client, nil
}

func inboundFromEnvelope(uid imap.UID, envelope *imap.Envelope, internalDate time.Time) InboundEmail {
	message := InboundEmail{
		UID:  uint32(uid),
		Date: internalDate,
	}

	if envelope == nil {
		return message
	}

	message.MessageID = normalizeMessageID(envelope.MessageID)
	for _, reference := range envelope.InReplyTo {
		message.InReplyTo = append(message.InReplyTo, normalizeMessageID(reference))
	}

	message.Subject = envelope.Subject
	message.FromEmail = firstAddress(envelope.From)
	message.ToEmail = firstAddress(envelope.To)

	if !envelope.Date.IsZero() {
		message.Date = envelope.Date
	}

	return message
}

func firstAddress(addresses []imap.Address) string {
	if len(addresses) == 0 {
		return ""
	}

	address := addresses[0]
	if address.Mailbox == "" || address.Host == "" {
		return ""
	}

	return normalizeEmail(address.Mailbox + "@" + address.Host)
}

func parseInboundRaw(raw []byte, target *InboundEmail) {
	reader, err := messagemail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return
	}
	defer reader.Close()

	if target.MessageID == "" {
		if messageID, err := reader.Header.MessageID(); err == nil {
			target.MessageID = normalizeMessageID(messageID)
		}
	}

	if references, err := reader.Header.MsgIDList("References"); err == nil {
		for _, reference := range references {
			target.References = append(target.References, normalizeMessageID(reference))
		}
	}

	if references, err := reader.Header.MsgIDList("In-Reply-To"); err == nil {
		for _, reference := range references {
			target.InReplyTo = appendUnique(target.InReplyTo, normalizeMessageID(reference))
		}
	}

	var htmlFallback string

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			break
		}

		header, ok := part.Header.(*messagemail.InlineHeader)
		if !ok {
			continue
		}

		contentType, _, err := header.ContentType()
		if err != nil {
			continue
		}

		data, err := io.ReadAll(io.LimitReader(part.Body, maxInboundBody))
		if err != nil {
			continue
		}

		switch contentType {
		case "text/plain":
			target.Body = strings.TrimSpace(string(data))

			return
		case "text/html":
			htmlFallback = strings.TrimSpace(string(data))
		}
	}

	target.Body = htmlFallback
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}

	for _, existing := range values {
		if existing == value {
			return values
		}
	}

	return append(values, value)
}

// ClassifyInbound identifies hard bounces and explicit opt-outs. Every other
// matched inbound message is a reply and stops future follow-ups.
func ClassifyInbound(message *InboundEmail) string {
	from := strings.ToLower(message.FromEmail)
	subject := strings.ToLower(message.Subject)

	bouncePhrases := []string{
		"undelivered", "delivery status notification", "mail delivery failed",
		"failure notice", "returned mail", "delivery incomplete",
		"退信", "无法送达", "投递失败", "邮件投递失败",
	}

	if strings.Contains(from, "mailer-daemon@") || strings.Contains(from, "postmaster@") ||
		containsAny(subject, bouncePhrases) {
		return InboundKindBounce
	}

	// Ignore quoted copies of our own footer; otherwise any ordinary reply
	// would contain the word "unsubscribe" and be misclassified.
	replyText := strings.ToLower(unquotedReply(message.Body))
	unsubscribePhrases := []string{
		"unsubscribe", "remove me", "stop emailing", "do not contact",
		"don't contact", "opt out", "退订", "不要再发", "停止发送",
	}

	if containsAny(subject, unsubscribePhrases) || containsAny(replyText, unsubscribePhrases) {
		return InboundKindUnsubscribe
	}

	return InboundKindReply
}

func unquotedReply(body string) string {
	separators := []string{
		"\n-----Original Message-----",
		"\n-----原始邮件-----",
		"\nFrom:",
		"\n发件人:",
	}

	cut := len(body)
	for _, separator := range separators {
		if index := strings.Index(body, separator); index >= 0 && index < cut {
			cut = index
		}
	}

	lines := strings.Split(body[:cut], "\n")
	kept := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") {
			continue
		}

		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "on ") && strings.HasSuffix(lower, " wrote:") {
			break
		}

		kept = append(kept, line)
	}

	text := strings.Join(kept, "\n")
	if len(text) > 2000 {
		text = text[:2000]
	}

	return text
}

func containsAny(value string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(value, phrase) {
			return true
		}
	}

	return false
}
