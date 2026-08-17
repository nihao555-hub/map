package engine

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// CountryInfo is a target market shown in the intelligent-engine country picker.
type CountryInfo struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Query  string `json:"-"`
	DDGKL  string `json:"-"`
	BingCC string `json:"-"`
}

// SearchCountries is the market list for 外贸获客. Code "" means no country filter.
var SearchCountries = []CountryInfo{
	{Code: "", Label: "不限"},
	{Code: "TH", Label: "泰国", Query: "Thailand", DDGKL: "th-th", BingCC: "TH"},
	{Code: "VN", Label: "越南", Query: "Vietnam", DDGKL: "vn-vi", BingCC: "VN"},
	{Code: "MY", Label: "马来西亚", Query: "Malaysia", DDGKL: "my-en", BingCC: "MY"},
	{Code: "ID", Label: "印度尼西亚", Query: "Indonesia", DDGKL: "id-id", BingCC: "ID"},
	{Code: "SG", Label: "新加坡", Query: "Singapore", DDGKL: "sg-en", BingCC: "SG"},
	{Code: "PH", Label: "菲律宾", Query: "Philippines", DDGKL: "ph-en", BingCC: "PH"},
	{Code: "KH", Label: "柬埔寨", Query: "Cambodia", DDGKL: "kh-en", BingCC: "KH"},
	{Code: "LA", Label: "老挝", Query: "Laos", DDGKL: "la-en", BingCC: "LA"},
	{Code: "MM", Label: "缅甸", Query: "Myanmar", DDGKL: "mm-en", BingCC: "MM"},
	{Code: "BN", Label: "文莱", Query: "Brunei", DDGKL: "bn-en", BingCC: "BN"},
	{Code: "IN", Label: "印度", Query: "India", DDGKL: "in-en", BingCC: "IN"},
	{Code: "AE", Label: "阿联酋", Query: "UAE", DDGKL: "ae-en", BingCC: "AE"},
	{Code: "SA", Label: "沙特阿拉伯", Query: "Saudi Arabia", DDGKL: "sa-en", BingCC: "SA"},
	{Code: "US", Label: "美国", Query: "USA", DDGKL: "us-en", BingCC: "US"},
	{Code: "GB", Label: "英国", Query: "UK", DDGKL: "uk-en", BingCC: "GB"},
	{Code: "DE", Label: "德国", Query: "Germany", DDGKL: "de-de", BingCC: "DE"},
	{Code: "FR", Label: "法国", Query: "France", DDGKL: "fr-fr", BingCC: "FR"},
	{Code: "IT", Label: "意大利", Query: "Italy", DDGKL: "it-it", BingCC: "IT"},
	{Code: "ES", Label: "西班牙", Query: "Spain", DDGKL: "es-es", BingCC: "ES"},
	{Code: "NL", Label: "荷兰", Query: "Netherlands", DDGKL: "nl-nl", BingCC: "NL"},
	{Code: "BE", Label: "比利时", Query: "Belgium", DDGKL: "be-fr", BingCC: "BE"},
	{Code: "AT", Label: "奥地利", Query: "Austria", DDGKL: "at-de", BingCC: "AT"},
	{Code: "CH", Label: "瑞士", Query: "Switzerland", DDGKL: "ch-de", BingCC: "CH"},
	{Code: "SE", Label: "瑞典", Query: "Sweden", DDGKL: "se-sv", BingCC: "SE"},
	{Code: "DK", Label: "丹麦", Query: "Denmark", DDGKL: "dk-da", BingCC: "DK"},
	{Code: "CZ", Label: "捷克", Query: "Czechia", DDGKL: "cz-cs", BingCC: "CZ"},
	{Code: "PL", Label: "波兰", Query: "Poland", DDGKL: "pl-pl", BingCC: "PL"},
	{Code: "RU", Label: "俄罗斯", Query: "Russia", DDGKL: "ru-ru", BingCC: "RU"},
	{Code: "TR", Label: "土耳其", Query: "Turkey", DDGKL: "tr-tr", BingCC: "TR"},
	{Code: "JP", Label: "日本", Query: "Japan", DDGKL: "jp-jp", BingCC: "JP"},
	{Code: "KR", Label: "韩国", Query: "Korea", DDGKL: "kr-kr", BingCC: "KR"},
	{Code: "AU", Label: "澳大利亚", Query: "Australia", DDGKL: "au-en", BingCC: "AU"},
	{Code: "BR", Label: "巴西", Query: "Brazil", DDGKL: "br-pt", BingCC: "BR"},
	{Code: "MX", Label: "墨西哥", Query: "Mexico", DDGKL: "mx-es", BingCC: "MX"},
	{Code: "CA", Label: "加拿大", Query: "Canada", DDGKL: "ca-en", BingCC: "CA"},
	{Code: "ZA", Label: "南非", Query: "South Africa", DDGKL: "za-en", BingCC: "ZA"},
	{Code: "NG", Label: "尼日利亚", Query: "Nigeria", DDGKL: "ng-en", BingCC: "NG"},
	{Code: "EG", Label: "埃及", Query: "Egypt", DDGKL: "eg-en", BingCC: "EG"},
	{Code: "TW", Label: "台湾", Query: "Taiwan", DDGKL: "tw-zh", BingCC: "TW"},
	{Code: "HK", Label: "香港", Query: "Hong Kong", DDGKL: "hk-en", BingCC: "HK"},
	{Code: "CN", Label: "中国", Query: "China", DDGKL: "cn-zh", BingCC: "CN"},
}

