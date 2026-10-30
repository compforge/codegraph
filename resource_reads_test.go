package codegraph

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// +case:id=resource_read_binding,desc=`Node fs aliases resolve literal module-relative reads`,expect=`Both occurrence and owner reach the supplied Document with evidence`
func TestResourceReadBindings(t *testing.T) {
	for _, ext := range []string{"js", "ts", "tsx"} {
		for _, tc := range []struct{ name, imports, reader, url string }{
			{"named", `import {readFileSync} from "node:fs";`, "readFileSync", "URL"},
			{"alias", `import {readFileSync as read} from "fs"; import {URL as FileURL} from "node:url";`, "read", "FileURL"},
			{"namespace", `import * as fs from "node:fs";`, "fs.readFileSync", "URL"},
			{"default", `import fs from "fs";`, "fs.readFileSync", "URL"},
		} {
			t.Run(ext+"/"+tc.name, func(t *testing.T) {
				source := tc.imports + ` function render(){ return ` + tc.reader + `(new ` + tc.url + `('./template.html', import.meta.url), 'utf8'); }`
				docs := []Document{{Path: "src/view." + ext, Content: []byte(source)}, {Path: "src/template.html", Content: []byte("<p>hello</p>")}}
				g, _, err := Build(context.Background(), "reads", docs, Options{})
				if err != nil {
					t.Fatal(err)
				}
				uses := sourceReferences(g, Reads, "")
				if len(uses) != 1 {
					t.Fatalf("reads = %+v; report = %+v", uses, g.Report())
				}
				use := uses[0]
				if use.Resource == nil || use.Resource.Path != "src/template.html" || !strings.HasPrefix(use.Resource.Expression, "new ") {
					t.Fatal(use)
				}
				if use.Location == nil || !strings.HasPrefix(source[use.Location.StartByte:use.Location.EndByte], tc.reader+"(") {
					t.Fatal(use)
				}
				for _, id := range []string{use.ID, g.Find("src/view."+ext, Function, "render")[0].ID} {
					edges := g.RelationsFrom(id, Reads)
					if len(edges) != 1 || edges[0].Target != DocumentID("src/template.html") || edges[0].Confidence != Exact || len(edges[0].Evidence) == 0 {
						t.Fatal(edges)
					}
				}
				rows := query(t, g, `MATCH (r:Reference {referenceKind:'reads'})-[:reads]->(d:Document) RETURN r.resourcePath AS path, d.path AS target`, nil)
				if len(rows) != 1 || rows[0]["path"] != "src/template.html" || rows[0]["target"] != "src/template.html" {
					t.Fatal(rows)
				}
				use.Resource.Path = "mutated"
				copy, _ := g.Node(use.ID)
				if copy.Resource.Path != "src/template.html" {
					t.Fatal("resource aliases graph storage")
				}
			})
		}
	}
}

