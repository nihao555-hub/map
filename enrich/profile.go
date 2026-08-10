// Package enrich turns the public web presence of a company into a structured
// background-research profile (背调): verified contact channels, decision-maker
// candidates, registration identifiers, trade role and market signals.
//
// Everything in this package is a pure function over already-fetched page
// content. Fetching and scheduling live in the job layer, which keeps the
// analysis deterministic and unit-testable without network access.
package enrich

import (
	"sort"
	"strings"
	"time"
)

// EmailKind classifies how useful an address is for outreach.
type EmailKind string

// Email classification buckets, ordered from most to least actionable.
const (
	// EmailKindPersonal looks like a named individual (john.smith@acme.com).
	EmailKindPersonal EmailKind = "personal"
	// EmailKindRole is a department mailbox (sales@, purchasing@, export@).
	EmailKindRole EmailKind = "role"
	// EmailKindGeneric is a catch-all mailbox (info@, contact@, hello@).
	EmailKindGeneric EmailKind = "generic"
	// EmailKindSupport is unlikely to reach a buyer (support@, noreply@).
	EmailKindSupport EmailKind = "support"
)

// Email is a discovered address plus the metadata needed to rank it.
type Email struct {
	Address string    `json:"address"`
	Kind    EmailKind `json:"kind"`
	// OnDomain reports whether the address belongs to the company's own
	// domain. Off-domain addresses (gmail, hosting providers) are weaker
	// evidence and are ranked below on-domain ones.
	OnDomain bool `json:"on_domain"`
	// Source is the page the address was found on.
	Source string `json:"source,omitempty"`
}

// Phone is a discovered phone number. Raw keeps the on-page formatting so a
// human can still read country/area grouping; E164 is best-effort.
type Phone struct {
	Raw      string `json:"raw"`
	E164     string `json:"e164,omitempty"`
	WhatsApp bool   `json:"whatsapp,omitempty"`
	Source   string `json:"source,omitempty"`
}

// Social is a company presence on a social or B2B platform.
type Social struct {
	Network string `json:"network"`
	URL     string `json:"url"`
	Handle  string `json:"handle,omitempty"`
	// IsPersonProfile marks LinkedIn member URLs (/in/...) as opposed to
	// company pages, since those are decision-maker candidates.
	IsPersonProfile bool `json:"is_person_profile,omitempty"`
}

// Person is a decision-maker candidate found on a team/about/contact page.
type Person struct {
	Name     string `json:"name"`
	Title    string `json:"title,omitempty"`
	Email    string `json:"email,omitempty"`
	LinkedIn string `json:"linkedin,omitempty"`
	Source   string `json:"source,omitempty"`
	// Seniority is derived from Title: "executive", "management" or "staff".
	Seniority string `json:"seniority,omitempty"`
}

// RegistrationID is a legal or trade identifier published by the company,
// such as a VAT number, company registration number or DUNS.
type RegistrationID struct {
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Country string `json:"country,omitempty"`
	Source  string `json:"source,omitempty"`
}

