package enrich

import (
	"encoding/json"
	"strconv"
	"strings"
)

// organizationTypes are the schema.org types that describe the company itself
// rather than a page, product or breadcrumb.
var organizationTypes = map[string]bool{
	"organization": true, "corporation": true, "localbusiness": true,
	"company": true, "ngo": true, "onlinebusiness": true, "onlinestore": true,
	"store": true, "wholesalestore": true, "manufacturer": true,
	"professionalservice": true, "homeandconstructionbusiness": true,
	"automotivebusiness": true, "foodestablishment": true, "restaurant": true,
	"medicalorganization": true, "educationalorganization": true,
	"governmentorganization": true, "sportsorganization": true,
	"performinggroup": true, "travelagency": true, "legalservice": true,
	"financialservice": true, "insuranceagency": true, "realestateagent": true,
	"generalcontractor": true, "hotel": true, "lodgingbusiness": true,
}

// registrationFields maps schema.org properties to the RegistrationID kind we
// publish. These are the identifiers that let a buyer be looked up in an
// official registry.
var registrationFields = map[string]string{
	"vatid":                   "vat",
	"taxid":                   "tax",
	"duns":                    "duns",
	"leicode":                 "lei",
	"globallocationnumber":    "gln",
	"isicv4":                  "isic",
	"naics":                   "naics",
	"iso6523code":             "iso6523",
	"registrationnumber":      "registration",
	"legalregistrationnumber": "registration",
}

// ParseJSONLD extracts company facts from the JSON-LD blocks of a page. Blocks
// that do not describe an organization are ignored.
func ParseJSONLD(blocks []string, source string) *CompanyProfile {
	profile := &CompanyProfile{}

	for _, block := range blocks {
		var decoded any
		if err := json.Unmarshal([]byte(strings.TrimSpace(block)), &decoded); err != nil {
			continue
		}

		walkJSONLD(decoded, profile, source)
	}

	return profile
}

func walkJSONLD(node any, profile *CompanyProfile, source string) {
	switch typed := node.(type) {
	case []any:
		for _, child := range typed {
			walkJSONLD(child, profile, source)
		}
	case map[string]any:
		if graph, ok := typed["@graph"]; ok {
			walkJSONLD(graph, profile, source)
		}

		if isOrganizationNode(typed) {
			applyOrganizationNode(typed, profile, source)
		}

		// Organizations are frequently nested under publisher/author/provider.
		for _, key := range []string{"publisher", "author", "provider", "brand", "parentOrganization", "sourceOrganization", "manufacturer", "seller"} {
			if child, ok := typed[key]; ok {
				walkJSONLD(child, profile, source)
			}
		}
	}
}

func isOrganizationNode(node map[string]any) bool {
	for _, value := range jsonLDStrings(node["@type"]) {
		if organizationTypes[strings.ToLower(strings.TrimPrefix(value, "schema:"))] {
			return true
		}
	}

	return false
}

//nolint:gocyclo // A flat sequence of independent field extractions is clearer here than splitting it across helpers.
func applyOrganizationNode(node map[string]any, profile *CompanyProfile, source string) {
	if legal := firstJSONLDString(node, "legalName", "name"); legal != "" {
		preferLonger(&profile.LegalName, legal)
	}

	if description := firstJSONLDString(node, "description", "slogan"); description != "" {
		preferLonger(&profile.Description, description)
	}

	if logo := jsonLDImage(node["logo"]); logo != "" {
		preferFirst(&profile.LogoURL, logo)
	}

	if year := parseFoundingYear(firstJSONLDString(node, "foundingDate", "dissolutionDate")); year > 0 {
		if profile.FoundedYear == 0 {
			profile.FoundedYear = year
		}
	}

	for field, kind := range registrationFields {
		for key, value := range node {
			if strings.ToLower(key) != field {
				continue
			}

			for _, id := range jsonLDStrings(value) {
				if id = strings.TrimSpace(id); id != "" {
					profile.RegistrationIDs = append(profile.RegistrationIDs, RegistrationID{
						Kind:    kind,
						Value:   id,
						Country: registrationCountry(kind, id),
						Source:  source,
					})
				}
			}
		}
	}

	for _, email := range jsonLDStrings(node["email"]) {
		email = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(email)), "mailto:")
		if address, ok := normalizeEmail(email); ok {
			profile.Emails = append(profile.Emails, Email{
				Address: address,
				Kind:    classifyLocalPart(strings.SplitN(address, "@", 2)[0]),
				Source:  source,
			})
		}
	}

	for _, telephone := range jsonLDStrings(node["telephone"]) {
		if phone, ok := newPhone(telephone, false, source); ok {
			profile.Phones = append(profile.Phones, phone)
		}
	}

	if address := formatPostalAddress(node["address"]); address != "" {
		profile.Addresses = append(profile.Addresses, address)
	}

	profile.Socials = append(profile.Socials, ExtractSocials(jsonLDStrings(node["sameAs"]), "")...)

	if headcount := parseEmployeeCount(node["numberOfEmployees"]); headcount != "" {
		preferFirst(&profile.EmployeeRange, headcount)
	}

	for _, key := range []string{"founder", "employee", "member"} {
		profile.People = append(profile.People, jsonLDPeople(node[key], source)...)
	}
}

