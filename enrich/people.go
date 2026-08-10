package enrich

import (
	"regexp"
	"strings"
)

// Seniority buckets for Person.Seniority, ordered from most to least senior.
const (
	SeniorityExecutive  = "executive"
	SeniorityManagement = "management"
	SeniorityStaff      = "staff"
)

// executiveTitles are the people who can sign off on a deal.
var executiveTitles = []string{
	"ceo", "chief executive", "cto", "chief technology", "cfo",
	"chief financial", "coo", "chief operating", "cmo", "chief marketing",
	"cpo", "chief procurement", "cco", "chief commercial", "cso",
	"chief sales", "president", "vice president", "vp", "founder",
	"co-founder", "cofounder", "owner", "proprietor", "partner",
	"managing director", "general manager", "board member", "chairman",
	"chairwoman", "chairperson", "director", "geschäftsführer",
	"inhaber", "gerente general", "直属", "总经理", "创始人", "董事",
}

// managementTitles run a function and are usually the right first contact.
var managementTitles = []string{
	"head of", "manager", "lead", "supervisor", "principal", "chef",
	"leiter", "responsable", "responsabile", "jefe", "gerente",
	"coordinator", "team leader", "经理", "主管", "负责人",
}

// jobTitleKeywords gate name/title extraction from free text: a phrase only
// counts as a job title when it contains one of these.
var jobTitleKeywords = append(append([]string{},
	executiveTitles...),
	append([]string{
		"sales", "export", "import", "purchasing", "procurement", "sourcing",
		"business development", "account", "marketing", "operations",
		"logistics", "quality", "engineer", "technical", "customer",
		"representative", "consultant", "specialist", "executive", "officer",
		"administrator", "assistant", "designer", "developer",
	}, managementTitles...)...,
)

// namePattern matches 2-4 capitalized words, allowing internal apostrophes,
// hyphens, nobiliary particles and middle initials, which covers most
// Latin-script personal names. A period is only allowed as part of an initial
// ("John A. Smith"), never at the end of a word, so a name cannot swallow the
// start of the next sentence.
const namePattern = `[A-Z][\p{L}'’\-]{1,20}` +
	`(?:\s+(?:van|von|de|del|della|da|di|dos|der|den|ter|bin|binte|al|el|la|le)\.?)?` +
	`(?:\s+(?:[A-Z]\.|[A-Z][\p{L}'’\-]{1,20})){1,3}`

// titlePattern matches a job title: words, spaces and a few connectors. A
// period is deliberately excluded so a match cannot run across a sentence
// boundary and glue two people's titles together.
const titlePattern = `[\p{L}][\p{L}\s&/\-'’,]{2,60}`

// peoplePatterns extract "name then title" and "title then name" layouts. Team
// pages, email signatures and Impressum blocks all reduce to one of these.
var peoplePatterns = []struct {
	re        *regexp.Regexp
	nameFirst bool
}{
	// "Jane Doe, Export Sales Manager" / "Jane Doe - Head of Purchasing"
	{regexp.MustCompile(`(` + namePattern + `)\s*(?:,|\||—|–|-|:)\s*(` + titlePattern + `)`), true},
	// "Export Manager: Jane Doe" / "CEO — Jane Doe"
	{regexp.MustCompile(`(` + titlePattern + `)\s*(?::|—|–|-)\s*(` + namePattern + `)`), false},
}

