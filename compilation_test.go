package codegraph

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language"
	"github.com/compforge/codegraph/internal/pipeline"
)

// +case:id=publication_budget,desc=`Many source uses fit a small selected graph without allocating Reference nodes`
func TestPublicationBudgetExcludesUnselectedUses(t *testing.T) {
	ctx := context.Background()
	doc := Document{Path: "app.ts", Content: []byte("function run() {}\nfunction handle() {\n" + strings.Repeat("run();\n", 100) + "}\n")}
	full, _, err := Build(ctx, "full", []Document{doc}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	policy := AnalysisAdmission()
	graph, report, err := Build(ctx, "analysis", []Document{doc}, Options{Admission: policy, MaxNodes: 5})
	if err != nil {
		t.Fatal(err)
	}
	if report.Publication.References != 0 || report.Analysis.Uses < 100 || len(full.Nodes()) <= 100 || len(graph.Nodes()) > 5 {
		t.Fatalf("unexpected work: %+v", report)
	}
	for _, node := range graph.Nodes() {
		if node.Kind == Reference {
			t.Fatal("unselected use was materialized")
		}
	}
	if _, _, err := Build(ctx, "full", []Document{doc}, Options{MaxNodes: 5}); !errors.Is(err, ErrBuildBudget) {
		t.Fatalf("full graph should exhaust publication: %v", err)
	}
}

// +case:id=analysis_budget_atomicity,desc=`Every analysis and selection budget fails without replacing a previous graph`
func TestAnalysisBudgetsPreservePublication(t *testing.T) {
	doc := Document{Path: "app.ts", Content: []byte("function run(){}\nfunction handle(){const local=run();run();return local;}\n")}
	for _, tc := range []struct {
		name   string
		limits AnalysisLimits
		stage  string
	}{
		{"Entities", AnalysisLimits{Entities: 1}, "analysis"},
		{"Uses", AnalysisLimits{Uses: 1}, "analysis"},
		{"Relations", AnalysisLimits{Relations: 1}, "analysis"},
		{"Evidence", AnalysisLimits{Evidence: 1}, "analysis"},
		{"SelectionSteps", AnalysisLimits{SelectionSteps: 1}, "selection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := NewBuilder("budget", Options{Admission: AnalysisAdmission(), Analysis: tc.limits})
			if err != nil {
				t.Fatal(err)
			}
			old := b.Result()
			_, err = b.addDocumentsSync(context.Background(), doc)
			var budget *BuildBudgetError
			if !errors.As(err, &budget) || !errors.Is(err, ErrBuildBudget) || budget.Stage != tc.stage || budget.Resource != tc.name {
				t.Fatalf("wrong failure: %v", err)
			}
			if b.Result() != old {
				t.Fatal("failed stage replaced the previous graph")
			}
		})
	}
}