// +case:id=resource_read_rebinding,desc=`A recognized read precedes its resource document`,expect=`Supplementing materials binds the same occurrence without mutating the old graph`
func TestResourceReadSupplement(t *testing.T) {
	ctx := context.Background()
	doc := Document{Path: "src/view.ts", Content: []byte(`import {readFileSync as read} from "node:fs"; function render(){return read(new URL("../template.html", import.meta.url));}`)}
	b, err := NewBuilder("snapshot", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = b.AddDocuments(ctx, doc); err != nil {
		t.Fatal(err)
	}
	report, err := b.Wait(ctx)
	before := b.Result()
	if err != nil {
		t.Fatal(err)
	}
	uses := sourceReferences(before, Reads, "")
	if len(uses) != 1 || uses[0].Resource.Path != "template.html" || len(before.RelationsFrom(uses[0].ID, Reads)) != 0 {
		t.Fatal(uses)
	}
	if !slices.ContainsFunc(report.Diagnostics, func(d Diagnostic) bool { return d.Code == "resource_not_loaded" && d.Relation == Reads }) {
		t.Fatal(report.Diagnostics)
	}
	if _, ok := before.Node(DocumentID("template.html")); ok {
		t.Fatal("fabricated target")
	}
	if err = b.AddDocuments(ctx, Document{Path: "template.html", Content: []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	report, err = b.Wait(ctx)
	after := b.Result()
	if err != nil {
		t.Fatal(err)
	}
	updated := sourceReferences(after, Reads, "")
	if len(updated) != 1 || updated[0].ID != uses[0].ID || len(after.RelationsFrom(updated[0].ID, Reads)) != 1 || len(before.RelationsFrom(uses[0].ID, Reads)) != 0 {
		t.Fatal("unstable publication", updated)
	}
	if slices.ContainsFunc(report.Diagnostics, func(d Diagnostic) bool { return d.Code == "resource_not_loaded" }) {
		t.Fatal(report.Diagnostics)
	}
	// Facts is a detached discovery view and remains usable without the extractor.
	e := newTestExtractor(t, nil)
	facts := extractTestFacts(t, e, doc.Path, string(doc.Content))
	if len(facts.ResourceReads) != 1 || facts.ResourceReads[0].Path != "template.html" {
		t.Fatal(facts.ResourceReads)
	}
	facts.ResourceReads[0].Path = "wrong"
	view, err := facts.View()
	if err != nil || view.ResourceReads[0].Path != "template.html" {
		t.Fatal(view, err)
	}
}

func TestResourceReadsRespectBindingsAndUnknownPaths(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		reads        int
		target       string
	}{
		{"user function", `function readFileSync(x){}; readFileSync(new URL('./template.html',import.meta.url));`, 0, ""},
		{"wrong module", `import {readFileSync} from './my-fs'; readFileSync(new URL('./template.html',import.meta.url));`, 0, ""},
		{"parameter shadow", `import {readFileSync} from 'node:fs'; function f(readFileSync){readFileSync(new URL('./template.html',import.meta.url));}`, 0, ""},
		{"namespace shadow", `import * as fs from 'node:fs'; function f(fs){fs.readFileSync(new URL('./template.html',import.meta.url));}`, 0, ""},
		{"type import", `import type {readFileSync} from 'node:fs'; readFileSync(new URL('./template.html',import.meta.url));`, 0, ""},
		{"URL shadow", `import {readFileSync as read} from 'node:fs'; function f(URL){read(new URL('./template.html',import.meta.url));}`, 1, ""},
		{"cwd relative", `import {readFileSync as read} from 'node:fs'; read('./template.html');`, 1, ""},
		{"dynamic", `import {readFileSync as read} from 'node:fs'; read(new URL(name,import.meta.url));`, 1, ""},
		{"escape root", `import {readFileSync as read} from 'node:fs'; read(new URL('../../template.html',import.meta.url));`, 1, ""},
		{"absolute URL", `import {readFileSync as read} from 'node:fs'; read(new URL('https://example.com/template.html',import.meta.url));`, 1, ""},
		{"escaped literal", `import {readFileSync as read} from 'node:fs'; read(new URL('./tem\u0070late.html',import.meta.url));`, 1, ""},
		{"directory URL", `import {readFileSync as read} from 'node:fs'; read(new URL('./template.html/',import.meta.url));`, 1, ""},
		{"spread", `import {readFileSync as read} from 'node:fs'; read(...paths);`, 1, ""},
		{"path text only", `const text='./template.html'; const u=new URL('./template.html',import.meta.url);`, 0, ""},
		{"non-normalized path", `import {readFileSync as read} from 'node:fs'; read(new URL('./sub/../template.html',import.meta.url));`, 1, "src/template.html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, report, err := Build(context.Background(), "bindings", []Document{{Path: "src/view.ts", Content: []byte(tc.source)}, {Path: "src/template.html", Content: []byte("hello")}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			uses := sourceReferences(g, Reads, "")
			if len(uses) != tc.reads {
				t.Fatalf("reads=%+v diagnostics=%+v", uses, report.Diagnostics)
			}
			for _, use := range uses {
				if use.Resource.Path != tc.target {
					t.Fatal(use)
				}
				if tc.target == "" && (len(g.RelationsFrom(use.ID, Reads)) != 0 || !slices.ContainsFunc(report.Diagnostics, func(d Diagnostic) bool { return d.Code == "unresolved_resource" })) {
					t.Fatal("invented target or lost gap", report)
				}
			}
		})
	}
}

func TestResourceReadAdmissionAndBudgets(t *testing.T) {
	ctx := context.Background()
	docs := []Document{{Path: "src/view.ts", Content: []byte(`import {readFileSync as read} from 'node:fs'; function render(){return read(new URL('./template.html',import.meta.url));}`)}, {Path: "src/template.html", Content: []byte("hello")}}
	g, _, err := Build(ctx, "admission", docs, Options{Admission: AnalysisAdmission()})
	if err != nil {
		t.Fatal(err)
	}
	if len(sourceReferences(g, Reads, "")) != 0 {
		t.Fatal("admission leaked Reference")
	}
	owner := g.Find("src/view.ts", Function, "render")
	if len(owner) != 1 || len(g.RelationsFrom(owner[0].ID, Reads)) != 1 {
		t.Fatal("lost semantic read", g.Relations())
	}
	full, report, err := Build(ctx, "full", docs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	evidence := 0
	for _, edge := range full.Relations() {
		evidence += len(edge.Evidence)
	}
	for name, opts := range map[string]Options{"nodes": {MaxNodes: report.Nodes - 1}, "relations": {MaxRelations: report.Relations - 1}, "evidence": {MaxEvidence: evidence - 1}, "uses": {Analysis: AnalysisLimits{Uses: report.Analysis.Uses - 1}}} {
		t.Run(name, func(t *testing.T) {
			g, _, err := Build(ctx, fmt.Sprint(name), docs, opts)
			if !errors.Is(err, ErrBuildBudget) || g != nil {
				t.Fatal(g, err)
			}
		})
	}
}

func TestResourceReadLocalInitializerSurvivesAdmission(t *testing.T) {
	docs := []Document{{Path: "src/view.ts", Content: []byte(`import {readFileSync as read} from 'node:fs'; function render(){const content=read(new URL('template.html',import.meta.url)); return content;}`)}, {Path: "src/template.html", Content: []byte("hello")}}
	g, _, err := Build(context.Background(), "bridge", docs, Options{Admission: AnalysisAdmission()})
	if err != nil {
		t.Fatal(err)
	}
	content := g.Find("src/view.ts", Variable, "render.content")
	if len(content) != 1 {
		t.Fatal("lost local read bridge", g.Nodes())
	}
	edges := g.RelationsFrom(content[0].ID, Reads)
	if len(edges) != 1 || edges[0].Target != DocumentID("src/template.html") {
		t.Fatal(edges)
	}
}
