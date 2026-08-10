package enrich

import (
	"regexp"
	"strings"
)

// phoneTextRe matches numbers written for humans: an optional international
// prefix followed by 6-20 digits broken up by spaces, dots, dashes or
// parentheses. Requiring at least one separator or a leading "+" keeps it from
// swallowing order numbers, prices and years.
var phoneTextRe = regexp.MustCompile(`(?:\+|00)\d[\d\s().\-]{5,20}\d`)

// minPhoneDigits/maxPhoneDigits bound a plausible international number: the
// ITU E.164 limit is 15 digits, plus room for an extension written inline.
const (
	minPhoneDigits = 7
	maxPhoneDigits = 17
)

// ExtractPhonesFromLinks reads numbers out of tel: and wa.me hrefs. These are
// unambiguous because the site author marked them up as dialable.
func ExtractPhonesFromLinks(hrefs []string, source string) []Phone {
	out := make([]Phone, 0, len(hrefs))

	for _, href := range hrefs {
		lower := strings.ToLower(strings.TrimSpace(href))

		switch {
		case strings.HasPrefix(lower, "tel:"), strings.HasPrefix(lower, "callto:"):
			raw := href[strings.Index(href, ":")+1:]
			if phone, ok := newPhone(unescapePercent(raw), false, source); ok {
				out = append(out, phone)
			}
		case strings.Contains(lower, "wa.me/"), strings.Contains(lower, "api.whatsapp.com/send"), strings.Contains(lower, "web.whatsapp.com/send"):
			if phone, ok := newPhone(whatsAppNumber(href), true, source); ok {
				out = append(out, phone)
			}
		}
	}

	return out
}

// ExtractPhonesFromText finds internationally formatted numbers in visible
// page text. Only "+"/"00"-prefixed numbers are collected: national formats
// cannot be told apart from other digit groups without knowing the country.
func ExtractPhonesFromText(text, source string) []Phone {
	matches := phoneTextRe.FindAllString(text, -1)

	out := make([]Phone, 0, len(matches))

	for _, match := range matches {
		if phone, ok := newPhone(match, false, source); ok {
			out = append(out, phone)
		}
	}

	return out
}

func newPhone(raw string, whatsApp bool, source string) (Phone, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Phone{}, false
	}

	digits := digitsOnly(raw)
	if len(digits) < minPhoneDigits || len(digits) > maxPhoneDigits {
		return Phone{}, false
	}

	// A number made of one repeated digit is a placeholder ("+1 111 111 1111").
	if isRepeatedDigit(digits) {
		return Phone{}, false
	}

	phone := Phone{
		Raw:      raw,
		WhatsApp: whatsApp,
		Source:   source,
	}

	switch {
	case strings.HasPrefix(raw, "+"):
		phone.E164 = "+" + digits
	case strings.HasPrefix(digits, "00"):
		phone.E164 = "+" + strings.TrimPrefix(digits, "00")
	case whatsApp:
		// wa.me numbers are always in international form without the "+".
		phone.E164 = "+" + digits
	}

	return phone, true
}

func whatsAppNumber(href string) string {
	lower := strings.ToLower(href)

	if idx := strings.Index(lower, "phone="); idx >= 0 {
		rest := href[idx+len("phone="):]
		if end := strings.IndexAny(rest, "&#"); end >= 0 {
			rest = rest[:end]
		}

		return rest
	}

	if idx := strings.Index(lower, "wa.me/"); idx >= 0 {
		rest := href[idx+len("wa.me/"):]
		if end := strings.IndexAny(rest, "?#/"); end >= 0 {
			rest = rest[:end]
		}

		return rest
	}

	return ""
}

func isRepeatedDigit(digits string) bool {
	if len(digits) < 2 {
		return false
	}

	for i := 1; i < len(digits); i++ {
		if digits[i] != digits[0] {
			return false
		}
	}

	return true
}
