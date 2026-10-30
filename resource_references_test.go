package codegraph

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func assetRules() []ResourceReferenceRule {
	return []ResourceReferenceRule{{Callable: Callable{Module: "asset-kit", Name: "locate"}, PathArgument: 1, BaseArgument: 2, Basis: "asset_kit_path"}}
}
func assetOptions() Options { return Options{ResourceReferences: assetRules()} }
func resourceUses(g *Graph) []Node {
	var uses []Node
	for _, n := range g.Nodes() {
		if n.Kind == Reference && n.Resource != nil {
			uses = append(uses, n)
		}
	}
	return uses
}

// +case:id=resource_reference_custom_api,desc=`A caller declares an arbitrary imported path factory`,expect=`References bind without hardcoded API names or argument positions`
func TestResourceReferenceCustomAPI(t *testing.T) {
	for _, ext := range []string{"js", "ts", "tsx"} {
		for _, tc := range []struct{ name, imports, call string }{
			{"named", `import {locate} from "asset-kit";`, "locate"},
			{"alias", `import {locate as asset} from "asset-kit";`, "asset"},
			{"namespace", `import * as assets from "asset-kit";`, "assets.locate"},
			{"default", `import assets from "asset-kit";`, "assets.locate"},
		} {
			t.Run(ext+"/"+tc.name, func(t *testing.T) {
				source := tc.imports + ` function render(){ return arbitrarySink(` + tc.call + `(0,'./template.html',import.meta.url)); }`
				docs := []Document{{Path: "src/view." + ext, Content: []byte(source)}, {Path: "src/template.html", Content: []byte("hello")}}
				g, _, err := Build(context.Background(), "resource", docs, assetOptions())
				if err != nil {
					t.Fatal(err)
				}
				uses := resourceUses(g)
				if len(uses) != 1 {
					t.Fatalf("uses=%+v report=%+v", uses, g.Report())
				}
				use := uses[0]
				if use.ReferenceKind != SymbolReference || use.Resource.Path != "src/template.html" || use.Resource.Expression != "'./template.html'" {
					t.Fatal(use)
				}
				if !strings.HasPrefix(source[use.Location.StartByte:use.Location.EndByte], tc.call+"(") {
					t.Fatal(use)
				}
				for _, id := range []string{use.ID, g.Find("src/view."+ext, Function, "render")[0].ID} {
					edges := g.RelationsFrom(id, References)
					found := slices.ContainsFunc(edges, func(r Relation) bool {
						return r.Target == DocumentID("src/template.html") && r.Confidence == Exact && len(r.Evidence) == 1 && r.Evidence[0].Basis == "asset_kit_path"
					})
					if !found {
						t.Fatal(edges)
					}
				}
				rows := query(t, g, `MATCH (r:Reference)-[:references]->(d:Document) RETURN r.resourcePath AS path, d.path AS target`, nil)
				if len(rows) != 1 || rows[0]["path"] != "src/template.html" || rows[0]["target"] != "src/template.html" {
					t.Fatal(rows)
				}
				use.Resource.Path = "mutated"
				copy, _ := g.Node(use.ID)
				if copy.Resource.Path != "src/template.html" {
					t.Fatal("graph metadata alias")
				}
			})
		}
	}
}

