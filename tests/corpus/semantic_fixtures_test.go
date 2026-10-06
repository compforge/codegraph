package corpus_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	cg "github.com/compforge/codegraph"
)

// +case=`Namespace ownership and binding are verified together against independent source answers; submission schedules preserve the entire published graph`
func TestSemanticFixtures(t *testing.T) {
	results := []fixtureResult{{Language: "go", Status: "not_run"}, {Language: "python", Status: "not_run"}, {Language: "typescript", Status: "not_run"}, {Language: "javascript", Status: "not_run"}}
	writeFixtureSummary(t, results, "running")
	t.Cleanup(func() {
		status := "measured"
		if t.Failed() {
			status = "failed"
		}
		writeFixtureSummary(t, results, status)
	})
	for i := range results {
		language := results[i].Language
		t.Run(language, func(t *testing.T) {
			result := &results[i]
			result.Status = "error"
			root := copySemanticFixture(t, language)
			ctx := context.Background()
			var o *oracle
			var docs []cg.Document
			var err error
			opts := cg.Options{}
			switch language {
			case "go":
				o, docs, _, err = loadGoModulesOracle(ctx, root, ".", "nested", "peer")
				opts.ResolutionContext.GoModules = map[string]string{".": "example.org/root", "nested": "example.org/root/child", "peer": "example.org/peer"}
			case "python":
				o, docs, _, _, err = loadPythonOracle(ctx, root, ".", filepath.Join(root, "bindings.json"))
			default:
				o, docs, _, _, err = loadTypeScriptOracle(ctx, root, filepath.Join(root, "profile.json"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(o.Diagnostics) != 0 {
				t.Fatalf("fixture compiler diagnostics: %+v", o.Diagnostics)
			}
			b, err := cg.NewBuilder("semantic-fixture", opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := b.AddDocuments(ctx, docs...); err != nil {
				t.Fatal(err)
			}
			report, err := b.Wait(ctx)
			if err != nil {
				t.Fatal(err)
			}
			a := observed{Nodes: b.Result().Nodes(), Relations: b.Result().Relations(), Report: report}
			for _, doc := range docs {
				facts, err := b.Extract(ctx, doc)
				if err != nil {
					t.Fatal(err)
				}
				a.Facts = append(a.Facts, facts)
			}
			e := evaluate(o, a)
			result.Documents = len(docs)
			result.Evaluation = &e
			result.Status = "measured"
			result.Contracts = "running"
			t.Cleanup(func() {
				result.Contracts = "passed"
				if t.Failed() {
					result.Contracts = "failed"
				}
				if *corpusNames != "" {
					dir := filepath.Join(*reportDir, "fixtures", language)
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					writeJSON(t, filepath.Join(dir, "oracle.json"), o)
					writeJSON(t, filepath.Join(dir, "graph.json"), a)
					writeJSON(t, filepath.Join(dir, "result.json"), result)
				}
			})
			// Every missing static call must match an explicitly reviewed source gap.
			// Gaps stay in the denominator and never permit alternate targets.
			calls := e.Bindings[cg.Calls]
			if calls.Expected != map[string]int{"go": 5, "python": 3, "typescript": 4, "javascript": 4}[language] || calls.otherTargets() != 0 || e.Verdict == "failed" {
				t.Errorf("binding: %+v; findings=%+v", calls, e.Findings)
			}
			for name, m := range e.Measurements {
				if strings.HasPrefix(name, "organizations/") || strings.HasPrefix(name, "organization_structure/") || strings.HasPrefix(name, "declarations/contract/") {
					if m.Expected != m.Found || m.Unexpected != 0 {
						t.Errorf("%s: %+v", name, m)
					}
				}
			}
			assertKnownCallGaps(t, root, o, e)
			assertWrongPackageRejected(t, o, a)
			assertSubmissionSchedules(t, docs, opts, a)
			t.Logf("%s: %s; 6 submission schedules; reviewed fixtures, separate from repository scorecard", language, summary(e))
		})
	}
}

func copySemanticFixture(t *testing.T, language string) string {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join("testdata", "semantics", language)
	err := filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		dest := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func assertSubmissionSchedules(t *testing.T, docs []cg.Document, opts cg.Options, want observed) {
	t.Helper()
	for _, concurrency := range []int{1, 4} {
		for _, schedule := range []string{"batch-reversed", "one-by-one", "caller-first"} {
			t.Run(fmt.Sprintf("%s/workers-%d", schedule, concurrency), func(t *testing.T) {
				ordered := slices.Clone(docs)
				switch schedule {
				case "batch-reversed":
					slices.Reverse(ordered)
				case "caller-first":
					// The fixture's main file refers to definitions supplied only later.
					slices.SortStableFunc(ordered, func(a, b cg.Document) int {
						main := func(d cg.Document) int {
							if strings.Contains(d.Path, "main.") {
								return 0
							}
							return 1
						}
						return main(a) - main(b)
					})
				}
				opts.BuildConcurrency = concurrency
				b, err := cg.NewBuilder("semantic-fixture", opts)
				if err != nil {
					t.Fatal(err)
				}
				type publication struct {
					graph     *cg.Graph
					nodes     []cg.Node
					relations []cg.Relation
					report    cg.BuildReport
				}
				var history []publication
				for i := 0; i < len(ordered); {
					end := i + 1
					if schedule == "batch-reversed" {
						end = len(ordered)
					}
					if err := b.AddDocuments(context.Background(), ordered[i:end]...); err != nil {
						t.Fatal(err)
					}
					if _, err := b.Wait(context.Background()); err != nil {
						t.Fatal(err)
					}
					g := b.Result()
					history = append(history, publication{g, g.Nodes(), g.Relations(), g.Report()})
					i = end
				}
				// A repeated input cannot duplicate an occurrence or upgrade its evidence.
				if err := b.AddDocuments(context.Background(), ordered...); err != nil {
					t.Fatal(err)
				}
				if _, err := b.Wait(context.Background()); err != nil {
					t.Fatal(err)
				}
				for _, old := range history {
					if !reflect.DeepEqual(old.nodes, old.graph.Nodes()) || !reflect.DeepEqual(old.relations, old.graph.Relations()) || !reflect.DeepEqual(old.report, old.graph.Report()) {
						t.Fatal("supplement mutated a prior publication")
					}
				}
				got := b.Result()
				if !reflect.DeepEqual(want.Nodes, got.Nodes()) {
					t.Error("submission schedule changed nodes")
				}
				if !reflect.DeepEqual(want.Relations, got.Relations()) {
					t.Error("submission schedule changed relations or evidence")
				}
				if !reflect.DeepEqual(want.Report, got.Report()) {
					t.Errorf("submission schedule changed diagnostics/report: got %+v; want %+v", got.Report(), want.Report)
				}
			})
		}
	}
}

// A same-named declaration in another namespace must not pass the target scorer,
// even when every organization edge is otherwise correct.
func assertWrongPackageRejected(t *testing.T, o *oracle, a observed) {
	t.Helper()
	for i, r := range a.Relations {
		if r.Kind != cg.Calls {
			continue
		}
		var target cg.Node
		for _, n := range a.Nodes {
			if n.ID == r.Target {
				target = n
				break
			}
		}
		if target.Location == nil {
			continue
		}
		for _, wrong := range a.Nodes {
			if wrong.Name != target.Name || wrong.Kind != target.Kind || wrong.Location == nil || wrong.Location.Path == target.Location.Path {
				continue
			}
			original := evaluate(o, a)
			mutated := a
			mutated.Relations = slices.Clone(a.Relations)
			mutated.Relations[i].Target = wrong.ID
			mutated.Relations[i].Confidence = cg.Exact
			bad := evaluate(o, mutated)
			if bad.Verdict != "failed" || bad.Bindings[cg.Calls].otherTargets() <= original.Bindings[cg.Calls].otherTargets() {
				t.Fatal("same-name wrong namespace escaped scoring")
			}
			return
		}
	}
	t.Fatal("fixture did not exercise a same-name cross-namespace call")
}

// Known limitations are matched by source occurrence, never by an allowed count.
// An unrelated regression cannot replace a documented gap and keep this green.
func assertKnownCallGaps(t *testing.T, root string, o *oracle, e evaluation) {
	t.Helper()
	var gaps []struct{ Path, Expression, Context, Reason string }
	data, err := os.ReadFile(filepath.Join(root, "known-call-gaps.json"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err == nil {
		if err := json.Unmarshal(data, &gaps); err != nil {
			t.Fatal(err)
		}
	}
	expected := map[string]string{}
	for _, gap := range gaps {
		data, err := os.ReadFile(filepath.Join(root, gap.Path))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		offset := 0
		if gap.Context != "" {
			offset = strings.Index(text, gap.Context)
			if offset < 0 {
				t.Fatal("gap context absent", gap)
			}
			text = gap.Context
		}
		if strings.Count(text, gap.Expression) != 1 || gap.Reason == "" {
			t.Fatal("gap must identify one source expression and its reason", gap)
		}
		start := offset + strings.Index(text, gap.Expression)
		at := site{gap.Path, start, start + len(gap.Expression)}
		call, ok := o.Calls[at.key()]
		if !ok || call.Class != "internal" {
			t.Fatal("known gap must remain an assessed compiler/reviewed target", at, call)
		}
		expected[at.key()] = gap.Reason
	}
	for _, f := range e.Findings {
		if !strings.HasPrefix(f.Category, "missing_target/calls/") {
			continue
		}
		reason, ok := expected[f.Site.key()]
		if !ok || f.Category != "missing_target/calls/localized" {
			t.Errorf("unexpected or silent call gap: %+v", f)
			continue
		}
		t.Logf("known call gap: %s: %s", f.Site.key(), reason)
		delete(expected, f.Site.key())
	}
	for key := range expected {
		t.Errorf("resolved known gap %s: review and remove its obsolete expectation", key)
	}
}

// Fixture measurements are intentionally separate from real repository rates.
// A passed regression contract may still have explicitly documented static gaps.
type fixtureResult struct {
	Language   string      `json:"language"`
	Status     string      `json:"status"`
	Contracts  string      `json:"contracts,omitempty"`
	Documents  int         `json:"documents"`
	Evaluation *evaluation `json:"evaluation,omitempty"`
}

func writeFixtureSummary(t *testing.T, results []fixtureResult, status string) {
	t.Helper()
	if *corpusNames == "" {
		return
	}
	dir := filepath.Join(*reportDir, "fixtures")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "summary.json"), struct {
		Status   string          `json:"status"`
		Fixtures []fixtureResult `json:"fixtures"`
	}{status, results})
	var text strings.Builder
	fmt.Fprintf(&text, "# Semantic fixtures\n\nStatus: %s. Fixed examples, separate from repository accuracy. Passing contracts do not erase known call gaps.\n\n| Language | State | Contracts | Call targets | Localized missing | Unknown/dynamic/external calls | Evidence |\n|---|---|---|---|---|---|---|\n", status)
	for _, r := range results {
		if r.Evaluation == nil {
			fmt.Fprintf(&text, "| %s | %s | — | — | — | — | — |\n", r.Language, r.Status)
			continue
		}
		b := r.Evaluation.Bindings[cg.Calls]
		unassessed := 0
		for key, n := range r.Evaluation.Unassessed {
			if strings.HasPrefix(key, "calls/") {
				unassessed += n
			}
		}
		fmt.Fprintf(&text, "| %s | %s | %s | %d/%d | %d | %d | [%s](%s/result.json) |\n", r.Language, r.Status, r.Contracts, b.Hit, b.Expected, b.LocalizedMissing, unassessed, r.Language, r.Language)
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.md"), []byte(text.String()), 0644); err != nil {
		t.Fatal(err)
	}
}
