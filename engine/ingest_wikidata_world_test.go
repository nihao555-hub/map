package engine

import "testing"

func TestParseWikidataWorldCompanies(t *testing.T) {
	raw := []byte(`{"results":{"bindings":[
	  {"item":{"value":"http://www.wikidata.org/entity/Q123"},
	   "itemLabel":{"value":"Signify N.V."},
	   "cc":{"value":"NL"},
	   "website":{"value":"https://www.signify.com"},
	   "lei":{"value":"549300YHEI7VVKKCYO67"},
	   "facebook":{"value":"Signify"},
	   "linkedin":{"value":"signify"}},
	  {"item":{"value":"http://www.wikidata.org/entity/Q5"},
	   "itemLabel":{"value":"human person page"},
	   "website":{"value":"https://example.org"}}
	]}}`)
	rows, leiRows := parseWikidataWorldCompanies(raw)
	if len(rows) != 1 || rows[0].Name != "Signify N.V." || rows[0].Country != "NL" {
		t.Fatalf("rows=%+v", rows)
	}
	if rows[0].Homepage != "https://www.signify.com" {
		t.Fatalf("home=%s", rows[0].Homepage)
	}
	sawFB, sawLI := false, false
	for _, p := range rows[0].Profiles {
		if p.Platform == PlatformFacebook {
			sawFB = true
		}
		if p.Platform == PlatformLinkedIn {
			sawLI = true
		}
	}
	if !sawFB || !sawLI {
		t.Fatalf("profiles=%+v", rows[0].Profiles)
	}
	if len(leiRows) != 1 || leiRows[0].ExtID != "gleif:549300YHEI7VVKKCYO67" {
		t.Fatalf("lei=%+v", leiRows)
	}
}
