package codegraph

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

// +case=`An omitted declaration preserves imports, exports and unrelated nodes`
func TestOutlineGapPreservesUsableFacts(t *testing.T) {
	ctx := context.Background()
	source := "import { work } from './work';\nexport { work as task };\nclass Agent { private async *retry(text: string): AsyncGenerator<Event> {} }\nexport function healthy() {}\n"
	g, err := New("rev", Options{})
	if err != nil {
		t.Fatal(err)
	}
	document := Document{Path: "agent.ts", Content: []byte(source)}
	facts, err := g.AddDocument(ctx, document).Wait()
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Imports) == 0 || facts.Exports["task"] != "work" {
		t.Fatalf("outline gap discarded module facts: %+v", facts)
	}
	if err := g.AddDocuments(ctx, Document{Path: "work.ts", Content: []byte("export function work() {}")}); err != nil {
		t.Fatal(err)
	}
	report, err := g.Wait(ctx)
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
	if len(g.Find("agent.ts", Function, "healthy")) != 1 {
		t.Fatal("unrelated declaration disappeared")
	}
	edges := g.RelationsFrom(document.ID(), Imports)
	if len(edges) != 1 || edges[0].Target != FileID("work.ts") || edges[0].Confidence != Exact {
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

// +case=`Candidate edges coexist with exact edges and locally unresolved references`
func TestCandidateEvidenceAndUnresolvedLocations(t *testing.T) {
	source := "import './lib';\nimport './missing';\nfunction target() {}\nfunction entry() { target(); unknown(); }\n"
	g, report, err := Build(context.Background(), "rev", []Document{
		{Path: "app.ts", Content: []byte(source)},
		{Path: "lib.ts", Content: []byte("export function work() {}")},
		{Path: "lib.js", Content: []byte("export function work() {}")},
	}, Options{})
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
	imports := g.RelationsFrom(FileID("app.ts"), Imports)
	if len(imports) != 2 {
		t.Fatal(imports)
	}
	for _, edge := range imports {
		if edge.Confidence != Candidate || edge.Basis == "" {
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
