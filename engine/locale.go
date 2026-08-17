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
// Keep phrases specific (switchgear, panel listrik) so directory FTS does not
// match every company with "electric" or "power" in the legal name.
var productGlossary = map[string]productLangTerms{
	"电动工具": {
		"en": {"power tools", "electric tools"},
		"th": {"เครื่องมือไฟฟ้า", "เครื่องใช้ไฟฟ้า"},
		"vi": {"máy công cụ điện", "dụng cụ điện"},
		"ms": {"alatan kuasa"},
		"id": {"perkakas listrik"},
		"zh": {"电动工具", "五金工具"},
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
		"en": {"LED light", "LED lamp", "LED lighting"},
		"th": {"ไฟ LED", "หลอด LED"},
		"vi": {"đèn LED"},
		"ms": {"lampu LED"},
		"id": {"lampu LED"},
		"zh": {"LED灯", "灯饰", "灯具"},
	},
	"led light": {
		"en": {"LED light", "LED lamp", "LED lighting"},
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
	"配电柜":                switchgearTerms,
	"配电箱":                switchgearTerms,
	"配电盘":                switchgearTerms,
	"配电":                 switchgearTerms,
	"开关柜":                switchgearTerms,
	"switchgear":         switchgearTerms,
	"distribution board": switchgearTerms,
	"电缆": {
		"en": {"power cable", "electrical cable"},
		"id": {"kabel listrik", "kabel power"},
		"th": {"สายไฟ"},
		"vi": {"cáp điện"},
		"ms": {"kabel elektrik"},
		"zh": {"电缆"},
	},
	"电线": {
		"en": {"electric wire", "electrical wire"},
		"id": {"kabel listrik"},
		"th": {"สายไฟ"},
		"vi": {"dây điện"},
		"zh": {"电线"},
	},
	"太阳能": {
		"en": {"solar panel", "solar energy"},
		"id": {"panel surya", "tenaga surya"},
		"th": {"โซลาร์เซลล์"},
		"vi": {"tấm pin mặt trời"},
		"ms": {"panel solar"},
		"zh": {"太阳能"},
	},
	"光伏": {
		"en": {"solar panel", "photovoltaic"},
		"id": {"panel surya"},
		"zh": {"光伏"},
	},
	"阀门": {
		"en": {"industrial valve", "valve"},
		"id": {"katup industri", "valve"},
		"th": {"วาล์ว"},
		"vi": {"van công nghiệp"},
		"zh": {"阀门"},
	},
	"服装": {
		"en": {"clothes", "garment"},
		"id": {"pakaian", "garmen"},
		"th": {"เสื้อผ้า"},
		"vi": {"quần áo"},
		"zh": {"服装"},
	},
	"衣服": {
		"en": {"clothes", "garment"},
		"id": {"pakaian"},
		"zh": {"衣服"},
	},
	"鞋子": {
		"en": {"shoes", "footwear"},
		"id": {"sepatu"},
		"th": {"รองเท้า"},
		"vi": {"giày"},
		"zh": {"鞋子"},
	},
	"鞋": {
		"en": {"shoes", "footwear"},
		"id": {"sepatu"},
		"zh": {"鞋"},
	},
}

var switchgearTerms = productLangTerms{
	"en": {"switchgear", "distribution board", "electrical panel"},
	"id": {"panel listrik", "lemari listrik", "panel distribusi", "toko listrik"},
	"th": {"ตู้ไฟฟ้า", "สวิตช์เกียร์"},
	"vi": {"tủ điện", "tủ phân phối"},
	"ms": {"papan suis", "switchgear"},
	"zh": {"配电柜", "配电箱"},
}

const maxLocalTerms = 8

func lookupGlossary(keyword string) productLangTerms {
	key := foldSearchText(keyword)
	if key == "" {
		return nil
	}
	if entry := productGlossary[key]; entry != nil {
		return entry
	}
	best := ""
	for k := range productGlossary {
		if len(k) < 2 || !strings.Contains(key, k) {
			continue
		}
		if len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return nil
	}
	return productGlossary[best]
}

// LocalSearchTerms expands a product keyword into phrases locals actually type.
// It always keeps the original keyword and adds English + the target-country language.
func LocalSearchTerms(keyword, country string) []string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil
	}

	lang := LangForCountry(country)
	out := []string{keyword}
	entry := lookupGlossary(keyword)
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

	return orderTermsForMarket(clipTerms(uniqueFoldedStrings(out), maxLocalTerms), country)
}

func glossaryAliases(keyword string) []string {
	entry := lookupGlossary(keyword)
	if entry == nil {
		return nil
	}
	var out []string
	for _, terms := range entry {
		out = append(out, terms...)
	}

	return uniqueFoldedStrings(out)
}

// orderTermsForMarket puts English/local phrases first when the user picked
// an overseas market, so Facebook/LinkedIn dorks do not lead with Chinese.
func orderTermsForMarket(terms []string, country string) []string {
	code := LookupCountry(country).Code
	if code == "" || code == "CN" || len(terms) < 2 {
		return terms
	}
	var local, cjk []string
	for _, term := range terms {
		if hasCJK(term) {
			cjk = append(cjk, term)
		} else {
			local = append(local, term)
		}
	}
	if len(local) == 0 {
		return terms
	}
	return append(local, cjk...)
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
		return []string{"采购", "进口商", "求购", "经销商", "店铺", "贸易"}
	default:
		return []string{"importer", "buyer", "sourcing", "dealer", "store", "shop", "trading", "distributor"}
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
