package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestMaxRadiusKm(t *testing.T) {
	if MaxRadiusKm() != 50 {
		t.Fatalf("MaxRadiusKm=%d want 50", MaxRadiusKm())
	}
	if MaxRadiusMeters() != 50000 {
		t.Fatalf("MaxRadiusMeters=%d want 50000", MaxRadiusMeters())
	}
}

func TestParseTargetRadiusMeters(t *testing.T) {
	mk := func(values url.Values) *http.Request {
		req := &http.Request{Form: values, PostForm: values}
		return req
	}

	meters, err := parseTargetRadiusMeters(mk(url.Values{"radius_km": {"12"}}))
	if err != nil || meters != 12000 {
		t.Fatalf("radius_km=12 -> %d, %v", meters, err)
	}

	_, err = parseTargetRadiusMeters(mk(url.Values{"radius_km": {"51"}}))
	if err == nil || !strings.Contains(err.Error(), "≤ 50") {
		t.Fatalf("expected max error, got %v", err)
	}

	meters, err = parseTargetRadiusMeters(mk(url.Values{"radius": {"8000"}}))
	if err != nil || meters != 8000 {
		t.Fatalf("radius meters -> %d, %v", meters, err)
	}

	meters, err = parseTargetRadiusMeters(mk(url.Values{}))
	if err != nil || meters != 10000 {
		t.Fatalf("default -> %d, %v", meters, err)
	}
}

func TestFilterDecisionMakersDropsHallucinations(t *testing.T) {
	evidence := "contact us at sales@acme.id ceo alice tan leads the team"
	known := []string{"sales@acme.id"}
	in := []DecisionMaker{
		{Name: "Alice Tan", Title: "CEO", Email: "sales@acme.id"},
		{Name: "Fake Person", Title: "CTO", Email: "fake@nowhere.test"},
		{Name: "", Title: "Sales", Email: "sales@acme.id"},
	}
	out := filterDecisionMakers(in, evidence, known)
	if len(out) != 2 {
		t.Fatalf("want 2 verified makers, got %d: %+v", len(out), out)
	}
}
