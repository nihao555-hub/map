package intel

import (
	"context"
	_ "embed"
	"os"
	"strings"
	"sync"

	emailverifier "github.com/AfterShip/email-verifier"

	"github.com/gosom/google-maps-scraper/enrich"
)

//go:embed disposable_email_blocklist.conf
var disposableBlocklistRaw string

type emailVerifier struct {
	v *emailverifier.Verifier
}

func (e *Enricher) emailClient() *emailVerifier {
	e.emailOnce.Do(func() {
		v := emailverifier.NewVerifier()

		if e.opts.EnableSMTP {
			v = v.EnableSMTPCheck()
		} else {
			v = v.DisableSMTPCheck()
		}

		// Prefer the embedded CC0 blocklist for offline/batch runs. Callers
		// that want the live feed can set INTEL_UPDATE_DISPOSABLE=1.
		if os.Getenv("INTEL_UPDATE_DISPOSABLE") == "1" {
			v = v.EnableAutoUpdateDisposable()
		}

		v.AddDisposableDomains(embeddedDisposableDomains())

		e.emails = &emailVerifier{v: v}
	})

	return e.emails
}

func (e *Enricher) verifyEmails(_ context.Context, emails []enrich.Email) []enrich.VerifiedEmail {
	if len(emails) == 0 {
		return nil
	}

	client := e.emailClient()
	out := make([]enrich.VerifiedEmail, 0, len(emails))

	// Cap concurrent SMTP/MX probes so a lead with many addresses does not
	// open dozens of connections at once.
	const maxConcurrent = 4

	sem := make(chan struct{}, maxConcurrent)

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		seen = make(map[string]bool, len(emails))
	)

	for i := range emails {
		address := strings.ToLower(strings.TrimSpace(emails[i].Address))
		if address == "" || seen[address] {
			continue
		}

		seen[address] = true

		wg.Add(1)

		go func(addr string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			result, err := client.v.Verify(addr)

			verified := enrich.VerifiedEmail{Address: addr}
			if err != nil || result == nil {
				mu.Lock()
				out = append(out, verified)
				mu.Unlock()

				return
			}

			verified.Disposable = result.Disposable
			verified.RoleAccount = result.RoleAccount
			verified.Free = result.Free
			verified.HasMX = result.HasMxRecords
			verified.Reachable = result.Reachable

			if result.SMTP != nil {
				verified.SMTPValid = result.SMTP.Deliverable || result.SMTP.CatchAll
			}

			mu.Lock()
			out = append(out, verified)
			mu.Unlock()
		}(address)
	}

	wg.Wait()

	return out
}

// applyVerificationToEmails drops disposable addresses and upgrades Kind when
// the verifier disagrees with our cheaper local classification.
func (e *Enricher) applyVerificationToEmails(profile *enrich.CompanyProfile) {
	if len(profile.VerifiedEmails) == 0 {
		return
	}

	byAddr := make(map[string]enrich.VerifiedEmail, len(profile.VerifiedEmails))
	for _, v := range profile.VerifiedEmails {
		byAddr[strings.ToLower(v.Address)] = v
	}

	kept := make([]enrich.Email, 0, len(profile.Emails))

	for _, email := range profile.Emails {
		v, ok := byAddr[strings.ToLower(email.Address)]
		if ok && v.Disposable {
			continue
		}

		if ok && v.RoleAccount && email.Kind == enrich.EmailKindPersonal {
			email.Kind = enrich.EmailKindRole
		}

		if ok && v.Free {
			email.OnDomain = false
		}

		kept = append(kept, email)
	}

	profile.Emails = kept
}

func mergeVerified(a, b []enrich.VerifiedEmail) []enrich.VerifiedEmail {
	if len(a) == 0 {
		return b
	}

	if len(b) == 0 {
		return a
	}

	index := make(map[string]int, len(a)+len(b))
	out := make([]enrich.VerifiedEmail, 0, len(a)+len(b))

	for _, list := range [][]enrich.VerifiedEmail{a, b} {
		for _, e := range list {
			key := strings.ToLower(e.Address)
			if key == "" {
				continue
			}

			if _, ok := index[key]; ok {
				continue
			}

			index[key] = len(out)
			out = append(out, e)
		}
	}

	return out
}

// embeddedDisposableDomains returns the CC0 disposable-email-domains
// blocklist that ships inside the binary (see disposable_email_blocklist.conf).
func embeddedDisposableDomains() []string {
	lines := strings.Split(disposableBlocklistRaw, "\n")
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(strings.ToLower(line))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		out = append(out, line)
	}

	return out
}
