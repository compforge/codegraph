package codegraph

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

// All tiers must survive publication, properties, relation values and paths.
func TestConfidencePublicationAndQuery(t *testing.T) {
	ctx := context.Background()
	g, _, err := Build(ctx, "tiers", []Document{
		{Path: "a.go", Content: []byte("package a;type I interface { Run() };type Box struct{};func(Box) Run(){};func work(){};func entry(b Box){work();b.Run()}")},
		{Path: "a.ts", Content: []byte("class Box { run() {} };function entry(x: any) { x.run(); }")},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	found := map[Confidence]bool{}
	originals := map[string]Relation{}
	for _, r := range g.Relations() {
		var derived Confidence
		for _, e := range r.Evidence {
			if !e.Confidence.Valid() {
				t.Fatal(e)
			}
			derived = derived.Stronger(e.Confidence)
		}
		if !r.Confidence.Valid() || r.Confidence != derived {
			t.Fatal(r)
		}
		found[r.Confidence] = true
		originals[r.ID] = r
	}
	if len(found) != 4 {
		t.Fatal(found)
	}
	rows, err := g.Query(ctx, "MATCH ()-[r]->() RETURN r, r.confidence AS confidence, r.evidenceData AS evidence", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		r := row["r"].(Relation)
		var proof []Evidence
		if err := json.Unmarshal([]byte(row["evidence"].(string)), &proof); err != nil {
			t.Fatal(err)
		}
		if row["confidence"] != string(r.Confidence) || !reflect.DeepEqual(proof, r.Evidence) || !reflect.DeepEqual(r, originals[r.ID]) {
			t.Fatal(row)
		}
	}
	rows, err = g.Query(ctx, "MATCH ()-[r]->() WHERE r.confidence IN ['exact', 'scoped'] RETURN r", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := 0
	for _, r := range originals {
		if r.Confidence.AtLeast(Scoped) {
			want++
		}
	}
	if len(rows) != want {
		t.Fatalf("threshold query: got %d want %d", len(rows), want)
	}
	for _, row := range rows {
		if !row["r"].(Relation).Confidence.AtLeast(Scoped) {
			t.Fatal(row)
		}
	}
	rows, err = g.Query(ctx, "MATCH p=()-[:calls*1..1]->() RETURN p", nil)
	if err != nil || len(rows) == 0 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		for _, r := range row["p"].(Path).Relations {
			if !reflect.DeepEqual(r, originals[r.ID]) {
				t.Fatal(r)
			}
		}
	}
}

// +case=`An omitted declaration preserves unrelated nodes and module facts from other documents`
func TestOutlineGapPreservesUsableFacts(t *testing.T) {
	ctx := context.Background()
	// Deliberately ambiguous query: diagnostics must remain testable when a
	// real language's previously ambiguous declarations become supported.
	entry := *grammars.DetectLanguageByName("python")
	entry.Name, entry.Extensions = "codegraph-conflicting-outline", []string{".cggap"}
	entry.TagsQuery = `(function_definition name: (identifier) @name) @definition.function
(function_definition parameters: (parameters (identifier) @name)) @definition.function`
	grammars.Register(entry)
	source := "def retry(text):\n    pass\ndef healthy():\n    pass\n"
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	document := Document{Path: "agent.cggap", Content: []byte(source)}
	facts, err := g.AddDocument(ctx, document).Wait()
	if err != nil {
		t.Fatal(err)
	}
	module := Document{Path: "app.ts", Content: []byte("import { work } from './work'; export { work as task };")}
	moduleFacts, err := g.AddDocument(ctx, module).Wait()
	if err != nil {
		t.Fatal(err)
	}
	if len(moduleFacts.Imports) == 0 || moduleFacts.Exports["task"] != "work" {
		t.Fatalf("outline gap discarded module facts: %+v", moduleFacts)
	}
	if err := g.AddDocuments(ctx, Document{Path: "work.ts", Content: []byte("export function work() {}")}); err != nil {
		t.Fatal(err)
	}
	report, err := g.Wait(ctx)
	// The generic extension also reports unsupported resolution. This test
	// isolates the declaration receipt from unrelated capability diagnostics.
	declarations := func(items []Diagnostic) []Diagnostic {
		var result []Diagnostic
		for _, item := range items {
			if item.Subject == DeclarationsSubject {
				result = append(result, item)
			}
		}
		return result
	}
	report.Diagnostics = declarations(report.Diagnostics)
	facts.Issues = declarations(facts.Issues)
	if err != nil || len(report.Diagnostics) != 1 {
		t.Fatal(report, err)
	}
	d := report.Diagnostics[0]
	if d.Code != "outline_incomplete" || d.Subject != DeclarationsSubject || d.Relation != "" ||
		d.Outline == nil || d.Outline.OmittedNameConflict != 2 {
		t.Fatalf("missing structured outline gap: %+v", d)
	}
	if d.Location.Path != document.Path || d.Location.StartByte != 0 || d.Location.EndByte != len(source) {
		t.Fatalf("upstream counters must describe the document, not a guessed declaration: %+v", d.Location)
	}
	if len(g.Find(document.Path, Function, "healthy")) != 1 {
		t.Fatal("unrelated declaration disappeared")
	}
	edges := g.RelationsFrom(module.ID(), Imports)
	moduleEdge := false
	for _, edge := range edges {
		target, ok := g.Node(edge.Target)
		if ok && target.Kind == Module && target.QualifiedName == "work" && edge.Confidence == Exact {
			moduleEdge = true
		}
	}
	if !moduleEdge {
		t.Fatalf("outline gap discarded or downgraded import: %+v", edges)
	}
	if !reflect.DeepEqual(facts.Issues, report.Diagnostics) {
		t.Fatalf("early facts and published report disagree: %+v %+v", facts.Issues, report.Diagnostics)
	}
	// Detached early facts, Report and Wait must not leak nested coverage values
	// back into the retained cache or another consumer's report.
	facts.Issues[0].Outline.OmittedNameConflict = 99
	report.Diagnostics[0].Outline.OmittedNameConflict = 99
	again, err := g.Wait(ctx)
	if err != nil || again.Diagnostics[0].Outline.OmittedNameConflict != 2 {
		t.Fatal(again, err)
	}
	snapshot := g.Report()
	snapshot.Diagnostics[0].Outline.OmittedNameConflict = 99
	if g.Report().Diagnostics[0].Outline.OmittedNameConflict != 2 {
		t.Fatal("Report aliases published coverage")
	}
	extracted, err := g.Extract(ctx, document)
	if err != nil || extracted.Issues[0].Outline.OmittedNameConflict != 2 {
		t.Fatal(extracted, err)
	}
	encoded, err := json.Marshal(again)
	if err != nil || strings.Contains(string(encoded), `"complete"`) || !strings.Contains(string(encoded), `"omittedNameConflict":2`) {
		t.Fatalf("report lost local coverage or introduced a global verdict: %s %v", encoded, err)
	}
}

// +case=`Scoped edges coexist with exact edges and locally unresolved references`
func TestCandidateEvidenceAndUnresolvedLocations(t *testing.T) {
	source := "import './lib';\nimport './missing';\nfunction target() {}\nfunction entry() { target(); unknown(); }\n"
	g, report, err := Build(context.Background(), "rev", []Document{
		{Path: "app.ts", Content: []byte(source)},
		{Path: "lib.ts", Content: []byte("export function work() {}")},
		{Path: "lib.js", Content: []byte("export function work() {}")},
	}, Options{})
	report.Diagnostics = withoutReferenceDiagnostics(report.Diagnostics)
	if err != nil || len(report.Diagnostics) != 2 {
		t.Fatal(report, err)
	}
	for _, d := range report.Diagnostics {
		if d.Subject != RelationsSubject || d.Location.Path != "app.ts" || d.Location.EndByte <= d.Location.StartByte {
			t.Fatalf("unlocalized relation gap: %+v", d)
		}
		switch d.Code {
		case "unresolved_import":
			if d.Relation != Imports || d.Location.Line != 2 {
				t.Fatal(d)
			}
		case "unresolved_call":
			if d.Relation != Calls || d.Location.Line != 4 || !strings.Contains(source[d.Location.StartByte:d.Location.EndByte], "unknown") {
				t.Fatal(d)
			}
		default:
			t.Fatalf("candidate edges must not also become coverage gaps: %+v", d)
		}
	}
	imports := g.RelationsFrom(DocumentID("app.ts"), Imports)
	if len(imports) != 2 {
		t.Fatal(imports)
	}
	for _, edge := range imports {
		if edge.Confidence != Scoped || edge.Evidence[0].Basis == "" {
			t.Fatal(edge)
		}
	}
	entry := g.Find("app.ts", Function, "entry")
	if len(entry) != 1 {
		t.Fatal(entry)
	}
	calls := g.RelationsFrom(entry[0].ID, Calls)
	if len(calls) != 1 || calls[0].Confidence != Exact {
		t.Fatalf("unresolved call contaminated exact call: %+v", calls)
	}
}

func TestDuplicateOutlineCandidatesAreNotGaps(t *testing.T) {
	entry := *grammars.DetectLanguageByName("python")
	entry.Name, entry.Extensions = "codegraph-duplicate-outline", []string{".cgduplicates"}
	pattern := "(function_definition name: (identifier) @name) @definition.function"
	entry.TagsQuery = pattern + "\n" + pattern
	grammars.Register(entry)
	g, report, err := Build(context.Background(), "rev", []Document{{Path: "app.cgduplicates", Content: []byte("def work():\n    pass\n")}}, Options{})
	if err != nil || hasDiagnostic(report, "outline_incomplete") || len(g.Find("app.cgduplicates", Function, "work")) != 1 {
		t.Fatal(report, err)
	}
}

func withoutReferenceDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(diagnostics))
	for _, d := range diagnostics {
		if d.Relation != References {
			out = append(out, d)
		}
	}
	return out
}

func TestEvidenceBudgetIsAtomic(t *testing.T) {
	g, _ := New("budget", Options{MaxEvidence: 1})
	if err := g.AddDocuments(context.Background(), Document{Path: "a.go", Content: []byte("package a;func f(){}")}); err != nil {
		t.Fatal(err)
	}
	_, err := g.Wait(context.Background())
	if !errors.Is(err, ErrBuildBudget) || len(g.Nodes()) != 0 {
		t.Fatal(err, g.Nodes())
	}
}
