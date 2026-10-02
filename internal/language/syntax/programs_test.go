package syntax

import (
	"reflect"
	"sync"
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	grammarblobs "github.com/odvcencio/gotreesitter/grammars/grammar_blobs"
)

func programTree(t *testing.T, lang *gts.Language, source string) *gts.Tree {
	t.Helper()
	tree, err := gts.NewParser(lang).ParseStrict([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Release)
	return tree
}

// +case=Concurrent cold compilation preserves fresh-program output across independent trees and retains no result buffers.
func TestSharedProgramsConcurrent(t *testing.T) {
	entry := *grammars.DetectLanguageByName("go")
	// A distinct grammar gives this test a cold cache without clearing shared state.
	lang := freshGoGrammar(t)
	entry.Language = func() *gts.Language { return lang }
	entry.TagsQuery = `(function_declaration name: (identifier) @name) @definition.function`
	kinds := gts.FactDefinitions | gts.FactCalls | gts.FactImports
	freshFacts, err := gts.NewFactProgram(lang, kinds)
	if err != nil {
		t.Fatal(err)
	}
	freshOutline, err := gts.NewOutliner(lang, entry.TagsQuery, gts.WithOutlineOwnerRules(grammars.OutlineOwnerRules(entry)))
	if err != nil {
		t.Fatal(err)
	}
	sources := []string{
		"package first\nfunc Entry() { Target() }\nfunc Target() {}\n",
		"package second\nimport \"fmt\"\nfunc Other() { fmt.Println(1) }\n",
	}
	type expected struct {
		tree    *gts.Tree
		facts   gts.FactSet
		outline []gts.OutlineSymbol
		report  gts.OutlineReport
	}
	var cases []expected
	for i := range 16 {
		tree := programTree(t, lang, sources[i%len(sources)])
		outline, report := freshOutline.OutlineTree(tree)
		facts := freshFacts.Extract(tree)
		if len(facts.Definitions) == 0 || len(outline) == 0 {
			t.Fatal("fixture has no declarations")
		}
		cases = append(cases, expected{tree, facts, outline, report})
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, tc := range cases {
		wg.Go(func() {
			<-start
			program, err := FactProgram(lang, kinds)
			if err != nil {
				t.Error(err)
				return
			}
			outliner, err := outlineProgram(lang, entry)
			if err != nil {
				t.Error(err)
				return
			}
			for range 8 {
				facts := program.Extract(tc.tree)
				outline, report := outliner.OutlineTree(tc.tree)
				if !reflect.DeepEqual(facts, tc.facts) || !reflect.DeepEqual(outline, tc.outline) || !reflect.DeepEqual(report, tc.report) {
					t.Error("shared program differs from fresh extraction")
					return
				}
				// Callers own results; mutation must not affect later extractions.
				facts.Definitions[0].Name = "mutated"
				outline[0].Name = "mutated"
			}
		})
	}
	close(start)
	wg.Wait()
}

// +case=Program reuse preserves grammar identity, selected fact kinds, query changes, and language-specific owner rules.
func TestSharedProgramConfiguration(t *testing.T) {
	entry := *grammars.DetectLanguageByName("go")
	lang := entry.Language()
	tree := programTree(t, lang, "package p\ntype Box struct{}\nfunc (b Box) Work() {}\nfunc Entry() { Target() }\n")
	definitions, err := FactProgram(lang, gts.FactDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	calls, err := FactProgram(lang, gts.FactCalls)
	if err != nil {
		t.Fatal(err)
	}
	if facts := definitions.Extract(tree); len(facts.Definitions) == 0 || len(facts.Calls) != 0 {
		t.Fatal(facts)
	}
	if facts := calls.Extract(tree); len(facts.Calls) != 1 || len(facts.Definitions) != 0 {
		t.Fatal(facts)
	}
	otherLang := freshGoGrammar(t)
	otherTree := programTree(t, otherLang, "package p\nfunc Other() {}\n")
	other, err := FactProgram(otherLang, gts.FactDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	if facts := other.Extract(otherTree); len(facts.Definitions) != 1 || facts.Definitions[0].Name != "Other" {
		t.Fatal(facts)
	}
	for _, tc := range []struct{ name, query, want, owner string }{
		{"go", `(method_declaration name: (field_identifier) @name) @definition.method`, "Work", "Box"},
		{"program-test-alias", `(method_declaration name: (field_identifier) @name) @definition.method`, "Work", ""},
		{"go", `(function_declaration name: (identifier) @name) @definition.function`, "Entry", ""},
	} {
		entry.Name, entry.TagsQuery = tc.name, tc.query
		outliner, err := outlineProgram(lang, entry)
		if err != nil {
			t.Fatal(err)
		}
		outline, report := outliner.OutlineTree(tree)
		if len(outline) != 1 || outline[0].Name != tc.want || outline[0].Owner != tc.owner || report.Declined() {
			t.Fatalf("%s: %+v, %+v", tc.name, outline, report)
		}
	}
	entry.TagsQuery = `(not_a_valid_go_node) @name`
	for range 2 {
		if _, err := outlineProgram(lang, entry); err == nil {
			t.Fatal("invalid query must still fail after reuse")
		}
	}
}

func freshGoGrammar(t *testing.T) *gts.Language {
	t.Helper()
	lang, err := grammars.LoadLanguage("go", grammarblobs.Go())
	if err != nil {
		t.Fatal(err)
	}
	return lang
}
