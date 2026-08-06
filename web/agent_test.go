package web

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestUnderstandIntentRulesJakartaCafe(t *testing.T) {
	intent := understandIntentRules("在雅加达找咖啡馆，半径15公里", "zh")
	if intent.CountryCode != "id" && intent.Location == "" {
		t.Fatalf("expected country or location, got %+v", intent)
	}
	if intent.RadiusKm != 15 {
		t.Fatalf("radius=%d want 15", intent.RadiusKm)
	}
	if len(intent.Keywords) == 0 {
		t.Fatalf("expected keywords, got %+v", intent)
	}
}

func TestUnderstandIntentBeijingHotpotSuggestion(t *testing.T) {
	intent := understandIntentRules("帮我找北京市朝阳区的火锅店", "zh")
	if intent.CountryCode != "cn" {
		t.Fatalf("country=%q want cn", intent.CountryCode)
	}
	if intent.Location == "" || (!strings.Contains(intent.Location, "朝阳") && !strings.Contains(intent.Location, "北京")) {
		t.Fatalf("location=%q want Beijing/Chaoyang", intent.Location)
	}
	found := false
	for _, kw := range intent.Keywords {
		if strings.Contains(kw, "火锅") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("keywords=%v want 火锅店", intent.Keywords)
	}
}

func TestUnderstandIntentInfersCountryFromCity(t *testing.T) {
	intent := understandIntentRules("在雅加达找咖啡馆，半径10公里", "zh")
	if intent.CountryCode != "id" {
		t.Fatalf("country=%q want id (from 雅加达)", intent.CountryCode)
	}
	if intent.Location == "" {
		t.Fatal("missing location")
	}
}

func TestUnderstandIntentCityWideRadius(t *testing.T) {
	intent := understandIntentRules("覆盖整个雅加达找咖啡馆", "zh")
	if intent.RadiusKm < 30 {
		t.Fatalf("city-wide radius=%d want >=30", intent.RadiusKm)
	}
	if intent.CountryCode != "id" {
		t.Fatalf("country=%s", intent.CountryCode)
	}
}

func TestUnderstandIntentJakartaDefaultsToMetroCoverage(t *testing.T) {
	// No radius → agent must prefer finishing the city, not a 3–10km sample.
	intent := understandIntentRules("在雅加达找咖啡馆", "zh")
	if intent.RadiusKm < 40 {
		t.Fatalf("metro default radius=%d want >=40", intent.RadiusKm)
	}
	if intent.Location == "" {
		t.Fatal("missing location")
	}
	plan := PlanTasks(intent)
	if len(plan.Tasks) < 5 {
		t.Fatalf("expected multi-district tasks, got %d: %+v", len(plan.Tasks), plan.Tasks)
	}
}

func TestUnderstandIntentHonorsExplicitSmallRadius(t *testing.T) {
	intent := understandIntentRules("在雅加达找咖啡馆，半径3公里", "zh")
	if intent.RadiusKm != 3 {
		t.Fatalf("radius=%d want 3 (explicit)", intent.RadiusKm)
	}
}

func TestPlanTasksSplitsKeywords(t *testing.T) {
	plan := PlanTasks(AgentIntent{
		RawGoal:     "test",
		CountryCode: "id",
		CountryName: "Indonesia",
		Location:    "Jakarta",
		Keywords:    []string{"cafe", "importer"},
		RadiusKm:    40,
		UILang:      "zh",
	})
	// Jakarta metro × 2 keywords → multi-district coverage tasks
	if len(plan.Tasks) < 4 {
		t.Fatalf("tasks=%d want >=4 (district × keyword split)", len(plan.Tasks))
	}
	kw := map[string]bool{}
	for _, task := range plan.Tasks {
		if task.Role != "scraper" {
			t.Fatalf("role=%s", task.Role)
		}
		if len(task.Keywords) != 1 {
			t.Fatalf("keywords=%v", task.Keywords)
		}
		kw[task.Keywords[0]] = true
	}
	if !kw["cafe"] || !kw["importer"] {
		t.Fatalf("missing keyword split: %v", kw)
	}
	found := false
	for _, r := range plan.Roles {
		if r == "DispatcherAgent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("roles missing DispatcherAgent: %v", plan.Roles)
	}
}

func TestPlanTasksBeijingChaoyangSplitsHubs(t *testing.T) {
	plan := PlanTasks(AgentIntent{
		RawGoal:     "帮我找北京市朝阳区的火锅店",
		CountryCode: "cn",
		CountryName: "China",
		Location:    "北京市朝阳区",
		Keywords:    []string{"火锅店"},
		RadiusKm:    25,
		UILang:      "zh",
	})
	if len(plan.Tasks) < 3 {
		t.Fatalf("tasks=%d want multi-hub split for 朝阳区, got %+v", len(plan.Tasks), plan.Tasks)
	}
}

func TestApplyFullVolumeDefaults(t *testing.T) {
	d := JobData{FastMode: true, MaxResults: 20, Depth: 5, MaxTime: time.Minute}
	ApplyFullVolumeDefaults(&d, 15000)
	if d.FastMode {
		t.Fatal("fast mode must be off")
	}
	if !d.GridMode {
		t.Fatal("grid must be on")
	}
	if d.MaxResults != 0 {
		t.Fatalf("maxResults=%d want 0", d.MaxResults)
	}
	if d.Radius != 15000 {
		t.Fatalf("radius=%d", d.Radius)
	}
	if d.Depth < 20 {
		t.Fatalf("depth=%d want >=20", d.Depth)
	}
	if len(d.Proxies) != 0 {
		t.Fatalf("proxies should be empty for server-side only")
	}
	if !d.Email {
		t.Fatal("email required")
	}
}

func TestUnderstandIntentEmpty(t *testing.T) {
	_, err := UnderstandIntent(context.Background(), "  ", "zh")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAdaptiveJobConcurrencyAtLeastOne(t *testing.T) {
	t.Setenv("GMS_WEB_JOB_CONCURRENCY", "4")
	t.Setenv("GMS_DEEP_WORKERS", "")
	n := AdaptiveJobConcurrency()
	if n < 1 {
		t.Fatalf("adaptive=%d", n)
	}
	per := AdaptivePerJobConcurrency(16, false)
	if per < 2 || per > 4 {
		t.Fatalf("per-job deep=%d want 2..4", per)
	}
}