func TestResourceReferenceUnknownPathsAndBindings(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		refs         int
		target       string
	}{
		{"local function", `function locate(a,b,c){};locate(0,'./template.html',import.meta.url)`, 0, ""},
		{"other module", `import {locate} from 'other-kit'; locate(0,'./template.html',import.meta.url)`, 0, ""},
		{"parameter shadow", `import {locate} from 'asset-kit'; function f(locate){locate(0,'./template.html',import.meta.url)}`, 0, ""},
		{"namespace shadow", `import * as assets from 'asset-kit'; function f(assets){assets.locate(0,'./template.html',import.meta.url)}`, 0, ""},
		{"type import", `import type {locate} from 'asset-kit';locate(0,'./template.html',import.meta.url)`, 0, ""},
		{"constructor mismatch", `import {locate} from 'asset-kit';new locate(0,'./template.html',import.meta.url)`, 0, ""},
		{"plain path text", `const path='./template.html';print(path)`, 0, ""},
		{"unknown base", `import {locate} from 'asset-kit';locate(0,'./template.html',base)`, 1, ""},
		{"missing argument", `import {locate} from 'asset-kit';locate()`, 1, ""},
		{"dynamic", `import {locate} from 'asset-kit';locate(0,name,import.meta.url)`, 1, ""},
		{"escaped", `import {locate} from 'asset-kit';locate(0,'./tem\u0070late.html',import.meta.url)`, 1, ""},
		{"escape root", `import {locate} from 'asset-kit';locate(0,'../../template.html',import.meta.url)`, 1, ""},
		{"absolute URL", `import {locate} from 'asset-kit';locate(0,'https://example.com/template.html',import.meta.url)`, 1, ""},
		{"directory", `import {locate} from 'asset-kit';locate(0,'./template.html/',import.meta.url)`, 1, ""},
		{"preceding spread", `import {locate} from 'asset-kit';locate(...names,'./template.html',import.meta.url)`, 1, ""},
		{"spread", `import {locate} from 'asset-kit';locate(0,...names,import.meta.url)`, 1, ""},
		{"normalized", `import {locate} from 'asset-kit';locate(0,'./sub/../template.html',import.meta.url)`, 1, "src/template.html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, report, err := Build(context.Background(), "unknown", []Document{{Path: "src/view.ts", Content: []byte(tc.source)}, {Path: "src/template.html", Content: []byte("hello")}}, assetOptions())
			if err != nil {
				t.Fatal(err)
			}
			uses := resourceUses(g)
			if len(uses) != tc.refs {
				t.Fatalf("uses=%+v diagnostics=%+v", uses, report.Diagnostics)
			}
			for _, use := range uses {
				if use.Resource.Path != tc.target {
					t.Fatal(use)
				}
				if tc.target == "" && (len(g.RelationsFrom(use.ID, References)) != 0 || !slices.ContainsFunc(report.Diagnostics, func(d Diagnostic) bool { return d.Code == "unresolved_resource" })) {
					t.Fatal("lost gap or invented target", report)
				}
			}
		})
	}
}