// nameStopWords are page-furniture words that the name pattern would otherwise
// accept because they are capitalized.
var nameStopWords = map[string]bool{
	"the": true, "and": true, "our": true, "your": true, "we": true,
	"home": true, "about": true, "contact": true, "privacy": true,
	"policy": true, "terms": true, "cookie": true, "cookies": true,
	"copyright": true, "rights": true, "reserved": true, "all": true,
	"read": true, "more": true, "learn": true, "view": true, "click": true,
	"sign": true, "log": true, "menu": true, "search": true, "share": true,
	"company": true, "products": true, "services": true, "solutions": true,
	"news": true, "blog": true, "careers": true, "support": true,
	"shipping": true, "returns": true, "faq": true, "help": true,
	"newsletter": true, "subscribe": true, "follow": true, "us": true,
	"team": true, "meet": true, "join": true, "get": true, "in": true,
	"touch": true, "quote": true, "request": true, "download": true,
	"catalogue": true, "catalog": true, "brochure": true, "email": true,
	"phone": true, "address": true, "office": true, "headquarters": true,
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true, "january": true,
	"february": true, "march": true, "april": true, "may": true,
	"june": true, "july": true, "august": true, "september": true,
	"october": true, "november": true, "december": true,
}

// ClassifySeniority maps a job title onto a seniority bucket. An empty title
// yields an empty bucket rather than guessing.
func ClassifySeniority(title string) string {
	if strings.TrimSpace(title) == "" {
		return ""
	}

	lower := strings.ToLower(title)

	for _, needle := range executiveTitles {
		if containsWord(lower, needle) {
			return SeniorityExecutive
		}
	}

	for _, needle := range managementTitles {
		if strings.Contains(lower, needle) {
			return SeniorityManagement
		}
	}

	return SeniorityStaff
}

func seniorityRank(seniority string) int {
	switch seniority {
	case SeniorityExecutive:
		return 0
	case SeniorityManagement:
		return 1
	case SeniorityStaff:
		return 2
	default:
		return 3
	}
}

// ExtractPeopleFromText finds decision-maker candidates in page text. Only
// pairs where the title half contains a recognised job-title word are kept, so
// ordinary prose does not turn into contacts.
func ExtractPeopleFromText(text, source string) []Person {
	var out []Person

	for _, pattern := range peoplePatterns {
		for _, match := range pattern.re.FindAllStringSubmatch(text, -1) {
			var name, title string

			if pattern.nameFirst {
				name, title = match[1], match[2]
			} else {
				title, name = match[1], match[2]
			}

			person, ok := newPerson(name, title, source)
			if !ok {
				continue
			}

			out = append(out, person)
		}
	}

	return out
}

func newPerson(name, title, source string) (Person, bool) {
	name = cleanPersonName(name)
	title = cleanJobTitle(title)

	if name == "" || title == "" {
		return Person{}, false
	}

	if !looksLikeJobTitle(title) {
		return Person{}, false
	}

	// A "name" that is really a title (e.g. "Sales Manager, Export") would
	// otherwise be recorded as a person.
	if looksLikeJobTitle(name) {
		return Person{}, false
	}

	return Person{
		Name:      name,
		Title:     title,
		Seniority: ClassifySeniority(title),
		Source:    source,
	}, true
}

// maxNameWords bounds a personal name; longer capitalized runs are headlines.
const maxNameWords = 4

func cleanPersonName(raw string) string {
	fields := strings.Fields(strings.Trim(strings.TrimSpace(raw), ".,;:-–—|"))
	if len(fields) < 2 || len(fields) > maxNameWords {
		return ""
	}

	for _, field := range fields {
		if nameStopWords[strings.ToLower(strings.Trim(field, ".,'’-"))] {
			return ""
		}

		// Digits never appear in a personal name but are common in addresses.
		if strings.ContainsAny(field, "0123456789@") {
			return ""
		}
	}

	return strings.Join(fields, " ")
}

// maxTitleWords bounds a job title; anything longer is a sentence.
const maxTitleWords = 8

func cleanJobTitle(raw string) string {
	title := strings.Join(strings.Fields(raw), " ")
	title = strings.Trim(title, " .,;:-–—|")

	if title == "" {
		return ""
	}

	if len(strings.Fields(title)) > maxTitleWords {
		return ""
	}

	return title
}

func looksLikeJobTitle(candidate string) bool {
	lower := strings.ToLower(candidate)

	for _, keyword := range jobTitleKeywords {
		if containsWord(lower, keyword) {
			return true
		}
	}

	return false
}
