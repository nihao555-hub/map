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
