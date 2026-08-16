package engine

import (
	"strings"
	"unicode"
)

// foldLegalName strips punctuation and trailing legal-form suffixes so
// "Signify Holding B.V." and "Signify" share a key.
func foldLegalName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	for _, suf := range cjkLegalSuffixes {
		s = strings.TrimSuffix(s, suf)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '&':
			b.WriteString(" and ")
		default:
			b.WriteByte(' ')
		}
	}
	fields := strings.Fields(b.String())
	if len(fields) > 1 && fields[0] == "the" {
		fields = fields[1:]
	}
	for stripped := true; stripped && len(fields) > 0; {
		stripped = false
		last := fields[len(fields)-1]
		if legalNameSuffixes[last] {
			fields = fields[:len(fields)-1]
			stripped = true
			continue
		}
		if len(fields) >= 2 {
			two := fields[len(fields)-2] + " " + last
			if legalNameSuffixes[two] {
				fields = fields[:len(fields)-2]
				stripped = true
			}
		}
	}
	return strings.Join(fields, " ")
}

func nameCountryKey(name, country string) string {
	folded := foldLegalName(name)
	if folded == "" || genericFoldedNames[folded] || len([]rune(folded)) < 4 {
		return ""
	}
	cc := strings.ToUpper(strings.TrimSpace(country))
	if len(cc) != 2 {
		return ""
	}
	return cc + "|" + folded
}

var cjkLegalSuffixes = []string{
	"股份有限公司", "有限责任公司", "有限公司", "集团有限公司",
	"株式会社", "有限会社",
}

var legalNameSuffixes = map[string]bool{
	"ltd": true, "limited": true, "llc": true, "inc": true, "incorporated": true,
	"corp": true, "corporation": true, "plc": true, "gmbh": true, "ag": true,
	"kg": true, "ohg": true, "ug": true, "bv": true, "nv": true, "sa": true,
	"sas": true, "sarl": true, "srl": true, "spa": true, "oy": true, "ab": true,
	"as": true, "pte": true, "bhd": true, "berhad": true, "sdn": true,
	"co": true, "company": true, "holding": true, "holdings": true,
	"kk": true, "pty": true, "llp": true, "lp": true, "pc": true, "cv": true,
	"vof": true, "ek": true, "ev": true, "se": true, "sca": true, "scs": true,
	"snc": true, "eurl": true, "sprl": true, "bvba": true, "asbl": true,
	"vzw": true, "kft": true, "zrt": true, "sro": true, "doo": true,
	"jsc": true, "pjsc": true, "ooo": true, "lda": true, "sl": true, "slu": true,
	"pvt": true, "private": true, "unipessoal": true, "limitada": true,
	"sdn bhd": true, "b v": true, "n v": true, "s a": true, "s p a": true,
	"p l c": true, "l l c": true, "a s": true, "s r l": true, "s a s": true,
}

var genericFoldedNames = map[string]bool{
	"group": true, "holdings": true, "holding": true, "trading": true,
	"company": true, "international": true, "services": true, "consulting": true,
	"investment": true, "capital": true, "partners": true, "industries": true,
	"enterprise": true, "enterprises": true, "technology": true, "technologies": true,
	"solutions": true, "global": true, "asia": true, "europe": true,
	"foundation": true, "trust": true, "fund": true, "bank": true,
	"insurance": true, "industrial": true,
}
