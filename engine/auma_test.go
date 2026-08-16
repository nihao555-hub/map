package engine

import "testing"

func TestParseAUMAFairs(t *testing.T) {
	html := `<table><tbody>
	<tr class="trade-fair-result__row">
	  <td class="trade-fair-result__cell trade-fair-result__cell--strTermin">15.04.-21.04.2026</td>
	  <td class="trade-fair-result__cell trade-fair-result__cell--strTitel">
	    <a class="trade-fair-result__link" href="https://www.auma.de/en/find-your-fair/details/?tfd=milan_salone-del-mobile_1">Salone del Mobile.Milano</a>
	  </td>
	  <td class="trade-fair-result__cell trade-fair-result__cell--strStadt">Milan</td>
	  <td class="trade-fair-result__cell trade-fair-result__cell--strLand">Italy</td>
	</tr>
	</tbody></table>`
	hits := parseAUMAFairs([]byte(html))
	if len(hits) != 1 || hits[0].Name != "Salone del Mobile.Milano" {
		t.Fatalf("%+v", hits)
	}
	if hits[0].Extra["city"] != "Milan" || hits[0].Extra["start"] != "2026-04-15" {
		t.Fatalf("extra %+v", hits[0].Extra)
	}
	if hits[0].Country != "IT" {
		t.Fatalf("country %+v", hits[0])
	}
}

func TestBrandToConsignee(t *testing.T) {
	if got := brandToConsignee("Foot Locker"); got != "FOOT LOCKER INC" {
		t.Fatalf("got %q", got)
	}
	if got := brandToConsignee("NIKE INC"); got != "NIKE INC" {
		t.Fatalf("got %q", got)
	}
	if brandToConsignee("Shoes") != "" || brandToConsignee("Amazon.com") != "" {
		t.Fatal("noise brands")
	}
}

func TestCompanyCandidatesFromHit(t *testing.T) {
	cands := companyCandidatesFromHit(Hit{Name: "Shoes for Men, Women, & Kids - Foot Locker"})
	found := false
	for _, c := range cands {
		if c == "FOOT LOCKER INC" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%v", cands)
	}
}
