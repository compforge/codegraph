package corpus_test

import (
	"encoding/json"
	cg "github.com/compforge/codegraph"
	"strings"
	"testing"
	"time"
)

func TestScorecardWeightsCountsAndKeepsFailures(t *testing.T) {
	reports := []runReport{
		{Status: "measured", Input: InputIdentity{Repository: repository{Name: "small"}}, Evaluation: &evaluation{Verdict: "measured", Measurements: map[string]*measurement{"declarations/all": {Expected: 1, Found: 1}}, Bindings: map[cg.RelationKind]*bindings{cg.Calls: {Expected: 1, Hit: 1, MaxTargets: 3, ByConfidence: map[cg.Confidence]*tierBindings{cg.Exact: {Hit: 1}}}}}},
		{Status: "measured", Input: InputIdentity{Repository: repository{Name: "large"}}, Evaluation: &evaluation{Verdict: "failed", Measurements: map[string]*measurement{"declarations/all": {Expected: 9, Found: 3, Unexpected: 1}}, Bindings: map[cg.RelationKind]*bindings{cg.Calls: {Expected: 9, Hit: 3, MaxTargets: 2, ByConfidence: map[cg.Confidence]*tierBindings{cg.Exact: {Hit: 3, Other: 1, Unassessed: 2}}}}, Unassessed: map[string]int{"calls/unknown": 7}}},
		{Status: "error", Error: "oracle unavailable", Input: InputIdentity{Repository: repository{Name: "broken", Language: "python"}}},
	}
	report := summarizeLanguages(reports, time.Time{}, true)
	goScore := report.Languages[0]
	m := goScore.Facts["declarations/all"]
	if *m.Precision != 0.8 || *m.Recall != 0.4 || goScore.Measured != 2 || goScore.Failed != 1 || report.Status != "error" {
		t.Fatalf("incorrect micro scores: %+v", report)
	}
	if goScore.Bindings[cg.Calls].MaxTargets != 3 || goScore.Bindings[cg.Calls].tier(cg.Exact).Other != 1 || goScore.Unassessed["calls/unknown"] != 7 {
		t.Fatal(goScore)
	}
	if report.Languages[1].Selected != 1 || report.Languages[1].Measured != 0 || report.Languages[2].Selected != 0 {
		t.Fatal(report.Languages)
	}
	text := renderScorecard(report)
	for _, want := range []string{"40.00% (4/10)", "80.00% (4/5)", "N/A (0/0)", "oracle unavailable", "Code organization"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	running := summarizeLanguages(reports, time.Time{}, false)
	if running.Status != "running" || running.FinishedAt != nil {
		t.Fatal(running)
	}
	failed := summarizeLanguages(reports[:2], time.Time{}, true)
	if failed.Status != "failed" {
		t.Fatal(failed.Status)
	}
}

func TestScorecardEmptyDenominatorsAreNull(t *testing.T) {
	r := runReport{Status: "measured", Evaluation: &evaluation{Measurements: map[string]*measurement{"declarations/all": {}, "declarations/contract/Function": {Expected: 2, Found: 1}}}}
	s := summarizeLanguages([]runReport{r}, time.Time{}, true)
	empty := s.Languages[0].Facts["declarations/all"]
	data, err := json.Marshal(empty)
	if err != nil || !strings.Contains(string(data), `"precision":null`) || !strings.Contains(string(data), `"recall":null`) {
		t.Fatalf("%s %v", data, err)
	}
	if s.Languages[0].Facts["declarations/contract/Function"].Precision != nil {
		t.Fatal("invented contract precision denominator")
	}
}