type regionCtxKey struct{}

// LookupCountry resolves an ISO code from the picker. Unknown codes are "不限".
func LookupCountry(code string) CountryInfo {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return SearchCountries[0]
	}
	for _, c := range SearchCountries {
		if c.Code == code {
			return c
		}
	}

	return SearchCountries[0]
}

// CountryQueryToken is appended to index queries. Empty means no geo bias.
func CountryQueryToken(code string, cjk bool) string {
	c := LookupCountry(code)
	if c.Code == "" {
		return ""
	}
	if c.Code == "CN" {
		return ""
	}
	if cjk && c.Label != "" && c.Label != "不限" {
		return c.Label
	}

	return c.Query
}

// WithSearchCountry stores the selected market on the request context.
func WithSearchCountry(ctx context.Context, code string) context.Context {
	return context.WithValue(ctx, regionCtxKey{}, LookupCountry(code))
}

func searchCountry(ctx context.Context) CountryInfo {
	if ctx == nil {
		return CountryInfo{}
	}
	if v, ok := ctx.Value(regionCtxKey{}).(CountryInfo); ok {
		return v
	}

	return CountryInfo{}
}

// countryPlaceAliases maps cities / extra names onto ISO codes already in SearchCountries.
var countryPlaceAliases = map[string]string{
	"united states": "US", "usa": "US", "u.s.a": "US", "u.s.": "US",
	"new york": "US", "california": "US", "texas": "US", "los angeles": "US",
	"united kingdom": "GB", "britain": "GB", "england": "GB", "london": "GB",
	"deutschland": "DE", "berlin": "DE",
	"paris": "FR", "milan": "IT", "milano": "IT", "madrid": "ES",
	"amsterdam": "NL", "warsaw": "PL", "moscow": "RU", "istanbul": "TR",
	"dubai": "AE", "abu dhabi": "AE", "riyadh": "SA",
	"mumbai": "IN", "delhi": "IN", "bangalore": "IN",
	"jakarta": "ID", "surabaya": "ID", "bandung": "ID", "medan": "ID", "denpasar": "ID", "雅加达": "ID",
	"印尼": "ID", "indonesia": "ID", "indonesian": "ID",
	"hanoi": "VN", "ho chi minh": "VN", "saigon": "VN", "da nang": "VN", "河内": "VN", "胡志明": "VN", "岘港": "VN",
	"越南":      "VN",
	"bangkok": "TH", "chiang mai": "TH", "pattaya": "TH", "phuket": "TH", "曼谷": "TH", "清迈": "TH",
	"kuala lumpur": "MY", "selangor": "MY", "johor": "MY", "penang": "MY",
	"马来": "MY", "大马": "MY",
	"phnom penh": "KH", "siem reap": "KH", "金边": "KH", "暹粒": "KH", "cambodia": "KH",
	"vientiane": "LA", "万象": "LA", "laos": "LA",
	"yangon": "MM", "mandalay": "MM", "仰光": "MM", "myanmar": "MM", "burma": "MM",
	"bandar seri begawan": "BN", "brunei": "BN", "文莱": "BN",
	"吉隆坡": "MY", "雪兰莪": "MY", "柔佛": "MY", "malaysian": "MY",
	"sdn bhd": "MY", "sdn. bhd": "MY", "sibu": "MY", "sarawak": "MY",
	"sabah": "MY", "kuching": "MY",
	"manila": "PH", "osaka": "JP", "tokyo": "JP", "seoul": "KR",
	"sydney": "AU", "melbourne": "AU",
	"sao paulo": "BR", "são paulo": "BR", "mexico city": "MX",
	"toronto": "CA", "vancouver": "CA",
	"johannesburg": "ZA", "lagos": "NG", "cairo": "EG",
	"taiwan": "TW", "taipei": "TW", "taichung": "TW", "台湾": "TW", "台北": "TW", "台中": "TW",
	"hong kong": "HK", "香港": "HK",
	"china": "CN", "guangdong": "CN", "shenzhen": "CN", "guangzhou": "CN",
	"yiwu": "CN", "dongguan": "CN", "zhejiang": "CN",
	"广东": "CN", "深圳": "CN", "广州": "CN", "义乌": "CN", "东莞": "CN", "浙江": "CN", "江苏": "CN",
}

type countryToken struct {
	token string
	code  string
}

var (
	countryTokensOnce sync.Once
	countryTokens     []countryToken
)

