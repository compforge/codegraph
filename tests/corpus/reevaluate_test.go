package corpus_test

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	cg "github.com/compforge/codegraph"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

var reevaluateDir = flag.String("reevaluate-dir", "", "explicitly score retained graph evidence with the current oracle, into a separate report directory")

// Artifact conversion is confined to the evaluator. It never changes a graph
// schema at runtime, modifies the original report, or fabricates a new subject.
func readObservation(dir string, input InputIdentity) (observed, SubjectIdentity, *artifactOrigin, error) {
	var graph observed
	var subject SubjectIdentity
	reportData, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		return graph, subject, nil, err
	}
	graphData, err := os.ReadFile(filepath.Join(dir, "graph.json"))
	if err != nil {
		return graph, subject, nil, err
	}
	var report runReport
	if err = json.Unmarshal(reportData, &report); err != nil {
		return graph, subject, nil, err
	}
	switch report.SchemaVersion {
	case 1:
		var legacy struct {
			Repository                                                               repository
			Profile                                                                  string
			Inputs                                                                   []inputFile
			CodeGraphRevision, CodeGraphDiffSHA256, CodeGraphSourceSHA256, BuildInfo string
		}
		if err = json.Unmarshal(reportData, &legacy); err != nil {
			return graph, subject, nil, err
		}
		report.Input = InputIdentity{legacy.Repository, legacy.Profile, legacy.Inputs}
		report.Subject = SubjectIdentity{legacy.CodeGraphRevision, legacy.CodeGraphDiffSHA256, legacy.CodeGraphSourceSHA256, legacy.BuildInfo}
	case 2:
	default:
		return graph, subject, nil, fmt.Errorf("unsupported original report schema %d", report.SchemaVersion)
	}
	if report.Status != "measured" || !reflect.DeepEqual(report.Input, input) {
		return graph, subject, nil, fmt.Errorf("re-evaluation requires measured evidence with identical input identity")
	}
	if err = json.Unmarshal(graphData, &graph); err != nil {
		return graph, subject, nil, err
	}
	if report.SchemaVersion == 1 {
		var legacy struct{ Relations []struct{ Basis string } }
		if err = json.Unmarshal(graphData, &legacy); err != nil {
			return graph, subject, nil, err
		}
		for i := range graph.Relations {
			r := &graph.Relations[i]
			if legacy.Relations[i].Basis == "" {
				return graph, subject, nil, fmt.Errorf("original relation lacks evidence")
			}
			r.Evidence = []cg.Evidence{{Basis: legacy.Relations[i].Basis, Confidence: r.Confidence}}
		}
	}
	origin := &artifactOrigin{report.SchemaVersion, fmt.Sprintf("%x", sha256.Sum256(reportData)), fmt.Sprintf("%x", sha256.Sum256(graphData))}
	return graph, report.Subject, origin, nil
}
func TestReevaluationPreservesSubjectAndSource(t *testing.T) {
	dir := t.TempDir()
	input := InputIdentity{Profile: "fixed"}
	raw := []byte(`{"schemaVersion":1,"status":"measured","profile":"fixed","codegraphRevision":"old-parser","codegraphSourceSHA256":"original"}`)
	graph := []byte(`{"relations":[{"id":"occurrence","confidence":"candidate","basis":"syntax"}]}`)
	for name, data := range map[string][]byte{"report.json": raw, "graph.json": graph} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, s, origin, err := readObservation(dir, input)
	if err != nil || s.Revision != "old-parser" || s.SourceSHA256 != "original" || origin == nil || a.Relations[0].Evidence[0].Basis != "syntax" {
		t.Fatal(a, s, origin, err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil || string(after) != string(raw) {
		t.Fatal("original overwritten", err)
	}
	input.Profile = "changed"
	if _, _, _, err := readObservation(dir, input); err == nil {
		t.Fatal("accepted different input")
	}
}
