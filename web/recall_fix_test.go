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
	if len(out.Tasks) < 3 || len(out.Tasks) > 4 {
		t.Fatalf("jakarta should expand to ~3 districts, got %d: %+v", len(out.Tasks), out.Tasks)
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

func TestTightenPlanCapsJakartaDistrictFanout(t *testing.T) {
	plan := AgentPlan{Tasks: []AgentTask{
		{Location: "Jakarta Pusat", Keywords: []string{"panel listrik"}, RadiusKm: 12},
		{Location: "Jakarta Barat", Keywords: []string{"panel listrik"}, RadiusKm: 12},
		{Location: "Jakarta Utara", Keywords: []string{"panel listrik"}, RadiusKm: 12},
		{Location: "Jakarta Timur", Keywords: []string{"panel listrik"}, RadiusKm: 12},
		{Location: "Jakarta Selatan", Keywords: []string{"panel listrik"}, RadiusKm: 12},
	}}
	out := tightenPlan(plan)
	if len(out.Tasks) > 3 {
		t.Fatalf("want <=3 tasks, got %d %+v", len(out.Tasks), out.Tasks)
	}
}

func TestNormalizeIntentHonorsExplicitSmallRadius(t *testing.T) {
	in := AgentIntent{Location: "Bandung", RadiusKm: 25, CountryCode: "id"}
	out := normalizeIntent(in, "在 Bandung 找咖啡馆，半径2公里", "zh")
	if out.RadiusKm != 2 {
		t.Fatalf("RadiusKm=%d want 2 (explicit user radius must beat AI default)", out.RadiusKm)
	}
}

func TestHonorExplicitCoverageLocksMaxRadiusAndSingleJob(t *testing.T) {
	plan := AgentPlan{
		Intent: AgentIntent{
			RawGoal:  "在 Surabaya 全市全量找咖啡馆，半径50公里，只创建一个任务不要拆分",
			Location: "Surabaya",
			RadiusKm: 50,
		},
		Tasks: []AgentTask{
			{Location: "Surabaya Pusat", Keywords: []string{"cafe"}, RadiusKm: 12},
			{Location: "Surabaya Timur", Keywords: []string{"cafe"}, RadiusKm: 12},
		},
	}
	out := honorExplicitCoverage(plan)
	if len(out.Tasks) != 1 {
		t.Fatalf("want 1 task, got %d %+v", len(out.Tasks), out.Tasks)
	}
	if out.Tasks[0].RadiusKm != 50 {
		t.Fatalf("RadiusKm=%d want 50", out.Tasks[0].RadiusKm)
	}
	if out.Tasks[0].Location != "Surabaya" {
		t.Fatalf("Location=%q want Surabaya", out.Tasks[0].Location)
	}
}

func TestTightenPlanCollapsesSameCityKeywordFanout(t *testing.T) {
	plan := AgentPlan{
		Intent: AgentIntent{Location: "Bandung", RadiusKm: 25}, // intent wrongly wide
		Tasks: []AgentTask{
			{Location: "Bandung", Keywords: []string{"cafe"}, RadiusKm: 2},
			{Location: "Bandung", Keywords: []string{"kedai kopi"}, RadiusKm: 2},
			{Location: "Bandung", Keywords: []string{"coffee shop"}, RadiusKm: 2},
		},
	}
	out := tightenPlan(plan)
	if len(out.Tasks) != 1 {
		t.Fatalf("want 1 task for same-city ≤5km fanout, got %d %+v", len(out.Tasks), out.Tasks)
	}
}

// PlanTasksAI used to restore an untightened heuristic base when AI collapsed to
// one task. That undid small-radius keyword collapse and filled both admit slots.
func TestPreferAIDoesNotUndoSmallRadiusTighten(t *testing.T) {
	base := AgentPlan{
		Intent: AgentIntent{Location: "Menteng, Jakarta", RadiusKm: 3},
		Tasks: []AgentTask{
			{Location: "Menteng, Jakarta", Keywords: []string{"kedai kopi"}, RadiusKm: 3},
			{Location: "Menteng, Jakarta", Keywords: []string{"cafe"}, RadiusKm: 3},
			{Location: "Menteng, Jakarta", Keywords: []string{"coffee shop"}, RadiusKm: 3},
		},
	}
	ai := AgentPlan{
		Intent: base.Intent,
		Tasks: []AgentTask{
			{Location: "Menteng, Jakarta", Keywords: []string{"kedai kopi"}, RadiusKm: 3},
		},
	}
	out := tightenPlan(ai)
	base = tightenPlan(base)
	if len(out.Tasks) < len(base.Tasks) && len(base.Tasks) >= 3 && len(out.Tasks) == 1 {
		t.Fatalf("tightened Menteng 3km base should be 1 task, got base=%d ai=%d", len(base.Tasks), len(out.Tasks))
	}
	if len(base.Tasks) != 1 || len(out.Tasks) != 1 {
		t.Fatalf("want both tightened to 1, base=%d ai=%d", len(base.Tasks), len(out.Tasks))
	}
}

func TestTightenPlanKeepsSmallRadiusAsOneCircle(t *testing.T) {
	plan := AgentPlan{
		Intent: AgentIntent{Location: "雅加达市中心", RadiusKm: 1},
		Tasks: []AgentTask{
			{Location: "Menteng, Jakarta", Keywords: []string{"kedai kopi"}, RadiusKm: 1},
			{Location: "Tanah Abang, Jakarta", Keywords: []string{"kedai kopi"}, RadiusKm: 1},
			{Location: "Gambir, Jakarta", Keywords: []string{"kedai kopi"}, RadiusKm: 1},
		},
	}

	out := tightenPlan(plan)
	if len(out.Tasks) != 1 {
		t.Fatalf("small radius should stay one search circle, got %+v", out.Tasks)
	}
	if out.Tasks[0].Location != "雅加达市中心" || out.Tasks[0].RadiusKm != 1 {
		t.Fatalf("intent center/radius not preserved: %+v", out.Tasks[0])
	}
}
