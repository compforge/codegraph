package codegraph

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func sourceReferences(g *Graph, kind RelationKind, name string) []Node {
	var out []Node
	for _, n := range g.Nodes() {
		if n.Kind == Reference && n.ReferenceKind == ReferenceKind(kind) && (name == "" || n.Name == name) {
			out = append(out, n)
		}
	}
	return out
}

// These expectations come from source syntax, not the extractor's Facts view.
func TestSourceUsesSurviveMissingTargets(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"app.go", "package p\nfunc entry(){ remote.Work(); remote.Work() }"},
		{"app.py", "def entry():\n    remote.Work()\n    remote.Work()\n"},
		{"app.js", "function entry(){ remote.Work(); remote.Work(); }"},
		{"app.ts", "function entry(){ remote.Work(); remote.Work(); }"},
		{"app.tsx", "function entry(){ remote.Work(); remote.Work(); }"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, report, err := Build(context.Background(), "partial", []Document{{Path: tc.path, Content: []byte(tc.source)}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(report.Documents, []string{tc.path}) {
				t.Fatal(report)
			}
			for _, kind := range []RelationKind{Calls, References} {
				uses := sourceReferences(g, kind, "Work")
				if len(uses) != 2 || uses[0].ID == uses[1].ID {
					t.Fatalf("%s: %+v", kind, uses)
				}
				for _, n := range uses {
					at := n.Location
					if at == nil || at.Path != tc.path || !strings.Contains(tc.source[at.StartByte:at.EndByte], "Work") || n.Receiver != "remote" {
						t.Fatal(n)
					}
					owner := g.RelationsFrom(n.ID, OccursIn)
					if len(owner) != 1 {
						t.Fatal(owner)
					}
					parent, ok := g.Node(owner[0].Target)
					if !ok || parent.Kind != Function || parent.Name != "entry" {
						t.Fatal(parent)
					}
					if len(g.RelationsFrom(n.ID, References)) != 0 {
						t.Fatal("invented an external target", n)
					}
					if len(g.RelationsTo(n.ID, Declares, Encloses, Contains)) != 0 {
						t.Fatal("use became a declaration", n)
					}
					rows := query(t, g, "MATCH (n {id:$id}) RETURN n, n.receiver AS receiver, n.referenceKind AS referenceKind", map[string]any{"id": n.ID})
					if len(rows) != 1 || rows[0]["receiver"] != "remote" || rows[0]["referenceKind"] != string(kind) || !reflect.DeepEqual(rows[0]["n"], n) {
						t.Fatal(rows)
					}
					// Returned source-use values are as detached as declaration nodes.
					n.Location.StartByte = -1
					original, _ := g.Node(n.ID)
					if original.Location.StartByte < 0 {
						t.Fatal("aliased location")
					}
				}
			}
			rows := query(t, g, "MATCH (use:Reference {referenceKind:'calls'})-[:occurs_in]->(owner) OPTIONAL MATCH (use)-[binding:references]->(target) RETURN use, owner, binding, target", nil)
			if len(rows) != 2 {
				t.Fatal("unbound sites disappeared from query", rows)
			}
			for _, row := range rows {
				if row["target"] != nil || row["binding"] != nil {
					t.Fatal(row)
				}
			}
			for _, n := range g.Find(tc.path, "", "") {
				if n.Kind == Reference {
					t.Fatal("Find includes a non-declaration", n)
				}
			}
			for _, r := range g.Relations() {
				if r.Kind == Calls || r.Kind == References {
					t.Fatal("unloaded target was fabricated", r)
				}
				if _, ok := g.Node(r.Source); !ok {
					t.Fatal(r)
				}
				if _, ok := g.Node(r.Target); !ok {
					t.Fatal(r)
				}
			}
			if len(report.Diagnostics) == 0 {
				t.Fatal("missing binding limitations")
			}
		})
	}
}

