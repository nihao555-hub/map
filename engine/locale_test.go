package engine

import (
	"strings"
	"testing"
)

func TestLangForCountryMatchesMapPicker(t *testing.T) {
	if LangForCountry("TH") != "th" || LangForCountry("cn") != "zh" || LangForCountry("xx") != "" {
		t.Fatalf("th=%q cn=%q xx=%q", LangForCountry("TH"), LangForCountry("cn"), LangForCountry("xx"))
	}
}

func TestLocalSearchTermsThailandPowerTools(t *testing.T) {
	got := LocalSearchTerms("电动工具", "TH")
	joined := strings.Join(got, " | ")
	for _, want := range []string{"电动工具", "power tools", "เครื่องมือไฟฟ้า"} {
		if !containsString(got, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
}

func TestParseAITermList(t *testing.T) {
	got := parseAITermList("Here you go:\n[\"power tools\", \"เครื่องมือไฟฟ้า\", \"electric tools\"]\n")
	if !containsString(got, "power tools") || !containsString(got, "เครื่องมือไฟฟ้า") {
		t.Fatalf("%v", got)
	}
}
