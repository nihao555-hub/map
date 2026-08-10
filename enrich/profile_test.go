package enrich_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gosom/google-maps-scraper/enrich"
)

func TestMergeKeepsStrongestValues(t *testing.T) {
	t.Parallel()

	homepage := &enrich.CompanyProfile{
		LegalName:    "Acme GmbH",
		Description:  "Tools.",
		Domain:       "acme.de",
		FoundedYear:  0,
		TradeRoles:   []string{"manufacturer"},
		PagesCrawled: []string{"https://acme.de/"},
		Emails: []enrich.Email{
			{Address: "info@acme.de", Kind: enrich.EmailKindGeneric, OnDomain: true},
		},
	}

	imprint := &enrich.CompanyProfile{
		LegalName:    "Acme Gesellschaft mit beschränkter Haftung",
		Description:  "A much longer and more useful description of the company.",
		Domain:       "acme.com",
		FoundedYear:  1990,
		TradeRoles:   []string{"Manufacturer", "exporter"},
		PagesCrawled: []string{"https://acme.de/impressum"},
		Emails: []enrich.Email{
			// Same address, stronger classification.
			{Address: "INFO@acme.de", Kind: enrich.EmailKindRole, Source: "https://acme.de/impressum"},
			{Address: "export@acme.de", Kind: enrich.EmailKindRole, OnDomain: true},
		},
	}

	homepage.Merge(imprint)

	assert.Equal(t, "Acme Gesellschaft mit beschränkter Haftung", homepage.LegalName)
	assert.Equal(t, "A much longer and more useful description of the company.", homepage.Description)
	assert.Equal(t, "acme.de", homepage.Domain, "the first non-empty scalar wins")
	assert.Equal(t, 1990, homepage.FoundedYear)

	assert.Equal(t, []string{"manufacturer", "exporter"}, homepage.TradeRoles,
		"list merge is case-insensitive and preserves first-seen casing")
	assert.Equal(t, []string{"https://acme.de/", "https://acme.de/impressum"}, homepage.PagesCrawled)

	require.Len(t, homepage.Emails, 2, "the same address seen twice must collapse")
	assert.Equal(t, enrich.EmailKindRole, homepage.Emails[0].Kind,
		"the stronger classification must win on merge")
	assert.Equal(t, "https://acme.de/impressum", homepage.Emails[0].Source,
		"a missing source must be filled in from the later observation")
}

func TestMergeIgnoresNil(t *testing.T) {
	t.Parallel()

	profile := &enrich.CompanyProfile{LegalName: "Acme"}

	profile.Merge(nil)

	assert.Equal(t, "Acme", profile.LegalName)
}

func TestMergeDedupesPeopleAndKeepsBestDetail(t *testing.T) {
	t.Parallel()

	profile := &enrich.CompanyProfile{
		People: []enrich.Person{{Name: "Jane Doe", Title: "Manager", Seniority: enrich.SeniorityManagement}},
	}

	profile.Merge(&enrich.CompanyProfile{
		People: []enrich.Person{{
			Name:      "jane  doe",
			Title:     "Managing Director",
			Email:     "jane@acme.de",
			Seniority: enrich.SeniorityExecutive,
		}},
	})

	require.Len(t, profile.People, 1)
	assert.Equal(t, "Managing Director", profile.People[0].Title)
	assert.Equal(t, "jane@acme.de", profile.People[0].Email)
	assert.Equal(t, enrich.SeniorityExecutive, profile.People[0].Seniority)
}

func TestFinalizeRanksEmailsForOutreach(t *testing.T) {
	t.Parallel()

	profile := &enrich.CompanyProfile{
		Emails: []enrich.Email{
			{Address: "support@acme.de", Kind: enrich.EmailKindSupport, OnDomain: true},
			{Address: "hello@agency.io", Kind: enrich.EmailKindPersonal},
			{Address: "info@acme.de", Kind: enrich.EmailKindGeneric, OnDomain: true},
			{Address: "purchasing@acme.de", Kind: enrich.EmailKindRole, OnDomain: true},
		},
	}

	profile.Finalize()

	assert.Equal(t, []string{
		"purchasing@acme.de",
		"info@acme.de",
		"support@acme.de",
		"hello@agency.io",
	}, profile.EmailAddresses(), "on-domain addresses first, then by outreach value")
}

func TestCompletenessRewardsActionableData(t *testing.T) {
	t.Parallel()

	empty := &enrich.CompanyProfile{}
	empty.Finalize()

	assert.Equal(t, 0, empty.Completeness)
	assert.True(t, empty.IsEmpty())

	generic := &enrich.CompanyProfile{
		Emails: []enrich.Email{{Address: "info@acme.de", Kind: enrich.EmailKindGeneric}},
	}
	generic.Finalize()

	actionable := &enrich.CompanyProfile{
		Emails: []enrich.Email{{Address: "purchasing@acme.de", Kind: enrich.EmailKindRole}},
	}
	actionable.Finalize()

	assert.Greater(t, actionable.Completeness, generic.Completeness,
		"a role mailbox is worth more than a catch-all address")
}

func TestFinalizeCapsRunawayLists(t *testing.T) {
	t.Parallel()

	profile := &enrich.CompanyProfile{}

	for i := range 100 {
		profile.ProductKeywords = append(profile.ProductKeywords, string(rune('a'+i%26))+string(rune('a'+i/26)))
	}

	profile.Finalize()

	assert.LessOrEqual(t, len(profile.ProductKeywords), 30)
}

func TestEmailAddressesEmptyProfile(t *testing.T) {
	t.Parallel()

	profile := &enrich.CompanyProfile{}

	assert.Nil(t, profile.EmailAddresses())
}
