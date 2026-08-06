package web

import (
	"strings"
	"testing"
)

func TestStripMapsPlaceSuffix(t *testing.T) {
	if got := stripMapsPlaceSuffix("panel listrik in Jakarta, Indonesia"); got != "panel listrik" {
		t.Fatalf("got %q", got)
	}
	if got := stripMapsPlaceSuffix("panel listrik"); got != "panel listrik" {
		t.Fatalf("got %q", got)
	}
}

func TestPreferHighRecallKeywords(t *testing.T) {
	got := preferHighRecallKeywords([]string{"supplier switchgear"})
	if len(got) != 1 || got[0] != "panel listrik" {
		t.Fatalf("got %#v", got)
	}
	got = preferHighRecallKeywords([]string{"distributor panel listrik in Surabaya"})
	if len(got) < 1 || got[0] != "panel listrik" {
		t.Fatalf("got %#v want panel listrik first", got)
	}
}

func TestExpandMetroPlanTasksJakarta(t *testing.T) {
	plan := AgentPlan{
		Tasks: []AgentTask{{
			Name: "雅加达 · 配电", Location: "Jakarta, Indonesia",
			Keywords: []string{"switchgear"}, RadiusKm: 25, CountryCode: "id",
		}},
	}
	out := expandMetroPlanTasks(plan)
	if len(out.Tasks) < 5 {
		t.Fatalf("jakarta should expand to districts, got %d: %+v", len(out.Tasks), out.Tasks)
	}
	for _, tsk := range out.Tasks {
		if tsk.Keywords[0] != "panel listrik" {
			t.Fatalf("keyword=%q want panel listrik", tsk.Keywords[0])
		}
	}
	// District pin must not re-expand.
	once := expandMetroPlanTasks(AgentPlan{Tasks: []AgentTask{{
		Location: "Jakarta Selatan", Keywords: []string{"panel listrik"}, RadiusKm: 12,
	}}})
	if len(once.Tasks) != 1 {
		t.Fatalf("district re-expanded: %+v", once.Tasks)
	}
	if !strings.EqualFold(once.Tasks[0].Location, "Jakarta Selatan") {
		t.Fatalf("location=%q", once.Tasks[0].Location)
	}
}
