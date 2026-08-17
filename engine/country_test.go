package engine

import "testing"

func TestLookupCountry(t *testing.T) {
	if got := LookupCountry("my"); got.Code != "MY" || got.Label != "马来西亚" {
		t.Fatalf("%+v", got)
	}
	if got := LookupCountry(""); got.Code != "" || got.Label != "不限" {
		t.Fatalf("%+v", got)
	}
	if got := LookupCountry("ZZ"); got.Code != "" {
		t.Fatalf("unknown should be 不限 %+v", got)
	}
	if got := LookupCountry("CH"); got.Code != "CH" || got.Label != "瑞士" {
		t.Fatalf("switzerland %+v", got)
	}
	if got := LookupCountry("KH"); got.Code != "KH" || got.Label != "柬埔寨" {
		t.Fatalf("cambodia %+v", got)
	}
}

func TestInferHitCountryKeepsDirectoryCountry(t *testing.T) {
	code, label := inferHitCountry(Hit{Name: "Indiana lighting shop", Country: "DE"}, "")
	if code != "DE" || label != "德国" {
		t.Fatalf("osm country overwritten: %s %s", code, label)
	}
}

func TestCountryQueryToken(t *testing.T) {
	if tok := CountryQueryToken("MY", true); tok != "马来西亚" {
		t.Fatalf("%q", tok)
	}
	if tok := CountryQueryToken("MY", false); tok != "Malaysia" {
		t.Fatalf("%q", tok)
	}
	if tok := CountryQueryToken("CN", true); tok != "" {
		t.Fatalf("cn=%q", tok)
	}
	if tok := CountryQueryToken("", true); tok != "" {
		t.Fatalf("empty=%q", tok)
	}
}

func TestMatchCountryCode(t *testing.T) {
	if got := matchCountryCode("Malaysia LED Importer in Kuala Lumpur"); got != "MY" {
		t.Fatalf("my=%q", got)
	}
	if got := matchCountryCode("factory in Indiana USA lighting"); got != "US" {
		t.Fatalf("usa should win over indiana, got %q", got)
	}
	if got := matchCountryCode("Indiana lighting shop"); got != "" {
		t.Fatalf("indiana must not map to India, got %q", got)
	}
	if got := matchCountryCode("Hua Yong Trading Sdn. Bhd. | Sibu"); got != "MY" {
		t.Fatalf("sdn bhd=%q", got)
	}
}

func TestSearchCountriesPutsThailandAfterUnlimited(t *testing.T) {
	if len(SearchCountries) < 3 {
		t.Fatalf("%+v", SearchCountries)
	}
	if SearchCountries[0].Label != "不限" || SearchCountries[1].Code != "TH" || SearchCountries[1].Label != "泰国" {
		t.Fatalf("thailand should be first market, got %+v", SearchCountries[:3])
	}
}

func TestInferCountryFromTextChinaNotIndia(t *testing.T) {
	code, label := inferCountryFromText("China", "")
	if code != "CN" || label != "中国" {
		t.Fatalf("China -> %s %s", code, label)
	}
	code, _ = inferCountryFromText("India", "")
	if code != "IN" {
		t.Fatalf("India -> %s", code)
	}
}

func TestInferHitCountry(t *testing.T) {
	code, label := inferHitCountry(Hit{
		Platform: PlatformFacebook,
		Name:     "LED Importer",
		Snippet:  "Based in Kuala Lumpur",
	}, "")
	if code != "MY" || label != "马来西亚" {
		t.Fatalf("%s %s", code, label)
	}

	code, label = inferHitCountry(Hit{
		Platform:    PlatformDouyin,
		Name:        "灯具店",
		HomepageURL: "https://www.douyin.com/user/MS4wLjABAAAAFactory",
	}, "")
	if code != "CN" || label != "中国" {
		t.Fatalf("douyin default %s %s", code, label)
	}

	code, label = inferHitCountry(Hit{
		Platform: PlatformFacebook,
		Name:     "LED Buyer",
	}, "MY")
	if code != "" || label != "" {
		t.Fatalf("must not stamp selected country on unlabeled hit: %s %s", code, label)
	}
}

func TestSplitKeywordCountry(t *testing.T) {
	kw, cc := SplitKeywordCountry("印尼配电柜", "")
	if kw != "配电柜" || cc != "ID" {
		t.Fatalf("印尼配电柜 -> %q %q", kw, cc)
	}
	kw, cc = SplitKeywordCountry("配电柜", "ID")
	if kw != "配电柜" || cc != "ID" {
		t.Fatalf("picker wins -> %q %q", kw, cc)
	}
	kw, cc = SplitKeywordCountry("Indonesia switchgear", "")
	if kw != "switchgear" || cc != "ID" {
		t.Fatalf("Indonesia switchgear -> %q %q", kw, cc)
	}
	kw, cc = SplitKeywordCountry("印尼", "")
	if kw != "" || cc != "ID" {
		t.Fatalf("country-only -> %q %q", kw, cc)
	}
	kw, cc = SplitKeywordCountry("LED灯", "")
	if kw != "LED灯" || cc != "" {
		t.Fatalf("no country -> %q %q", kw, cc)
	}
}
