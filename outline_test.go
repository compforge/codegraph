package codegraph

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// +case=Outline preserves upstream ranges, lexical children and receiver names after the parser tree is released.
func TestOutlineUpstreamContract(t *testing.T) {
	e, err := NewExtractor(ExtractionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, language, source string }{
		{"box.go", "go", "package p\ntype Box struct{ Value int }\nfunc (b *Box) Run() {}\n"},
		{"box.py", "python", "class Box:\n    def run(self):\n        def nested():\n            pass\n        return nested()\n"},
		{"box.rs", "rust", "struct Box { value: i32 }\nfn run() {}\n"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			doc := Document{Path: tc.path, Content: []byte(tc.source)}
			got, report, err := e.Outline(context.Background(), doc)
			if err != nil {
				t.Fatal(err)
			}
			entry := *grammars.DetectLanguageByName(tc.language)
			lang := entry.Language()
			parser := gts.NewParser(lang)
			var tree *gts.Tree
			if entry.TokenSourceFactory != nil {
				tree, err = parser.ParseWithTokenSourceStrict(doc.Content, entry.TokenSourceFactory(doc.Content, lang))
			} else {
				tree, err = parser.ParseStrict(doc.Content)
			}
			if err != nil {
				t.Fatal(err)
			}
			outliner, err := gts.NewOutliner(lang, grammars.ResolveTagsQuery(entry), gts.WithOutlineOwnerRules(grammars.OutlineOwnerRules(entry)))
			if err != nil {
				tree.Release()
				t.Fatal(err)
			}
			want, wantReport := outliner.OutlineTree(tree)
			tree.Release()
			if len(got) == 0 || report.Declined() || report.TreeHasError {
				t.Fatalf("outline=%+v report=%+v", got, report)
			}
			if !reflect.DeepEqual(got, want) || report != wantReport {
				t.Fatalf("got %+v / %+v; want %+v / %+v", got, report, want, wantReport)
			}
			if tc.language == "go" {
				var method *gts.OutlineSymbol
				for i := range got {
					if got[i].Name == "Run" {
						method = &got[i]
					}
				}
				if method == nil || method.Owner != "Box" {
					t.Fatalf("receiver method must remain a root with owner: %+v", got)
				}
			}
			if tc.language == "python" && (got[0].Name != "Box" || len(got[0].Children) != 1 || got[0].Children[0].Name != "run" || len(got[0].Children[0].Children) != 1) {
				t.Fatalf("lost lexical nesting: %+v", got)
			}
		})
	}
}

