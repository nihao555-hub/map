package intel

import (
	"strings"

	"github.com/nyaruka/phonenumbers"

	"github.com/gosom/google-maps-scraper/enrich"
)

// normalizePhones rewrites every phone into E.164 when libphonenumber can
// parse it, keeping the original Raw value for human display.
func normalizePhones(phones []enrich.Phone, defaultRegion string) []enrich.Phone {
	if len(phones) == 0 {
		return phones
	}

	out := make([]enrich.Phone, len(phones))
	copy(out, phones)

	for i := range out {
		raw := out[i].Raw
		if raw == "" {
			raw = out[i].E164
		}

		if raw == "" {
			continue
		}

		parsed, err := phonenumbers.Parse(raw, defaultRegion)
		if err != nil || !phonenumbers.IsValidNumber(parsed) {
			// Try again treating a leading 00 as +.
			if strings.HasPrefix(raw, "00") {
				parsed, err = phonenumbers.Parse("+"+strings.TrimPrefix(raw, "00"), defaultRegion)
			}
		}

		if err != nil || parsed == nil || !phonenumbers.IsValidNumber(parsed) {
			continue
		}

		out[i].E164 = phonenumbers.Format(parsed, phonenumbers.E164)
		if out[i].Raw == "" {
			out[i].Raw = out[i].E164
		}
	}

	return out
}
