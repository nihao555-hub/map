package engine

import (
	"context"
	"strings"
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
	{Code: "US", Label: "美国", Query: "USA", DDGKL: "us-en", BingCC: "US"},
	{Code: "GB", Label: "英国", Query: "UK", DDGKL: "uk-en", BingCC: "GB"},
	{Code: "DE", Label: "德国", Query: "Germany", DDGKL: "de-de", BingCC: "DE"},
	{Code: "FR", Label: "法国", Query: "France", DDGKL: "fr-fr", BingCC: "FR"},
	{Code: "IT", Label: "意大利", Query: "Italy", DDGKL: "it-it", BingCC: "IT"},
	{Code: "ES", Label: "西班牙", Query: "Spain", DDGKL: "es-es", BingCC: "ES"},
	{Code: "NL", Label: "荷兰", Query: "Netherlands", DDGKL: "nl-nl", BingCC: "NL"},
	{Code: "PL", Label: "波兰", Query: "Poland", DDGKL: "pl-pl", BingCC: "PL"},
	{Code: "RU", Label: "俄罗斯", Query: "Russia", DDGKL: "ru-ru", BingCC: "RU"},
	{Code: "TR", Label: "土耳其", Query: "Turkey", DDGKL: "tr-tr", BingCC: "TR"},
	{Code: "AE", Label: "阿联酋", Query: "UAE", DDGKL: "ae-en", BingCC: "AE"},
	{Code: "SA", Label: "沙特阿拉伯", Query: "Saudi Arabia", DDGKL: "sa-en", BingCC: "SA"},
	{Code: "IN", Label: "印度", Query: "India", DDGKL: "in-en", BingCC: "IN"},
	{Code: "ID", Label: "印度尼西亚", Query: "Indonesia", DDGKL: "id-id", BingCC: "ID"},
	{Code: "VN", Label: "越南", Query: "Vietnam", DDGKL: "vn-vi", BingCC: "VN"},
	{Code: "TH", Label: "泰国", Query: "Thailand", DDGKL: "th-th", BingCC: "TH"},
	{Code: "MY", Label: "马来西亚", Query: "Malaysia", DDGKL: "my-en", BingCC: "MY"},
	{Code: "SG", Label: "新加坡", Query: "Singapore", DDGKL: "sg-en", BingCC: "SG"},
	{Code: "PH", Label: "菲律宾", Query: "Philippines", DDGKL: "ph-en", BingCC: "PH"},
	{Code: "JP", Label: "日本", Query: "Japan", DDGKL: "jp-jp", BingCC: "JP"},
	{Code: "KR", Label: "韩国", Query: "Korea", DDGKL: "kr-kr", BingCC: "KR"},
	{Code: "AU", Label: "澳大利亚", Query: "Australia", DDGKL: "au-en", BingCC: "AU"},
	{Code: "BR", Label: "巴西", Query: "Brazil", DDGKL: "br-pt", BingCC: "BR"},
	{Code: "MX", Label: "墨西哥", Query: "Mexico", DDGKL: "mx-es", BingCC: "MX"},
	{Code: "CA", Label: "加拿大", Query: "Canada", DDGKL: "ca-en", BingCC: "CA"},
	{Code: "ZA", Label: "南非", Query: "South Africa", DDGKL: "za-en", BingCC: "ZA"},
	{Code: "NG", Label: "尼日利亚", Query: "Nigeria", DDGKL: "ng-en", BingCC: "NG"},
	{Code: "EG", Label: "埃及", Query: "Egypt", DDGKL: "eg-en", BingCC: "EG"},
	{Code: "CN", Label: "中国", Query: "", DDGKL: "cn-zh", BingCC: "CN"},
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
