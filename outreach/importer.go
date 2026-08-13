package outreach

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	netmail "net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ImportSummary describes a lead CSV import.
type ImportSummary struct {
	Rows     int `json:"rows"`
	Emails   int `json:"emails"`
	Inserted int `json:"inserted"`
	Skipped  int `json:"skipped"`
}

// ImportCSV reads a scraper CSV and imports one valid, non-suppressed recipient
// per map listing. When a listing contains multiple email addresses, the
// importer prefers a named/sales address on the business website's domain.
func ImportCSV(
	ctx context.Context,
	store *Store,
	campaignID string,
	reader io.Reader,
	now time.Time,
) (ImportSummary, error) {
	campaign, err := store.Campaign(ctx, campaignID)
	if err != nil {
		return ImportSummary{}, err
	}

	if campaign.Status != CampaignStatusDraft {
		return ImportSummary{}, errors.New("contacts can only be imported into a draft campaign")
	}

	settings, err := store.Settings(ctx)
	if err != nil {
		return ImportSummary{}, err
	}

	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1
	csvReader.ReuseRecord = false

	headers, err := csvReader.Read()
	if err != nil {
		return ImportSummary{}, fmt.Errorf("read lead CSV header: %w", err)
	}

	index := make(map[string]int, len(headers))
	for i, header := range headers {
		index[strings.ToLower(strings.TrimSpace(header))] = i
	}

	if _, ok := index["emails"]; !ok {
		return ImportSummary{}, errors.New("lead CSV has no emails column; enable 抓取邮箱 on the map job")
	}

	var (
		summary  ImportSummary
		contacts []Contact
		seen     = make(map[string]struct{})
	)

	for {
		record, err := csvReader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return ImportSummary{}, fmt.Errorf("read lead CSV row %d: %w", summary.Rows+2, err)
		}

		summary.Rows++

		emails := splitEmails(field(record, index, "emails"))
		summary.Emails += len(emails)

		validEmails := make([]string, 0, len(emails))

		for _, value := range emails {
			email, valid := validRecipient(value)
			if !valid {
				summary.Skipped++

				continue
			}

			validEmails = append(validEmails, email)
		}

		email := bestRecipient(validEmails, field(record, index, "website"))
		if email == "" {
			continue
		}

		// One map listing is one customer. Sending to multiple addresses at
		// the same company creates complaints and hurts reply rates.
		summary.Skipped += len(validEmails) - 1

		if _, duplicate := seen[email]; duplicate {
			summary.Skipped++

			continue
		}

		seen[email] = struct{}{}

		timezone := field(record, index, "timezone")
		location := LoadLocation(timezone, settings.DefaultTimezone)

		contact := Contact{
			CampaignID:  campaignID,
			Email:       email,
			Name:        field(record, index, "title"),
			Category:    field(record, index, "category"),
			Address:     field(record, index, "address"),
			City:        cityFromRecord(record, index),
			Website:     field(record, index, "website"),
			Phone:       field(record, index, "phone"),
			Rating:      field(record, index, "review_rating"),
			ReviewCount: parseInt(field(record, index, "review_count")),
			Timezone:    timezone,
			Status:      ContactStatusActive,
			NextSendAt:  NextSendTimeJittered(now, location, settings.Window()),
		}

		if contact.Name == "" {
			contact.Name = strings.Split(email, "@")[0]
		}

		contacts = append(contacts, contact)
	}

	summary.Inserted, err = store.AddContacts(ctx, contacts)
	if err != nil {
		return ImportSummary{}, err
	}

	summary.Skipped += len(contacts) - summary.Inserted

	return summary, nil
}

func field(record []string, index map[string]int, name string) string {
	position, ok := index[name]
	if !ok || position < 0 || position >= len(record) {
		return ""
	}

	return strings.TrimSpace(record[position])
}

func splitEmails(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || unicode.IsSpace(r)
	})

	emails := make([]string, 0, len(fields))

	for _, item := range fields {
		item = strings.Trim(strings.TrimPrefix(strings.TrimSpace(item), "mailto:"), "<>[]()\"'")
		if item != "" {
			emails = append(emails, item)
		}
	}

	return emails
}

func validRecipient(value string) (string, bool) {
	address, err := netmail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}

	normalized := normalizeEmail(address.Address)

	at := strings.LastIndex(normalized, "@")
	if at <= 0 || at == len(normalized)-1 || !strings.Contains(normalized[at+1:], ".") {
		return "", false
	}

	local := normalized[:at]
	switch local {
	case "noreply", "no-reply", "donotreply", "do-not-reply", "mailer-daemon",
		"youremail", "your-email", "email", "example", "test", "sentry":
		return "", false
	}

	// Skip addresses that scrapers commonly pick up from page scripts/widgets
	// rather than a real human inbox (e.g. Sentry DSNs, tracking endpoints).
	domain := normalized[at+1:]
	junkDomains := []string{"sentry.io", "sentry-cdn.com", "wixpress.com", "ingest.sentry.io"}

	for _, junk := range junkDomains {
		if domain == junk || strings.HasSuffix(domain, "."+junk) {
			return "", false
		}
	}

	return normalized, true
}

func bestRecipient(emails []string, website string) string {
	if len(emails) == 0 {
		return ""
	}

	websiteDomain := ""
	if parsed, err := url.Parse(website); err == nil {
		websiteDomain = strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	}

	best := emails[0]
	bestScore := recipientScore(best, websiteDomain)

	for _, email := range emails[1:] {
		score := recipientScore(email, websiteDomain)
		if score > bestScore {
			best = email
			bestScore = score
		}
	}

	return best
}

func recipientScore(email, websiteDomain string) int {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return -1
	}

	local := parts[0]
	domain := strings.TrimPrefix(parts[1], "www.")
	score := 0

	if websiteDomain != "" && (domain == websiteDomain || strings.HasSuffix(domain, "."+websiteDomain)) {
		score += 5
	}

	switch local {
	case "owner", "founder", "ceo", "director", "manager":
		score += 4
	case "sales", "business", "partnerships", "marketing":
		score += 3
	case "contact", "hello", "info", "office":
		score += 2
	case "support", "help", "privacy", "legal", "abuse":
		score--
	default:
		// Named addresses are usually more suitable than a role inbox.
		score += 4
	}

	return score
}

func cityFromRecord(record []string, index map[string]int) string {
	type completeAddress struct {
		City string `json:"city"`
	}

	var address completeAddress
	if value := field(record, index, "complete_address"); value != "" {
		if err := json.Unmarshal([]byte(value), &address); err == nil && address.City != "" {
			return address.City
		}
	}

	return ""
}

func parseInt(value string) int {
	result, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}

	return result
}