// +case:id=resource_reference_rebinding,desc=`The target Document arrives after its reference`,expect=`Stable occurrence identity and immutable previous graph`
func TestResourceReferenceSupplement(t *testing.T) {
	ctx := context.Background()
	b, err := NewBuilder("snapshot", assetOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err = b.AddDocuments(ctx, Document{Path: "src/view.ts", Content: []byte(`import {locate as asset} from 'asset-kit';function render(){return asset(0,'../template.html',import.meta.url)}`)}); err != nil {
		t.Fatal(err)
	}
	report, err := b.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := b.Result()
	uses := resourceUses(before)
	if len(uses) != 1 || uses[0].Resource.Path != "template.html" || len(before.RelationsFrom(uses[0].ID, References)) != 0 {
		t.Fatal(uses)
	}
	if !slices.ContainsFunc(report.Diagnostics, func(d Diagnostic) bool { return d.Code == "resource_not_loaded" && d.Relation == References }) {
		t.Fatal(report.Diagnostics)
	}
	if _, ok := before.Node(DocumentID("template.html")); ok {
		t.Fatal("fabricated target")
	}
	if err = b.AddDocuments(ctx, Document{Path: "template.html", Content: []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	report, err = b.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after := b.Result()
	updated := resourceUses(after)
	if len(updated) != 1 || updated[0].ID != uses[0].ID || len(after.RelationsFrom(updated[0].ID, References)) != 1 || len(before.RelationsFrom(uses[0].ID, References)) != 0 {
		t.Fatal(updated)
	}
	if slices.ContainsFunc(report.Diagnostics, func(d Diagnostic) bool { return d.Code == "resource_not_loaded" }) {
		t.Fatal(report.Diagnostics)
	}
}

func TestResourceReferenceCacheAndRuleIsolation(t *testing.T) {
	e := newTestExtractor(t, nil)
	facts := extractTestFacts(t, e, "src/view.ts", `import {locate} from 'asset-kit';locate(0,'./template.html',import.meta.url)`)
	if len(facts.Invocations) != 1 || facts.Invocations[0].Target.Module != "asset-kit" || facts.Invocations[0].Arguments[1].Literal == nil {
		t.Fatal(facts.Invocations)
	}
	facts.Invocations[0].Target.Module = "mutated"
	*facts.Invocations[0].Arguments[1].Literal = "wrong"
	view, err := facts.View()
	if err != nil || view.Invocations[0].Target.Module != "asset-kit" || *view.Invocations[0].Arguments[1].Literal != "./template.html" {
		t.Fatal(view, err)
	}
	opts := assetOptions()
	enabled, err := NewBuilder("enabled", opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.ResourceReferences[0].Name = "mutated"
	if err = enabled.Add(facts); err != nil {
		t.Fatal(err)
	}
	with, _, err := enabled.Build(context.Background())
	if err != nil || len(resourceUses(with)) != 1 {
		t.Fatal(with, err)
	}
	disabled, err := NewBuilder("disabled", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = disabled.Add(facts); err != nil {
		t.Fatal(err)
	}
	without, report, err := disabled.Build(context.Background())
	if err != nil || len(resourceUses(without)) != 0 {
		t.Fatal(without, err)
	}
	if slices.ContainsFunc(report.Diagnostics, func(d Diagnostic) bool { return d.Code == "resource_not_loaded" }) {
		t.Fatal("rules leaked into cached facts")
	}
	for _, rules := range [][]ResourceReferenceRule{{{}}, {assetRules()[0], assetRules()[0]}} {
		if _, err := NewBuilder("bad", Options{ResourceReferences: rules}); err == nil {
			t.Fatal("accepted invalid rules", rules)
		}
	}
}

func TestResourceReferenceAdmissionAndBudgets(t *testing.T) {
	docs := []Document{{Path: "src/view.ts", Content: []byte(`import {locate} from 'asset-kit';function render(){const content=locate(0,'template.html',import.meta.url);return content}`)}, {Path: "src/template.html", Content: []byte("hello")}}
	opts := assetOptions()
	opts.Admission = AnalysisAdmission()
	g, _, err := Build(context.Background(), "admission", docs, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(resourceUses(g)) != 0 {
		t.Fatal("Reference leaked through admission")
	}
	values := g.Find("src/view.ts", Variable, "render.content")
	if len(values) != 1 || !slices.ContainsFunc(g.RelationsFrom(values[0].ID, References), func(r Relation) bool { return r.Target == DocumentID("src/template.html") }) {
		t.Fatal("lost bridge", g.Nodes())
	}
	full, report, err := Build(context.Background(), "full", docs, assetOptions())
	if err != nil {
		t.Fatal(err)
	}
	evidence := 0
	for _, r := range full.Relations() {
		evidence += len(r.Evidence)
	}
	for name, options := range map[string]Options{"nodes": {MaxNodes: report.Nodes - 1}, "relations": {MaxRelations: report.Relations - 1}, "evidence": {MaxEvidence: evidence - 1}, "uses": {Analysis: AnalysisLimits{Uses: report.Analysis.Uses - 1}}} {
		t.Run(name, func(t *testing.T) {
			options.ResourceReferences = assetRules()
			g, _, err := Build(context.Background(), name, docs, options)
			if g != nil || !errors.Is(err, ErrBuildBudget) {
				t.Fatal(g, err)
			}
		})
	}
}

func TestResourceReferenceSharedExtractionCache(t *testing.T) {
	cache, err := NewExtractionCache(4, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	docs := []Document{{Path: "a.ts", Content: []byte(`import {locate} from 'asset-kit';locate(0,'./asset.txt',import.meta.url)`)}, {Path: "asset.txt", Content: []byte("hello")}}
	for _, enabled := range []bool{true, false, true} {
		opts := Options{ExtractionCache: cache}
		if enabled {
			opts.ResourceReferences = assetRules()
		}
		g, _, err := Build(context.Background(), "cached", docs, opts)
		if err != nil {
			t.Fatal(err)
		}
		got := len(resourceUses(g))
		if (enabled && got != 1) || (!enabled && got != 0) {
			t.Fatalf("enabled=%v references=%d", enabled, got)
		}
	}
}