func jsonLDPeople(node any, source string) []Person {
	var out []Person

	switch typed := node.(type) {
	case []any:
		for _, child := range typed {
			out = append(out, jsonLDPeople(child, source)...)
		}
	case map[string]any:
		name := strings.TrimSpace(firstJSONLDString(typed, "name"))
		if name == "" {
			return nil
		}

		title := strings.TrimSpace(firstJSONLDString(typed, "jobTitle"))

		out = append(out, Person{
			Name:      name,
			Title:     title,
			Email:     strings.TrimPrefix(strings.TrimSpace(firstJSONLDString(typed, "email")), "mailto:"),
			Seniority: ClassifySeniority(title),
			Source:    source,
		})
	}

	return out
}

func formatPostalAddress(node any) string {
	switch typed := node.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		for _, child := range typed {
			if formatted := formatPostalAddress(child); formatted != "" {
				return formatted
			}
		}
	case map[string]any:
		parts := make([]string, 0, 5)

		for _, key := range []string{"streetAddress", "addressLocality", "addressRegion", "postalCode", "addressCountry"} {
			value := firstJSONLDString(typed, key)
			if value == "" {
				if nested, ok := typed[key].(map[string]any); ok {
					value = firstJSONLDString(nested, "name")
				}
			}

			if value != "" {
				parts = append(parts, value)
			}
		}

		return strings.Join(parts, ", ")
	}

	return ""
}

func parseEmployeeCount(node any) string {
	switch typed := node.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.Itoa(int(typed))
	case map[string]any:
		if value := firstJSONLDString(typed, "value", "name"); value != "" {
			return value
		}

		minValue := firstJSONLDString(typed, "minValue")
		maxValue := firstJSONLDString(typed, "maxValue")

		if minValue != "" && maxValue != "" {
			return minValue + "-" + maxValue
		}

		if minValue != "" {
			return minValue + "+"
		}

		if number, ok := typed["value"].(float64); ok {
			return strconv.Itoa(int(number))
		}
	}

	return ""
}

func parseFoundingYear(value string) int {
	if len(value) < 4 {
		return 0
	}

	year, err := strconv.Atoi(value[:4])
	if err != nil {
		return 0
	}

	return plausibleYear(year)
}

// jsonLDStrings normalizes a JSON-LD value that may be a string, a number, a
// list, or an object carrying the value under "url"/"@id"/"name".
func jsonLDStrings(node any) []string {
	switch typed := node.(type) {
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	case float64:
		return []string{strconv.FormatFloat(typed, 'f', -1, 64)}
	case []any:
		var out []string

		for _, child := range typed {
			out = append(out, jsonLDStrings(child)...)
		}

		return out
	case map[string]any:
		for _, key := range []string{"url", "@id", "name", "value", "@value"} {
			if value, ok := typed[key]; ok {
				if strings, ok := value.(string); ok && strings != "" {
					return []string{strings}
				}
			}
		}
	}

	return nil
}

func firstJSONLDString(node map[string]any, keys ...string) string {
	for _, key := range keys {
		if values := jsonLDStrings(node[key]); len(values) > 0 {
			return values[0]
		}
	}

	return ""
}

func jsonLDImage(node any) string {
	values := jsonLDStrings(node)
	if len(values) == 0 {
		return ""
	}

	return values[0]
}
