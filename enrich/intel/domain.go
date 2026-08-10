package intel

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/likexian/whois"
	whoisparser "github.com/likexian/whois-parser"
)

// domainInfo is the subset of WHOIS + MX data that matters for lead quality.
type domainInfo struct {
	MailProvider string
	MXHosts      []string
	CreatedAt    string
	AgeDays      int
	Registrar    string
}

// Known MX patterns → corporate mail provider. This is the practical value of
// "dnsx MX lookup" for back-research without pulling the full ProjectDiscovery
// toolchain into the binary.
var mxProviders = []struct {
	needle   string
	provider string
}{
	{"google.com", "google-workspace"},
	{"googlemail.com", "google-workspace"},
	{"outlook.com", "microsoft-365"},
	{"protection.outlook.com", "microsoft-365"},
	{"mail.protection.outlook.com", "microsoft-365"},
	{"qq.com", "tencent-exmail"},
	{"mxhichina.com", "alibaba-mail"},
	{"aliyun.com", "alibaba-mail"},
	{"secureserver.net", "godaddy"},
	{"emailsrvr.com", "rackspace"},
	{"pphosted.com", "proofpoint"},
	{"messagelabs.com", "symantec"},
	{"mimecast.com", "mimecast"},
	{"zoho.com", "zoho"},
	{"yahoodns.net", "yahoo"},
	{"yandex.net", "yandex"},
	{"mailgun.org", "mailgun"},
	{"amazonaws.com", "amazon-ses"},
}

func lookupDomain(ctx context.Context, domain string) domainInfo {
	info := domainInfo{}

	info.MXHosts, info.MailProvider = lookupMX(ctx, domain)

	created, registrar := lookupWHOIS(ctx, domain)
	info.CreatedAt = created
	info.Registrar = registrar

	if created != "" {
		if t, err := time.Parse("2006-01-02", created); err == nil {
			info.AgeDays = int(time.Since(t).Hours() / 24)
			if info.AgeDays < 0 {
				info.AgeDays = 0
			}
		}
	}

	return info
}

func lookupMX(ctx context.Context, domain string) ([]string, string) {
	type result struct {
		hosts    []string
		provider string
	}

	done := make(chan result, 1)

	go func() {
		records, err := net.LookupMX(domain)
		if err != nil {
			done <- result{}

			return
		}

		hosts := make([]string, 0, len(records))
		for _, mx := range records {
			host := strings.TrimSuffix(strings.ToLower(mx.Host), ".")
			if host != "" {
				hosts = append(hosts, host)
			}
		}

		done <- result{hosts: hosts, provider: classifyMX(hosts)}
	}()

	select {
	case <-ctx.Done():
		return nil, ""
	case r := <-done:
		return r.hosts, r.provider
	}
}

func classifyMX(hosts []string) string {
	for _, host := range hosts {
		for _, candidate := range mxProviders {
			if strings.Contains(host, candidate.needle) {
				return candidate.provider
			}
		}
	}

	if len(hosts) > 0 {
		return "self-hosted"
	}

	return ""
}

func lookupWHOIS(ctx context.Context, domain string) (created, registrar string) {
	type result struct {
		created, registrar string
	}

	done := make(chan result, 1)

	go func() {
		raw, err := whois.Whois(domain)
		if err != nil {
			done <- result{}

			return
		}

		parsed, err := whoisparser.Parse(raw)
		if err != nil || parsed.Domain == nil {
			done <- result{}

			return
		}

		created := ""
		if parsed.Domain.CreatedDateInTime != nil {
			created = parsed.Domain.CreatedDateInTime.UTC().Format("2006-01-02")
		} else if parsed.Domain.CreatedDate != "" {
			created = truncateDate(parsed.Domain.CreatedDate)
		}

		reg := ""
		if parsed.Registrar != nil {
			reg = parsed.Registrar.Name
		}

		done <- result{created: created, registrar: reg}
	}()

	select {
	case <-ctx.Done():
		return "", ""
	case r := <-done:
		return r.created, r.registrar
	}
}

func truncateDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 10 {
		return raw[:10]
	}

	return raw
}