func TestSourceUseOwnershipAndBindingEvidence(t *testing.T) {
	g, _, err := Build(context.Background(), "uses", []Document{
		{Path: "a.go", Content: []byte("package p\ntype Box struct{}\nfunc (Box) Run(){}\nfunc target(){}\nfunc entry(b Box){ target(); b.Run() }")},
		{Path: "a.py", Content: []byte("def target():\n    pass\ndef outer():\n    def inner():\n        target()\n    return inner\ntarget()\n")},
		{Path: "a.ts", Content: []byte("class Box { run() {} }; function entry(x: any) { x.run(); }")},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var nested, topLevel bool
	for _, n := range sourceReferences(g, Calls, "target") {
		if n.Location.Path != "a.py" {
			continue
		}
		owner := g.RelationsFrom(n.ID, OccursIn)
		if len(owner) != 1 {
			t.Fatal(owner)
		}
		parent, _ := g.Node(owner[0].Target)
		nested = nested || parent.Name == "inner"
		topLevel = topLevel || parent.Kind == DocumentKind
	}
	if !nested || !topLevel {
		t.Fatal("lost nested or top-level use ownership", g.Nodes())
	}
	seen := map[Confidence]bool{}
	for _, r := range g.Relations() {
		if r.Kind != Calls && r.Kind != References {
			continue
		}
		source, _ := g.Node(r.Source)
		if source.Kind == Reference {
			continue
		}
		kind := r.Kind
		matched := 0
		for _, n := range sourceReferences(g, kind, "") {
			if *n.Location != r.Location {
				continue
			}
			owner := g.RelationsFrom(n.ID, OccursIn)
			if len(owner) != 1 || owner[0].Target != r.Source {
				continue
			}
			for _, binding := range g.RelationsFrom(n.ID, References) {
				if binding.Target != r.Target {
					continue
				}
				if binding.Confidence != r.Confidence || !reflect.DeepEqual(binding.Evidence, r.Evidence) || binding.Location != r.Location {
					t.Fatal("projection changed evidence", r, binding)
				}
				matched++
				seen[r.Confidence] = true
			}
		}
		if matched != 1 {
			t.Fatalf("binding has %d source uses: %+v", matched, r)
		}
	}
	if !seen[Exact] || !seen[Scoped] || !seen[NameOnly] {
		t.Fatal("fixture did not exercise evidence tiers", seen)
	}
}

// +case=Adding documents updates previous target sets and confidence without changing source-use identity or previous publications.
func TestSourceUsesRebindOnAddedDocuments(t *testing.T) {
	ctx := context.Background()
	b, err := NewBuilder("snapshot", Options{})
	if err != nil {
		t.Fatal(err)
	}
	add := func(path, source string) *Graph {
		t.Helper()
		if err := b.AddDocuments(ctx, Document{Path: path, Content: []byte(source)}); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		return b.Result()
	}
	first := add("entry.go", "package p\nfunc entry(){ Work() }")
	uses := sourceReferences(first, Calls, "Work")
	if len(uses) != 1 || len(first.RelationsFrom(uses[0].ID, References)) != 0 {
		t.Fatal(first.Nodes())
	}
	id := uses[0].ID
	second := add("work.go", "package p\nfunc Work(){}")
	secondNodes, secondEdges := second.Nodes(), second.Relations()
	bound := second.RelationsFrom(id, References)
	if len(bound) != 1 || bound[0].Confidence != Exact {
		t.Fatal(bound)
	}
	if n, ok := second.Node(id); !ok || !reflect.DeepEqual(n, uses[0]) {
		t.Fatal("source identity changed", n)
	}
	// Static analysis accepts incomplete or conflicting code. A second possible
	// declaration revises an existing exact binding rather than just appending.
	third := add("other.go", "package p\nfunc Work(){}")
	rebound := third.RelationsFrom(id, References)
	if len(rebound) != 2 {
		t.Fatal(rebound)
	}
	retainedID := false
	for _, r := range rebound {
		if r.Confidence != Scoped {
			t.Fatal("stale exact proof", r)
		}
		if r.ID == bound[0].ID {
			retainedID = true
		}
	}
	if !retainedID {
		t.Fatal("changed evidence renamed relation")
	}
	owner := third.RelationsFrom(id, OccursIn)[0].Target
	calls := third.RelationsFrom(owner, Calls)
	if len(calls) != 2 || calls[0].Confidence != Scoped || calls[1].Confidence != Scoped {
		t.Fatal(calls)
	}
	if len(first.RelationsFrom(id, References)) != 0 || !reflect.DeepEqual(second.Nodes(), secondNodes) || !reflect.DeepEqual(second.Relations(), secondEdges) {
		t.Fatal("supplementation mutated old publications")
	}
	// Material order and duplicate submission cannot alter published facts.
	reverse, _, err := Build(ctx, "snapshot", []Document{
		{Path: "other.go", Content: []byte("package p\nfunc Work(){}")},
		{Path: "work.go", Content: []byte("package p\nfunc Work(){}")},
		{Path: "entry.go", Content: []byte("package p\nfunc entry(){ Work() }")},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(third.Nodes(), reverse.Nodes()) || !reflect.DeepEqual(third.Relations(), reverse.Relations()) {
		t.Fatal("order-dependent graph")
	}
	again := add("entry.go", "package p\nfunc entry(){ Work() }")
	if !reflect.DeepEqual(again.Nodes(), third.Nodes()) || !reflect.DeepEqual(again.Relations(), third.Relations()) {
		t.Fatal("duplicate submission changed graph")
	}
}

func TestSourceUseBudgetsPreservePublication(t *testing.T) {
	ctx := context.Background()
	docs := []Document{
		{Path: "base.go", Content: []byte("package p\nfunc target(){}")},
		{Path: "entry.go", Content: []byte("package p\nfunc entry(){ target(); missing() }")},
	}
	complete, report, err := Build(ctx, "snapshot", docs, Options{})
	if err != nil {
		t.Fatal(err)
	}
	evidence := 0
	for _, r := range complete.Relations() {
		evidence += len(r.Evidence)
	}
	for name, opts := range map[string]Options{
		"nodes":     {MaxNodes: report.Nodes - 1},
		"relations": {MaxRelations: report.Relations - 1},
		"evidence":  {MaxEvidence: evidence - 1},
	} {
		t.Run(name, func(t *testing.T) {
			b, err := NewBuilder("snapshot", opts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.addDocumentsSync(ctx, docs[0]); err != nil {
				t.Fatal(err)
			}
			previous := b.Result()
			if _, err := b.addDocumentsSync(ctx, docs[1]); !errors.Is(err, ErrBuildBudget) {
				t.Fatal("source uses escaped budget", err)
			}
			if b.Result() != previous {
				t.Fatal("failed build replaced publication")
			}
		})
	}
}

func TestSourceUseCapabilities(t *testing.T) {
	for _, c := range Capabilities() {
		if !slices.Contains(c.References, CallReference) || !slices.Contains(c.References, SymbolReference) || !slices.Contains(c.Relations, OccursIn) || !slices.Contains(c.Relations, References) {
			t.Fatal(c)
		}
		if !slices.Contains(c.SourceItems, Import) {
			t.Fatal(c)
		}
		if (c.Language == "javascript" || c.Language == "typescript" || c.Language == "tsx") != slices.Contains(c.SourceItems, Export) {
			t.Fatal(c)
		}
		if slices.Contains(c.Declarations, Reference) {
			t.Fatal(c)
		}
	}
	for _, c := range Capabilities("rust", "java") {
		if len(c.References) != 0 || slices.Contains(c.Relations, References) {
			t.Fatal("grammar support advertised semantic use extraction", c)
		}
	}
}
