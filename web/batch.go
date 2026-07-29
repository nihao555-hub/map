package web

import "strings"

// ExpandBatchKeywords returns the Cartesian product of business types and locations.
func ExpandBatchKeywords(types, locations []string) []string {
	queries := make([]string, 0, len(types)*max(1, len(locations)))

	for _, businessType := range cleanBatchValues(types) {
		if len(locations) == 0 {
			queries = append(queries, businessType)

			continue
		}

		for _, location := range cleanBatchValues(locations) {
			queries = append(queries, businessType+" in "+location)
		}
	}

	return queries
}

func cleanBatchValues(values []string) []string {
	cleaned := make([]string, 0, len(values))

	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || r == '\n'
		}) {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				cleaned = append(cleaned, trimmed)
			}
		}
	}

	return cleaned
}