func TestSelectorAndAssemblerLeaveSemanticFactsUnchanged(t *testing.T) {
	ctx := context.Background()
	extractor := newTestExtractor(t, nil)
	facts := extractTestFacts(t, extractor, "app.ts", "function run(){}\nfunction handle(){const local=run();return local;}")
	scope := analysis.NewBuildScope(map[string]analysis.Facts{"app.ts": *facts.raw}, "")
	analyzer := pipeline.Analyzer{Limits: analysis.Limits{Entities: 1000, Relations: 1000, Evidence: 1000}, Lookup: language.Lookup}
	index, _, err := analyzer.Analyze(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	entities := make(map[analysis.Ref]analysis.Entity)
	for k, v := range index.Entities {
		entities[k] = v
	}
	edges := append([]analysis.Edge(nil), index.Edges...)
	bindings := append([]analysis.Binding(nil), facts.raw.Lexical.Bindings...)
	opts := Options{Admission: AnalysisAdmission()}
	if err := defaults(&opts); err != nil {
		t.Fatal(err)
	}
	tags, err := NewTagMatcher(nil)
	if err != nil {
		t.Fatal(err)
	}
	var previous SelectionReport
	for i := 0; i < 10; i++ {
		selector := entitySelector{index: index, policy: opts.Admission, limit: opts.Analysis.SelectionSteps}
		selected, err := selector.Select(ctx)
		if err != nil {
			t.Fatal(err)
		}
		assembler := newGraphAssembler(index, selected, opts, tags)
		if err := assembler.Assemble(ctx, selected, nil); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(index.Entities, entities) || !reflect.DeepEqual(index.Edges, edges) || !reflect.DeepEqual(facts.raw.Lexical.Bindings, bindings) {
			t.Fatal("publication mutated semantics")
		}
		current := SelectionReport{Steps: selector.steps}
		if i > 0 && current != previous {
			t.Fatal("selection work depends on map traversal order")
		}
		previous = current
	}
}

func TestSymbolOnlyPolicyPreservesFailureContextByPolicy(t *testing.T) {
	for _, kind := range []NodeKind{DocumentNodeKind, DirectoryNodeKind, Function} {
		b, err := NewBuilder("failure", Options{Admission: &AdmissionPolicy{NodeKinds: []NodeKind{kind}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.AddFailure(Document{Path: "sub/bad.ts", Content: []byte("bad")}, errors.New("fixture failure")); err != nil {
			t.Fatal(err)
		}
		g, r, err := b.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Diagnostics) != 1 {
			t.Fatal(r)
		}
		for _, n := range g.Nodes() {
			if n.Kind != kind {
				t.Fatalf("unexpected context %s for %s", n.Kind, kind)
			}
		}
	}
}

func TestExcludedMaterialDoesNotConsumeAdmissionBudget(t *testing.T) {
	ctx := context.Background()
	docs := []Document{{Path: "node_modules/dependency.ts", Content: []byte("function dependency(){}")}, {Path: "app.ts", Content: []byte("function app(){}")}}
	for _, external := range []bool{false, true} {
		b, err := NewBuilder("excluded", Options{Admission: AnalysisAdmission(), MaxDocuments: 1, MaxSourceBytes: int64(len(docs[1].Content))})
		if err != nil {
			t.Fatal(err)
		}
		if external {
			extractor := newTestExtractor(t, nil)
			for _, doc := range docs {
				facts := extractTestFacts(t, extractor, doc.Path, string(doc.Content))
				if err := b.Add(facts); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := b.Build(ctx); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := b.addDocumentsSync(ctx, docs...); err != nil {
				t.Fatal(err)
			}
		}
		if len(b.Report().Documents) != 1 || !hasDiagnostic(b.Report(), "excluded_material") {
			t.Fatal(b.Report())
		}
		if want := (MaterialReport{Submitted: 2, Accepted: 1, Excluded: 1}); b.Report().Materials != want {
			t.Fatalf("material outcomes = %+v, want %+v", b.Report().Materials, want)
		}
	}
}

// +case:id=material_outcomes,desc=`Scope exclusions and parse failures reconcile with submitted materials`
func TestMaterialOutcomesReconcile(t *testing.T) {
	ctx := context.Background()
	_, report, err := Build(ctx, "scope", []Document{
		{Path: "src/main.go", Content: []byte("package p\nfunc F(){}\n")},
		{Path: "outside.go", Content: []byte("package p\n")},
	}, Options{Scope: []string{"src"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := (MaterialReport{Submitted: 2, Accepted: 1, Excluded: 1}); report.Materials != want {
		t.Fatalf("scope outcomes = %+v, want %+v", report.Materials, want)
	}
	if !hasDiagnostic(report, "out_of_scope") {
		t.Fatal("scope diagnostic lost")
	}
	b, err := NewBuilder("failed", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddFailure(Document{Path: "bad.go"}, errors.New("parser failed")); err != nil {
		t.Fatal(err)
	}
	_, report, err = b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := (MaterialReport{Submitted: 1, Failed: 1}); report.Materials != want {
		t.Fatalf("parse outcomes = %+v, want %+v", report.Materials, want)
	}
}

// +case:id=excluded_snapshot_identity,desc=`Material exclusion cannot hide changes to an admitted or staged path`
func TestExcludedMaterialPreservesSnapshotIdentity(t *testing.T) {
	ctx := context.Background()
	extractor := newTestExtractor(t, nil)
	link, err := extractor.Extract(ctx, Document{Path: "main.go", Gitlink: strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	source := extractTestFacts(t, extractor, "main.go", "package p\nfunc F(){}\n")
	for _, batch := range []bool{false, true} {
		b, err := NewBuilder("identity", Options{Admission: &AdmissionPolicy{ExcludedTags: []Tag{SourceTag}}})
		if err != nil {
			t.Fatal(err)
		}
		if !batch {
			if err := b.Add(link); err != nil {
				t.Fatal(err)
			}
			if _, _, err := b.Build(ctx); err != nil {
				t.Fatal(err)
			}
		}
		previous := b.Result()
		if batch {
			err = b.Add(link, source)
		} else {
			err = b.Add(source)
		}
		if !errors.Is(err, ErrSnapshotChanged) {
			t.Fatalf("batch=%v: want snapshot error, got %v", batch, err)
		}
		if b.Result() != previous {
			t.Fatal("rejected admission replaced the graph")
		}
		_, report, err := b.Build(ctx)
		if err != nil {
			t.Fatal(err)
		}
		want := MaterialReport{Submitted: 1, Accepted: 1}
		if batch {
			want = MaterialReport{}
		}
		if report.Materials != want || hasDiagnostic(report, "excluded_material") {
			t.Fatalf("rejected batch changed materials: %+v", report)
		}
	}
}

// Excluded facts are not admitted; a later eligible material owns the path outcome.
func TestAdmittedMaterialReplacesExcludedOutcome(t *testing.T) {
	ctx := context.Background()
	extractor := newTestExtractor(t, nil)
	source := extractTestFacts(t, extractor, "main.go", "package p\n")
	link, err := extractor.Extract(ctx, Document{Path: "main.go", Gitlink: strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewBuilder("outcome", Options{Admission: &AdmissionPolicy{ExcludedTags: []Tag{SourceTag}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Add(source, link); err != nil {
		t.Fatal(err)
	}
	_, report, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Materials != (MaterialReport{Submitted: 1, Accepted: 1}) || hasDiagnostic(report, "excluded_material") {
		t.Fatalf("stale excluded outcome: %+v", report)
	}
}

// +case:id=resolver_budget,desc=`Resolver exhaustion retains typed budget classification and the previous graph`
func TestResolverBudgetPreservesClassification(t *testing.T) {
	ctx := context.Background()
	extractor := newTestExtractor(t, nil)
	facts := extractTestFacts(t, extractor, "main.go", "package p\nfunc F(){}\nfunc G(){F();F();F();F();F();}\n")
	b, err := NewBuilder("budget", Options{Analysis: AnalysisLimits{Evidence: 8}})
	if err != nil {
		t.Fatal(err)
	}
	previous := b.Result()
	if err := b.Add(facts); err != nil {
		t.Fatal(err)
	}
	_, _, err = b.Build(ctx)
	var budget *BuildBudgetError
	if !errors.Is(err, ErrBuildBudget) || !errors.As(err, &budget) {
		t.Fatalf("resolver lost budget classification: %v", err)
	}
	if budget.Stage != "analysis" || budget.Resource != "Evidence" || budget.Limit != 8 || budget.Used+budget.Adding <= budget.Limit {
		t.Fatalf("wrong resolver budget details: %+v", budget)
	}
	if b.Result() != previous {
		t.Fatal("failed resolver replaced graph")
	}
}
