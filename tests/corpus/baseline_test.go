package corpus_test

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	cg "github.com/compforge/codegraph"
)

func evaluatorHash(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(strings.HasSuffix(name, ".go") || name == "repos.json") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s\x00%x\n", name, sha256.Sum256(data))
	}

	for _, directory := range []string{"python", "typescript"} {
		err = filepath.WalkDir(directory, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") || d.Name() == "__pycache__" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(name, ".py") && !strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".mjs") {
				return nil
			}
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%s\x00%x\n", filepath.ToSlash(name), sha256.Sum256(data))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func sourceHash(t *testing.T) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir("../..", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("../..", name)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || rel == "tests") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") && rel != "go.mod" && rel != "go.sum" && rel != "VERSION" {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%x\n", filepath.ToSlash(rel), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Baselines are explicit, reviewed artifacts. Never refresh them during a test
// run: changing the oracle, profile or input changes what a score means.
func compareBaseline(base, current runReport) ([]string, error) {
	if base.Status != "measured" || current.Status != "measured" || base.Evaluation == nil || current.Evaluation == nil {
		return nil, fmt.Errorf("baseline comparison requires two measured runs")
	}
	if base.SchemaVersion != current.SchemaVersion {
		return nil, fmt.Errorf("baseline schema mismatch; explicitly re-evaluate artifacts with the same schema")
	}
	if !reflect.DeepEqual(base.Input, current.Input) {
		return nil, fmt.Errorf("baseline input identity mismatch")
	}
	if !reflect.DeepEqual(base.Evaluator, current.Evaluator) {
		return nil, fmt.Errorf("baseline evaluator identity mismatch")
	}
	var regressions []string
	for key, prior := range base.Evaluation.Measurements {
		now := current.Evaluation.Measurements[key]
		if now == nil || now.Expected != prior.Expected {
			return nil, fmt.Errorf("oracle denominator changed: %s", key)
		}
		if now.Found < prior.Found {
			regressions = append(regressions, key+": fewer facts recovered")
		}
		if now.Unexpected > prior.Unexpected {
			regressions = append(regressions, key+": more unexpected facts")
		}
	}
	for kind, prior := range base.Evaluation.Bindings {
		now := current.Evaluation.Bindings[kind]
		if now == nil || now.Expected != prior.Expected {
			return nil, fmt.Errorf("binding denominator changed: %s", kind)
		}
		if now.Hit < prior.Hit {
			regressions = append(regressions, string(kind)+": fewer correct targets")
		}
		if now.tier(cg.Exact).Other > prior.tier(cg.Exact).Other {
			regressions = append(regressions, string(kind)+": more incorrect exact targets")
		}
		if now.otherTargets() > prior.otherTargets() || now.MaxTargets > prior.MaxTargets {
			regressions = append(regressions, string(kind)+": target-set expansion needs review")
		}
		if now.SilentMissing > prior.SilentMissing {
			regressions = append(regressions, string(kind)+": more silent target gaps")
		}
	}
	sort.Strings(regressions)
	return regressions, nil
}

func TestBaselineRejectsDriftAndDetectsLoss(t *testing.T) {
	base := runReport{SchemaVersion: 3, Status: "measured", Evaluator: EvaluatorIdentity{SourceSHA256: "same"}, Evaluation: &evaluation{Measurements: map[string]*measurement{"declarations/all": {Expected: 10, Found: 9}}, Bindings: map[cg.RelationKind]*bindings{cg.Calls: {Expected: 3, Hit: 3}}}}
	current := base
	current.Evaluation = &evaluation{Measurements: map[string]*measurement{"declarations/all": {Expected: 10, Found: 8}}, Bindings: map[cg.RelationKind]*bindings{cg.Calls: {Expected: 3, Hit: 2}}}
	r, err := compareBaseline(base, current)
	if err != nil || len(r) != 2 {
		t.Fatal(r, err)
	}
	current.Evaluator.SourceSHA256 = "different"
	if _, err := compareBaseline(base, current); err == nil {
		t.Fatal("accepted a different oracle")
	}
}