func countryTokenList() []countryToken {
	countryTokensOnce.Do(func() {
		seen := map[string]string{}
		add := func(tok, code string) {
			tok = foldSearchText(tok)
			if tok == "" || code == "" {
				return
			}
			if prev, ok := seen[tok]; ok && prev != code {
				return
			}
			if _, ok := seen[tok]; ok {
				return
			}
			seen[tok] = code
			countryTokens = append(countryTokens, countryToken{token: tok, code: code})
		}
		for _, c := range SearchCountries {
			if c.Code == "" {
				continue
			}
			add(c.Label, c.Code)
			add(c.Query, c.Code)
		}
		for tok, code := range countryPlaceAliases {
			add(tok, code)
		}
		sort.Slice(countryTokens, func(i, j int) bool {
			if len(countryTokens[i].token) == len(countryTokens[j].token) {
				return countryTokens[i].token < countryTokens[j].token
			}

			return len(countryTokens[i].token) > len(countryTokens[j].token)
		})
	})

	return countryTokens
}

func matchCountryCode(blob string) string {
	blob = foldSearchText(blob)
	if blob == "" {
		return ""
	}
	for _, tok := range countryTokenList() {
		if countryTokenMatches(blob, tok.token) {
			return tok.code
		}
	}

	return ""
}

func countryTokenMatches(blob, tok string) bool {
	if tok == "" || blob == "" {
		return false
	}
	if hasCJK(tok) {
		return strings.Contains(blob, tok)
	}
	idx := 0
	for {
		i := strings.Index(blob[idx:], tok)
		if i < 0 {
			return false
		}
		i += idx
		beforeOK := i == 0 || !isCountryWordChar(rune(blob[i-1]))
		after := i + len(tok)
		afterOK := after == len(blob) || !isCountryWordChar(rune(blob[after]))
		if beforeOK && afterOK {
			return true
		}
		idx = i + 1
	}
}

func isCountryWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func inferHitCountry(hit Hit, selected string) (code, label string) {
	if cc := strings.ToUpper(strings.TrimSpace(hit.Country)); len(cc) == 2 {
		c := LookupCountry(cc)
		if c.Code != "" {
			return c.Code, c.Label
		}
		return cc, firstNonEmpty(hit.CountryLabel, cc)
	}
	blob := strings.Join([]string{hit.Name, hit.Handle, hit.Title, hit.Snippet, hit.HomepageURL}, " ")
	if found := matchCountryCode(blob); found != "" {
		c := LookupCountry(found)
		if c.Code != "" {
			return c.Code, c.Label
		}

		return found, found
	}

	switch hit.Platform {
	case PlatformDouyin, PlatformXiaohongshu, PlatformKuaishou, PlatformWeibo, PlatformBilibili:
		c := LookupCountry("CN")

		return c.Code, c.Label
	}

	return "", ""
}

// SplitKeywordCountry peels a leading/trailing market name out of the keyword
// ("印尼配电柜" → 配电柜 + ID). An explicit picker country wins.
func SplitKeywordCountry(keyword, selected string) (clean, country string) {
	keyword = NormalizeKeyword(keyword)
	country = LookupCountry(selected).Code
	if keyword == "" {
		return "", country
	}
	folded := foldSearchText(keyword)
	for _, tok := range countryTokenList() {
		if tok.token == "" || !countryTokenMatches(folded, tok.token) {
			continue
		}
		rest := stripCountryToken(keyword, tok.token)
		rest = NormalizeKeyword(rest)
		if rest == "" || foldSearchText(keyword) == tok.token {
			if country == "" {
				country = tok.code
			}
			return "", country
		}
		if country == "" {
			country = tok.code
		}
		return rest, country
	}
	return keyword, country
}

func stripCountryToken(keyword, token string) string {
	folded := foldSearchText(keyword)
	tok := foldSearchText(token)
	if tok == "" || !strings.Contains(folded, tok) {
		return keyword
	}
	// Rebuild by cutting the same rune window from the original string.
	src := []rune(keyword)
	foldRunes := []rune(foldSearchText(string(src)))
	tokRunes := []rune(tok)
	if len(foldRunes) != len(src) {
		return strings.ReplaceAll(folded, tok, " ")
	}
	for i := 0; i+len(tokRunes) <= len(foldRunes); i++ {
		if string(foldRunes[i:i+len(tokRunes)]) != tok {
			continue
		}
		if !hasCJK(tok) {
			if i > 0 && isCountryWordChar(foldRunes[i-1]) {
				continue
			}
			if i+len(tokRunes) < len(foldRunes) && isCountryWordChar(foldRunes[i+len(tokRunes)]) {
				continue
			}
		}
		out := append([]rune{}, src[:i]...)
		out = append(out, src[i+len(tokRunes):]...)
		return string(out)
	}
	return keyword
}

func countryBonus(hit Hit, selected string) int {
	selected = strings.ToUpper(strings.TrimSpace(selected))
	if selected == "" || hit.Country == "" {
		return 0
	}
	if hit.Country == selected {
		return 12
	}

	return -10
}