// +case=Outline and graph extraction share one cached parse; mutation of nested outline values cannot alter subsequent views or graph members.
func TestOutlineCacheAndGraphOwnership(t *testing.T) {
	ctx := context.Background()
	cache, err := NewExtractionCache(4, 4096)
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewExtractor(ExtractionOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	parsed := 0
	parseObserver = func(string) { parsed++ }
	defer func() { parseObserver = nil }()
	doc := Document{Path: "box.ts", Content: []byte("export class Box { run() {} }\n")}
	outline, report, err := e.Outline(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(outline) != 1 || outline[0].Name != "Box" || len(outline[0].Children) != 1 || outline[0].Children[0].Name != "run" {
		t.Fatalf("outline=%+v", outline)
	}
	outline[0].Children[0].Name = "corrupt"
	outline[0].Name = "corrupt"
	report.Symbols = -1
	facts, err := e.Extract(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	got, receipt, err := facts.Outline()
	if err != nil || got[0].Name != "Box" || got[0].Children[0].Name != "run" || receipt.Symbols != 2 {
		t.Fatalf("cached outline=%+v receipt=%+v err=%v", got, receipt, err)
	}
	got[0].Children[0].Range.StartByte = 999
	view, err := facts.View()
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := view.Outline()
	if err != nil || again[0].Children[0].Range.StartByte == 999 {
		t.Fatal("nested range aliases stored outline")
	}
	taskFacts, err := e.Submit(ctx, doc).Wait()
	if err != nil {
		t.Fatal(err)
	}
	taskOutline, _, err := taskFacts.Outline()
	if err != nil || !reflect.DeepEqual(again, taskOutline) {
		t.Fatalf("task outline changed: %v", err)
	}
	builder, err := NewBuilder("outline-snapshot", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.Add(facts); err != nil {
		t.Fatal(err)
	}
	graph, _, err := builder.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	classes := graph.Find(doc.Path, Class, "Box")
	methods := graph.Find(doc.Path, Method, "Box.run")
	if len(classes) != 1 || len(methods) != 1 {
		t.Fatalf("declarations changed: %+v", graph.Nodes())
	}
	found := false
	for _, rel := range graph.RelationsFrom(classes[0].ID, Contains) {
		if rel.Target == methods[0].ID {
			found = true
		}
	}
	if !found {
		t.Fatal("missing semantic membership")
	}
	if parsed != 1 {
		t.Fatalf("parsed %d times; want one shared parse", parsed)
	}
	doc.Content = []byte("export function changed() {}\n")
	changed, _, err := e.Outline(ctx, doc)
	if err != nil || len(changed) != 1 || changed[0].Name != "changed" || parsed != 2 {
		t.Fatalf("content version reused stale outline: %+v, parses=%d err=%v", changed, parsed, err)
	}
}

func TestOutlineEmptyUnavailableAndFailure(t *testing.T) {
	e, err := NewExtractor(ExtractionOptions{MaxDocumentBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	symbols, report, err := e.Outline(context.Background(), Document{Path: "empty.go", Content: []byte("package p\n")})
	if err != nil || len(symbols) != 0 || report.Declined() {
		t.Fatalf("empty source is not unavailable: %+v %v", report, err)
	}
	for _, doc := range []Document{
		{Path: "unknown.zzz", Content: []byte("some text")},
		{Path: "sdk", Gitlink: strings.Repeat("a", 40)},
	} {
		facts, err := e.Extract(context.Background(), doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := facts.Outline(); err == nil {
			t.Fatalf("%s must not report a valid empty outline", doc.Path)
		}
		if _, _, err := e.Outline(context.Background(), doc); err == nil {
			t.Fatalf("%s must report unavailable", doc.Path)
		}
	}
	if _, _, err := (Facts{}).Outline(); err == nil {
		t.Fatal("zero Facts accepted")
	}
	if _, _, err := e.Outline(context.Background(), Document{Path: "bad.go", Content: []byte("package p\nfunc (")}); err == nil {
		t.Fatal("malformed input accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := e.Outline(ctx, Document{Path: "a.go"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if _, _, err := e.Outline(context.Background(), Document{Path: "large.go", Content: []byte(strings.Repeat(" ", 129))}); !errors.Is(err, ErrBuildBudget) {
		t.Fatalf("budget=%v", err)
	}
}

// +case=Partial outlines retain usable symbols and the unchanged upstream omission receipt.
func TestOutlineCoverageReport(t *testing.T) {
	entry := *grammars.DetectLanguageByName("python")
	entry.Name, entry.Extensions = "codegraph-public-outline-gap", []string{".cgoutlinegap"}
	entry.TagsQuery = `(function_definition name: (identifier) @name) @definition.function
(function_definition parameters: (parameters (identifier) @name)) @definition.function`
	grammars.Register(entry)
	e, err := NewExtractor(ExtractionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	doc := Document{Path: "app.cgoutlinegap", Content: []byte("def retry(text):\n    pass\ndef healthy():\n    pass\n")}
	symbols, report, err := e.Outline(context.Background(), doc)
	if err != nil || len(symbols) != 1 || symbols[0].Name != "healthy" || report.OmittedNameConflict != 2 || report.Declined() {
		t.Fatalf("partial outline=%+v report=%+v err=%v", symbols, report, err)
	}
	symbols, report, err = e.Outline(context.Background(), Document{Path: "data.json", Content: []byte(`{"x":1}`)})
	if err != nil || len(symbols) != 0 || report.DeclineReason != gts.OutlineDeclineQueryEmpty {
		t.Fatalf("declined query=%+v err=%v", report, err)
	}
	entry.Name, entry.Extensions = "codegraph-public-outline-invalid-query", []string{".cgoutlineinvalid"}
	entry.TagsQuery = `(node_that_does_not_exist) @definition.function`
	grammars.Register(entry)
	doc.Path = "app.cgoutlineinvalid"
	facts, err := e.Extract(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := facts.Outline(); err == nil {
		t.Fatal("invalid query must report an error")
	}
}