// CompanyProfile is the aggregated background-research result for one company.
// Every field is optional: a profile is built from whatever the company chose
// to publish, and Completeness reports how much of it came back.
type CompanyProfile struct {
	HomepageURL string `json:"homepage_url,omitempty"`
	Domain      string `json:"domain,omitempty"`
	LegalName   string `json:"legal_name,omitempty"`
	Description string `json:"description,omitempty"`
	LogoURL     string `json:"logo_url,omitempty"`
	FoundedYear int    `json:"founded_year,omitempty"`

	RegistrationIDs []RegistrationID `json:"registration_ids,omitempty"`

	Emails    []Email  `json:"emails,omitempty"`
	Phones    []Phone  `json:"phones,omitempty"`
	Socials   []Social `json:"socials,omitempty"`
	People    []Person `json:"people,omitempty"`
	Addresses []string `json:"addresses,omitempty"`

	// TradeRoles are self-declared positions in the supply chain, e.g.
	// "manufacturer", "wholesaler", "distributor", "importer".
	TradeRoles []string `json:"trade_roles,omitempty"`
	// Certifications are quality/compliance marks such as ISO 9001, CE, FDA.
	Certifications []string `json:"certifications,omitempty"`
	// ProductKeywords are the product/service terms the site emphasises.
	ProductKeywords []string `json:"product_keywords,omitempty"`
	// Markets are country/region names the site claims to serve or export to.
	Markets []string `json:"markets,omitempty"`
	// Languages are the locales the site publishes in (from hreflang and
	// language switchers) — a proxy for how international the company is.
	Languages []string `json:"languages,omitempty"`
	// EmployeeRange is a published headcount band, e.g. "50-100".
	EmployeeRange string `json:"employee_range,omitempty"`
	// Platform is the detected CMS/commerce stack, e.g. "shopify".
	Platform     string `json:"platform,omitempty"`
	HasEcommerce bool   `json:"has_ecommerce,omitempty"`

	// PagesCrawled lists the URLs the profile was built from, so a human can
	// audit any conclusion.
	PagesCrawled []string `json:"pages_crawled,omitempty"`
	// Completeness is a 0-100 score over the dimensions that matter for
	// outreach. Use it to rank leads by how much is actually known.
	Completeness int `json:"completeness"`
	// ResearchedAt records when the profile was assembled.
	ResearchedAt time.Time `json:"researched_at,omitempty"`
}

// Merge folds other into p, keeping the strongest value for scalar fields and
// deduplicating every list. Merge is how multi-page research is assembled:
// the homepage usually wins on identity, deeper pages add contacts.
func (p *CompanyProfile) Merge(other *CompanyProfile) {
	if other == nil {
		return
	}

	preferLonger(&p.LegalName, other.LegalName)
	preferLonger(&p.Description, other.Description)
	preferFirst(&p.LogoURL, other.LogoURL)
	preferFirst(&p.Domain, other.Domain)
	preferFirst(&p.HomepageURL, other.HomepageURL)
	preferFirst(&p.Platform, other.Platform)
	preferFirst(&p.EmployeeRange, other.EmployeeRange)

	if p.FoundedYear == 0 {
		p.FoundedYear = other.FoundedYear
	}

	if other.HasEcommerce {
		p.HasEcommerce = true
	}

	p.Emails = mergeEmails(p.Emails, other.Emails)
	p.Phones = mergePhones(p.Phones, other.Phones)
	p.Socials = mergeSocials(p.Socials, other.Socials)
	p.People = mergePeople(p.People, other.People)
	p.RegistrationIDs = mergeRegistrationIDs(p.RegistrationIDs, other.RegistrationIDs)

	p.Addresses = mergeStrings(p.Addresses, other.Addresses)
	p.TradeRoles = mergeStrings(p.TradeRoles, other.TradeRoles)
	p.Certifications = mergeStrings(p.Certifications, other.Certifications)
	p.ProductKeywords = mergeStrings(p.ProductKeywords, other.ProductKeywords)
	p.Markets = mergeStrings(p.Markets, other.Markets)
	p.Languages = mergeStrings(p.Languages, other.Languages)
	p.PagesCrawled = mergeStrings(p.PagesCrawled, other.PagesCrawled)
}

// Finalize sorts the ranked lists, trims them to sane sizes and computes the
// completeness score. Call it once after the last Merge.
func (p *CompanyProfile) Finalize() {
	sortEmails(p.Emails)
	sortPeople(p.People)
	sort.Slice(p.Socials, func(i, j int) bool {
		if p.Socials[i].Network != p.Socials[j].Network {
			return p.Socials[i].Network < p.Socials[j].Network
		}

		return p.Socials[i].URL < p.Socials[j].URL
	})

	p.Emails = capSlice(p.Emails, maxEmails)
	p.Phones = capSlice(p.Phones, maxPhones)
	p.People = capSlice(p.People, maxPeople)
	p.ProductKeywords = capSlice(p.ProductKeywords, maxProductKeywords)
	p.Markets = capSlice(p.Markets, maxMarkets)

	if p.ResearchedAt.IsZero() {
		p.ResearchedAt = time.Now().UTC()
	}

	p.Completeness = p.score()
}

