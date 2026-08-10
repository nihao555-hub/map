package enrich

import (
	"sort"
	"strings"
)

// mergeStrings appends the values of b that a does not already hold, comparing
// case-insensitively but preserving the casing of the first occurrence.
func mergeStrings(a, b []string) []string {
	if len(b) == 0 {
		return a
	}

	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))

	for _, list := range [][]string{a, b} {
		for _, v := range list {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}

			key := strings.ToLower(v)
			if seen[key] {
				continue
			}

			seen[key] = true
			out = append(out, v)
		}
	}

	return out
}

func mergeEmails(a, b []Email) []Email {
	if len(b) == 0 {
		return a
	}

	index := make(map[string]int, len(a)+len(b))
	out := make([]Email, 0, len(a)+len(b))

	for _, list := range [][]Email{a, b} {
		for _, e := range list {
			key := strings.ToLower(e.Address)
			if key == "" {
				continue
			}

			if at, ok := index[key]; ok {
				// Keep the stronger classification and the first source.
				if emailKindRank(e.Kind) < emailKindRank(out[at].Kind) {
					out[at].Kind = e.Kind
				}

				if out[at].Source == "" {
					out[at].Source = e.Source
				}

				continue
			}

			index[key] = len(out)
			out = append(out, e)
		}
	}

	return out
}

func mergePhones(a, b []Phone) []Phone {
	if len(b) == 0 {
		return a
	}

	index := make(map[string]int, len(a)+len(b))
	out := make([]Phone, 0, len(a)+len(b))

	for _, list := range [][]Phone{a, b} {
		for _, ph := range list {
			key := phoneKey(ph)
			if key == "" {
				continue
			}

			if at, ok := index[key]; ok {
				if ph.WhatsApp {
					out[at].WhatsApp = true
				}

				if out[at].E164 == "" {
					out[at].E164 = ph.E164
				}

				continue
			}

			index[key] = len(out)
			out = append(out, ph)
		}
	}

	return out
}

func phoneKey(p Phone) string {
	if p.E164 != "" {
		return p.E164
	}

	return digitsOnly(p.Raw)
}

func mergeSocials(a, b []Social) []Social {
	if len(b) == 0 {
		return a
	}

	seen := make(map[string]bool, len(a)+len(b))
	out := make([]Social, 0, len(a)+len(b))

	for _, list := range [][]Social{a, b} {
		for _, s := range list {
			key := s.Network + "|" + strings.ToLower(s.URL)
			if s.URL == "" || seen[key] {
				continue
			}

			seen[key] = true
			out = append(out, s)
		}
	}

	return out
}

func mergePeople(a, b []Person) []Person {
	if len(b) == 0 {
		return a
	}

	index := make(map[string]int, len(a)+len(b))
	out := make([]Person, 0, len(a)+len(b))

	for _, list := range [][]Person{a, b} {
		for _, p := range list {
			key := strings.ToLower(strings.Join(strings.Fields(p.Name), " "))
			if key == "" {
				continue
			}

			if at, ok := index[key]; ok {
				preferLonger(&out[at].Title, p.Title)
				preferFirst(&out[at].Email, p.Email)
				preferFirst(&out[at].LinkedIn, p.LinkedIn)

				if out[at].Seniority == "" || seniorityRank(p.Seniority) < seniorityRank(out[at].Seniority) {
					out[at].Seniority = p.Seniority
				}

				continue
			}

			index[key] = len(out)
			out = append(out, p)
		}
	}

	return out
}

func mergeRegistrationIDs(a, b []RegistrationID) []RegistrationID {
	if len(b) == 0 {
		return a
	}

	seen := make(map[string]bool, len(a)+len(b))
	out := make([]RegistrationID, 0, len(a)+len(b))

	for _, list := range [][]RegistrationID{a, b} {
		for _, r := range list {
			key := r.Kind + "|" + strings.ToUpper(strings.ReplaceAll(r.Value, " ", ""))
			if r.Value == "" || seen[key] {
				continue
			}

			seen[key] = true
			out = append(out, r)
		}
	}

	return out
}

// emailKindRank orders classifications by outreach value, lowest is best.
func emailKindRank(k EmailKind) int {
	switch k {
	case EmailKindPersonal:
		return 0
	case EmailKindRole:
		return 1
	case EmailKindGeneric:
		return 2
	case EmailKindSupport:
		return 3
	default:
		return 4
	}
}

// sortEmails ranks on-domain addresses first, then by classification, so the
// first address is the one a salesperson should actually write to.
func sortEmails(emails []Email) {
	sort.SliceStable(emails, func(i, j int) bool {
		if emails[i].OnDomain != emails[j].OnDomain {
			return emails[i].OnDomain
		}

		ri, rj := emailKindRank(emails[i].Kind), emailKindRank(emails[j].Kind)
		if ri != rj {
			return ri < rj
		}

		return emails[i].Address < emails[j].Address
	})
}

func sortPeople(people []Person) {
	sort.SliceStable(people, func(i, j int) bool {
		ri, rj := seniorityRank(people[i].Seniority), seniorityRank(people[j].Seniority)
		if ri != rj {
			return ri < rj
		}

		return people[i].Name < people[j].Name
	})
}
