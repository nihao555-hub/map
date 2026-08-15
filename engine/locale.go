package engine

import "strings"

// LangForCountry is the same country → language table 地图获客 uses for Google hl.
// Unknown codes return "".
func LangForCountry(countryCode string) string {
	switch strings.ToLower(strings.TrimSpace(countryCode)) {
	case "cn", "hk", "tw", "mo":
		return "zh"
	case "jp":
		return "ja"
	case "kr":
		return "ko"
	case "th":
		return "th"
	case "vn":
		return "vi"
	case "id":
		return "id"
	case "my":
		return "ms"
	case "sg", "ph", "in", "us", "gb", "au", "nz", "ca":
		return "en"
	case "de", "at", "ch":
		return "de"
	case "fr":
		return "fr"
	case "es", "mx":
		return "es"
	case "it":
		return "it"
	case "pt", "br":
		return "pt"
	case "ru":
		return "ru"
	case "ua":
		return "uk"
	case "tr":
		return "tr"
	case "nl":
		return "nl"
	case "pl":
		return "pl"
	case "se":
		return "sv"
	case "ae", "sa", "eg":
		return "ar"
	default:
		return ""
	}
}

type productLangTerms map[string][]string

// productGlossary maps a folded Chinese/English product name onto local search phrases.
var productGlossary = map[string]productLangTerms{
	"电动工具": {
		"en": {"power tools", "electric tools"},
		"th": {"เครื่องมือไฟฟ้า", "เครื่องใช้ไฟฟ้า"},
		"vi": {"máy công cụ điện", "dụng cụ điện"},
		"ms": {"alatan kuasa"},
		"id": {"perkakas listrik"},
		"ja": {"電動工具"},
		"ko": {"전동공구"},
		"de": {"Elektrowerkzeuge"},
		"fr": {"outils électriques"},
		"es": {"herramientas eléctricas"},
		"pt": {"ferramentas elétricas"},
		"ar": {"أدوات كهربائية"},
		"ru": {"электроинструмент"},
	},
	"power tools": {
		"en": {"power tools", "electric tools"},
		"th": {"เครื่องมือไฟฟ้า", "เครื่องใช้ไฟฟ้า"},
		"zh": {"电动工具"},
	},
	"led灯": {
		"en": {"LED light", "LED lamp"},
		"th": {"ไฟ LED", "หลอด LED"},
		"vi": {"đèn LED"},
		"ms": {"lampu LED"},
		"id": {"lampu LED"},
	},
	"led light": {
		"en": {"LED light", "LED lamp"},
		"th": {"ไฟ LED"},
		"zh": {"LED灯"},
	},
	"furniture": {
		"en": {"furniture"},
		"th": {"เฟอร์นิเจอร์"},
		"zh": {"家具"},
		"vi": {"nội thất"},
	},
	"家具": {
		"en": {"furniture"},
		"th": {"เฟอร์นิเจอร์"},
		"vi": {"nội thất"},
	},
}

const maxLocalTerms = 6

// LocalSearchTerms expands a product keyword into phrases locals actually type.
// It always keeps the original keyword and adds English + the target-country language.
func LocalSearchTerms(keyword, country string) []string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil
	}

	lang := LangForCountry(country)
	out := []string{keyword}
	entry := productGlossary[foldSearchText(keyword)]
	if entry != nil {
		out = append(out, entry["en"]...)
		if lang != "" && lang != "en" {
			out = append(out, entry[lang]...)
		}
		if lang == "" {
			out = append(out, entry["en"]...)
		}
	} else if lang != "" && lang != "zh" && hasCJK(keyword) {
		out = append(out, keyword)
	}

	return clipTerms(uniqueFoldedStrings(out), maxLocalTerms)
}

func glossaryAliases(keyword string) []string {
	entry := productGlossary[foldSearchText(keyword)]
	if entry == nil {
		return nil
	}
	var out []string
	for _, terms := range entry {
		out = append(out, terms...)
	}

	return uniqueFoldedStrings(out)
}

func localIntentWords(lang, role string) []string {
	if NormalizeRole(role) == RoleSeller {
		switch lang {
		case "th":
			return []string{"ขายส่ง", "โรงงาน"}
		case "vi":
			return []string{"bán sỉ", "nhà máy"}
		case "zh":
			return []string{"批发", "厂家"}
		default:
			return []string{"wholesaler", "manufacturer"}
		}
	}
	switch lang {
	case "th":
		return []string{"นำเข้า", "ผู้นำเข้า"}
	case "vi":
		return []string{"nhập khẩu"}
	case "ms":
		return []string{"pengimport"}
	case "id":
		return []string{"importir"}
	case "zh":
		return []string{"采购", "进口商"}
	default:
		return []string{"importer", "buyer"}
	}
}

func uniqueFoldedStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := foldSearchText(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}

	return out
}

func clipTerms(in []string, n int) []string {
	if n <= 0 || len(in) <= n {
		return in
	}

	return in[:n]
}