// Limits keep a single profile from ballooning a CSV cell while still holding
// everything a salesperson would actually act on.
const (
	maxEmails          = 20
	maxPhones          = 10
	maxPeople          = 25
	maxProductKeywords = 30
	maxMarkets         = 25
)

// scoreWeights maps each research dimension to its share of the 100-point
// completeness score. Contact reachability is weighted highest because a lead
// you cannot contact has no value regardless of how much else is known.
var scoreWeights = []struct {
	weight int
	has    func(*CompanyProfile) bool
}{
	{25, func(p *CompanyProfile) bool { return p.hasContactableEmail() }},
	{10, func(p *CompanyProfile) bool { return len(p.Emails) > 0 }},
	{10, func(p *CompanyProfile) bool { return len(p.Phones) > 0 }},
	{10, func(p *CompanyProfile) bool { return len(p.People) > 0 }},
	{5, func(p *CompanyProfile) bool { return p.hasSeniorPerson() }},
	{10, func(p *CompanyProfile) bool { return len(p.Socials) > 0 }},
	{5, func(p *CompanyProfile) bool { return p.hasNetwork("linkedin") }},
	{5, func(p *CompanyProfile) bool { return p.Description != "" }},
	{5, func(p *CompanyProfile) bool { return len(p.RegistrationIDs) > 0 }},
	{5, func(p *CompanyProfile) bool { return len(p.TradeRoles) > 0 }},
	{5, func(p *CompanyProfile) bool { return len(p.Certifications) > 0 }},
	{5, func(p *CompanyProfile) bool { return len(p.ProductKeywords) > 0 }},
}

func (p *CompanyProfile) score() int {
	total := 0

	for _, dim := range scoreWeights {
		if dim.has(p) {
			total += dim.weight
		}
	}

	return total
}

func (p *CompanyProfile) hasContactableEmail() bool {
	for i := range p.Emails {
		switch p.Emails[i].Kind {
		case EmailKindPersonal, EmailKindRole:
			return true
		case EmailKindGeneric, EmailKindSupport:
			continue
		}
	}

	return false
}

func (p *CompanyProfile) hasSeniorPerson() bool {
	for i := range p.People {
		if p.People[i].Seniority == SeniorityExecutive || p.People[i].Seniority == SeniorityManagement {
			return true
		}
	}

	return false
}

func (p *CompanyProfile) hasNetwork(network string) bool {
	for i := range p.Socials {
		if p.Socials[i].Network == network {
			return true
		}
	}

	return false
}

// IsEmpty reports whether nothing worth persisting was found.
func (p *CompanyProfile) IsEmpty() bool {
	return len(p.Emails) == 0 &&
		len(p.Phones) == 0 &&
		len(p.Socials) == 0 &&
		len(p.People) == 0 &&
		len(p.RegistrationIDs) == 0 &&
		p.Description == "" &&
		p.LegalName == ""
}

// EmailAddresses returns just the addresses, best first. It keeps the existing
// `emails` output column populated while the richer detail lives in the
// profile itself.
func (p *CompanyProfile) EmailAddresses() []string {
	if len(p.Emails) == 0 {
		return nil
	}

	out := make([]string, 0, len(p.Emails))
	for i := range p.Emails {
		out = append(out, p.Emails[i].Address)
	}

	return out
}

func preferLonger(dst *string, candidate string) {
	if len(strings.TrimSpace(candidate)) > len(strings.TrimSpace(*dst)) {
		*dst = candidate
	}
}

func preferFirst(dst *string, candidate string) {
	if *dst == "" {
		*dst = candidate
	}
}

func capSlice[T any](in []T, limit int) []T {
	if len(in) <= limit {
		return in
	}

	return in[:limit]
}
