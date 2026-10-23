package codegraph

import (
	"context"
	"github.com/compforge/codegraph/internal/analysis"
	"reflect"
	"slices"
	"sync"
	"testing"
)

// +case:id=admission_binding,desc=`Imports and local bindings resolve before graph projection`,expect=`Retained callers still reach retained targets`
func TestAnalysisAdmissionBindings(t *testing.T) {
	cases := []struct {
		name           string
		docs           []Document
		caller, target string
	}{
		{"go", []Document{{Path: "app.go", Content: []byte("package app\ntype Service struct { value int }\nvar Global = 1\nfunc (s *Service) Run() {}\nfunc Handle() { svc := &Service{}; svc.Run() }\n")}}, "Handle", "Run"},
		{"python", []Document{{Path: "target.py", Content: []byte("def run():\n    pass\n")}, {Path: "app.py", Content: []byte("from target import run as execute\nGLOBAL = 1\ndef handle():\n    local = GLOBAL\n    execute()\n")}}, "handle", "run"},
		{"typescript", []Document{{Path: "target.ts", Content: []byte("export function run() { return 1; }\n")}, {Path: "app.ts", Content: []byte("import { run as execute } from './target';\nexport const GLOBAL = 1;\nexport function handle() { const local = execute(); }\n")}}, "handle", "run"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full, _, err := Build(context.Background(), tc.name, tc.docs, Options{})
			if err != nil {
				t.Fatal(err)
			}
			g, report, err := Build(context.Background(), tc.name, tc.docs, Options{Admission: AnalysisAdmission()})
			if err != nil {
				t.Fatal(err)
			}
			if len(g.Nodes()) >= len(full.Nodes()) {
				t.Fatal("projection did not reduce nodes")
			}
			selected := map[string]bool{}
			var caller, target string
			for _, n := range g.Nodes() {
				selected[n.ID] = true
				switch n.Kind {
				case Reference, Import, Export:
					t.Fatalf("unwanted node: %+v", n)
				}
				if n.Name == tc.caller {
					caller = n.ID
				}
				if n.Name == tc.target {
					target = n.ID
				}
			}
			found := false
			for _, r := range g.Relations() {
				if !selected[r.Source] || !selected[r.Target] {
					t.Fatalf("dangling edge: %+v", r)
				}
				if r.Kind == Calls && r.Source == caller && r.Target == target {
					found = true
				}
			}
			if caller == "" || target == "" || (!found && !admissionReaches(g, caller, target)) {
				t.Fatalf("call missing: caller=%s target=%s edges=%+v", caller, target, g.Relations())
			}
			for _, r := range full.Relations() {
				if selected[r.Source] && selected[r.Target] {
					var got Relation
					var ok bool
					for _, candidate := range g.Relations() {
						if candidate.ID == r.ID {
							got, ok = candidate, true
						}
					}
					if !ok || !reflect.DeepEqual(got, r) {
						t.Fatalf("original evidence lost: %+v", r)
					}
				}
			}
			if report.Nodes != len(g.Nodes()) || report.Relations != len(g.Relations()) {
				t.Fatal(report)
			}
		})
	}
}

// +case:id=material_admission,desc=`Directory and document classification precede parser submission`,forbid=`Parsing excluded descendants or ordinary resources`
func TestAnalysisMaterialAdmission(t *testing.T) {
	var mu sync.Mutex
	var parsed []string
	parseObserver = func(name string) { mu.Lock(); defer mu.Unlock(); parsed = append(parsed, name) }
	defer func() { parseObserver = nil }()
	rules := append(BuiltinTagRules(), TagRule{Name: DependencyTag, Pattern: `^external$`})
	docs := []Document{
		{Path: "app.go", Content: []byte("package app\nfunc Run() {}\n")},
		{Path: "external/deep/broken.go", Content: []byte("invalid go")},
		{Path: "nested/.codex/helper.ts", Content: []byte("invalid typescript")},
		{Path: "AGENTS.md", Content: []byte("# Agent instructions")},
		{Path: "nested/CLAUDE.md", Content: []byte("# Agent instructions")},
		{Path: "nested/GEMINI.md", Content: []byte("# Agent instructions")},
		{Path: ".editorconfig", Content: []byte("invalid editor configuration")},
		{Path: ".dockerignore", Content: []byte("build/")},
		{Path: ".gitignore", Content: []byte("build/")},
		{Path: ".env.example", Content: []byte("APP_MODE=example")},
		{Path: "README.md", Content: []byte("# Guide\n")},
		{Path: "assets/data.json", Content: []byte("invalid json")},
		{Path: "go.mod", Content: []byte("module example.com/app\ngo 1.26\n")},
	}
	g, report, err := Build(context.Background(), "material", docs, Options{Admission: AnalysisAdmission(), TagRules: rules})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(parsed)
	if !reflect.DeepEqual(parsed, []string{"app.go", "go.mod"}) {
		t.Fatal(parsed)
	}
	for _, name := range []string{"external/deep/broken.go", "nested/.codex/helper.ts", "AGENTS.md", "nested/CLAUDE.md", "nested/GEMINI.md"} {
		if _, ok := g.Document(name); ok {
			t.Fatal("excluded document", name)
		}
		if slices.Contains(report.Documents, name) {
			t.Fatal(report)
		}
	}
	for _, name := range []string{"README.md", "assets/data.json", "go.mod", ".dockerignore", ".gitignore", ".env.example", ".editorconfig"} {
		if _, ok := g.Document(name); !ok {
			t.Fatal("missing context", name)
		}
	}
	if d, _ := g.Document("go.mod"); d.Manifest == nil || d.Manifest.Name != "example.com/app" {
		t.Fatal(d)
	}
	if len(report.Diagnostics) != 5 {
		t.Fatal(report.Diagnostics)
	}
	for _, d := range report.Diagnostics {
		if d.Code != "excluded_material" {
			t.Fatal(d)
		}
	}
}

func TestAdmissionExternalFactsAndPolicyIsolation(t *testing.T) {
	ctx := context.Background()
	cache, err := NewExtractionCache(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewExtractor(ExtractionOptions{Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	f, err := e.Extract(ctx, Document{Path: "vendor/a.ts", Content: []byte("export function run() {}")})
	if err != nil {
		t.Fatal(err)
	}
	policy := AnalysisAdmission()
	b, err := NewBuilder("filtered", Options{Admission: policy, ExtractionCache: cache})
	if err != nil {
		t.Fatal(err)
	}
	policy.ExcludedTags[0] = "changed"
	policy.NodeKinds[0] = Reference
	if err = b.Add(f); err != nil {
		t.Fatal(err)
	}
	g, _, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes()) != 0 {
		t.Fatal(g.Nodes())
	}
	all, err := NewBuilder("all", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = all.Add(f); err != nil {
		t.Fatal(err)
	}
	g, _, err = all.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Find("vendor/a.ts", Function, "run")) != 1 {
		t.Fatal(g.Nodes())
	}
}

func TestAdmissionNodeKindsAndScopes(t *testing.T) {
	doc := Document{Path: "app.ts", Content: []byte(`
export const GLOBAL = 1;
export const arrow = () => { const inside = 1; return inside; };
function outer() { const local = 1; const callback = () => GLOBAL; function nested() { return GLOBAL; } }
namespace Space { export const value = GLOBAL; }
{ let block = 1; }
class Box { field = 1; method() { let localMethod = 1; } }
`)}
	g, _, err := Build(context.Background(), "scopes", []Document{doc}, Options{Admission: AnalysisAdmission()})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes() {
		if slices.Contains([]string{"inside", "local", "block", "field", "localMethod"}, n.Name) {
			t.Fatalf("unwanted declaration %+v", n)
		}
	}
	for _, name := range []string{"GLOBAL", "arrow", "outer", "callback", "nested", "value", "Box", "method"} {
		found := false
		for _, n := range g.Nodes() {
			if n.Name == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %s: %+v", name, g.Nodes())
		}
	}
	p := &AdmissionPolicy{NodeKinds: []NodeKind{Function}}
	only, _, err := Build(context.Background(), "kinds", []Document{doc}, Options{Admission: p})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range only.Nodes() {
		if n.Kind != Function {
			t.Fatal(n)
		}
	}
	bridges := &AdmissionPolicy{NodeKinds: []NodeKind{Function}, IncludeDependencyBridges: true}
	withBridge, _, err := Build(context.Background(), "custom-bridge", []Document{{Path: "app.ts", Content: []byte("function target(){} function caller(){const local=target();}")}}, Options{Admission: bridges})
	if err != nil {
		t.Fatal(err)
	}
	if len(withBridge.Find("app.ts", Variable, "caller.local")) != 1 {
		t.Fatal("custom policy lost local bridge")
	}

	empty, _, err := Build(context.Background(), "empty", []Document{doc}, Options{Admission: &AdmissionPolicy{NodeKinds: []NodeKind{}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Nodes()) != 0 || len(empty.Relations()) != 0 {
		t.Fatal(empty.Nodes())
	}
}

// +case:id=admission_rebuild,desc=`Supplementing material rebinds complete facts before projection`,expect=`Old graph immutable and local facts retained`
func TestAdmissionSupplementAndFacts(t *testing.T) {
	ctx := context.Background()
	b, err := NewBuilder("supplement", Options{Admission: AnalysisAdmission()})
	if err != nil {
		t.Fatal(err)
	}
	task := b.AddDocument(ctx, Document{Path: "app.ts", Content: []byte("import {run} from './target'; export function handle(){const local = run();}")})
	facts, err := task.Wait()
	if err != nil {
		t.Fatal(err)
	}
	foundLocal := false
	for _, d := range facts.Declarations {
		if d.Name == "local" {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatal("graph policy stripped extraction facts")
	}
	if _, err = b.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	old := b.Result()
	oldNodes, oldEdges := old.Nodes(), old.Relations()
	if err = b.AddDocuments(ctx, Document{Path: "target.ts", Content: []byte("export function run(){return 1;}")}); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	g := b.Result()
	caller := g.Find("app.ts", Function, "handle")
	target := g.Find("target.ts", Function, "run")
	if len(caller) != 1 || len(target) != 1 {
		t.Fatal(g.Nodes())
	}
	if !admissionReaches(g, caller[0].ID, target[0].ID) {
		t.Fatal("missing rebound dependency path")
	}
	locals := g.Find("app.ts", Variable, "handle.local")
	if len(locals) != 1 {
		t.Fatal("missing bridge", g.Nodes())
	}
	if len(old.Find("app.ts", Variable, "handle.local")) != 0 {
		t.Fatal("unresolved dependency must not admit a bridge")
	}
	if !reflect.DeepEqual(oldNodes, old.Nodes()) || !reflect.DeepEqual(oldEdges, old.Relations()) {
		t.Fatal("old graph changed")
	}
}

func TestAdmissionFieldDependency(t *testing.T) {
	g, _, err := Build(context.Background(), "field", []Document{{Path: "app.go", Content: []byte("package app\ntype Box struct { Value int }\nfunc Read(b Box) int { return b.Value }\n")}}, Options{Admission: AnalysisAdmission()})
	if err != nil {
		t.Fatal(err)
	}
	caller := g.Find("app.go", Function, "Read")
	owner := g.Find("app.go", Struct, "Box")
	if len(caller) != 1 || len(owner) != 1 {
		t.Fatal(g.Nodes())
	}
	found := false
	for _, r := range g.RelationsFrom(caller[0].ID, References) {
		if r.Target == owner[0].ID {
			found = true
		}
	}
	if !found {
		t.Fatal("field removal erased owning type dependency")
	}
}

// +case:id=admission_bridges,desc=`Select dependency corridors between distinct code entities`,forbid=`Structural-only and dangling expansion or invented edges`
func TestAdmissionDependencyBridges(t *testing.T) {
	nodes := map[string]Node{}
	for _, id := range []string{"a", "b", "c"} {
		nodes[id] = Node{ID: id, Kind: Function}
	}
	for _, id := range []string{"local", "next", "unused", "self", "tail"} {
		nodes[id] = Node{ID: id, Kind: Variable}
	}
	nodes["field"] = Node{ID: "field", Kind: Field}
	nodes["doc"] = Node{ID: "doc", Kind: DocumentNodeKind}
	nodes["ref"] = Node{ID: "ref", Kind: Reference}
	selected := map[string]Node{"a": nodes["a"], "b": nodes["b"], "c": nodes["c"], "doc": nodes["doc"]}
	local := map[string]bool{"local": true, "next": true, "unused": true, "self": true, "tail": true}
	relations := map[string]Relation{}
	add := func(source, target string, kind RelationKind) {
		id := source + "/" + target
		relations[id] = Relation{ID: id, Source: source, Target: target, Kind: kind, Confidence: Heuristic, Evidence: []Evidence{{Basis: "original", Confidence: Heuristic}}}
	}
	add("a", "local", Encloses)
	add("local", "next", References)
	add("next", "local", References) // Cycles terminate.
	add("next", "b", Calls)
	add("a", "unused", Encloses)
	add("unused", "tail", References) // No core destination.
	add("doc", "tail", Declares)      // Context cannot seed a bridge.
	add("a", "self", Encloses)
	add("self", "a", References) // Same core at both ends is insufficient.
	add("a", "field", References)
	add("field", "c", References)
	add("a", "ref", References)
	add("ref", "b", References) // Use-site occurrences are not candidates.
	original := make(map[string]Relation)
	for id, r := range relations {
		original[id] = r
	}
	if err := selectDependencyBridges(context.Background(), nodes, relations, selected, local); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"local", "next", "field"} {
		if _, ok := selected[id]; !ok {
			t.Fatal("missing bridge", id)
		}
	}
	for _, id := range []string{"unused", "self", "tail", "ref"} {
		if _, ok := selected[id]; ok {
			t.Fatal("unwanted bridge", id)
		}
	}
	if !reflect.DeepEqual(original, relations) {
		t.Fatal("projection changed original relations")
	}
}

func admissionReaches(g *Graph, source, target string) bool {
	queue, seen := []string{source}, map[string]bool{source: true}
	for i := 0; i < len(queue); i++ {
		for _, r := range g.RelationsFrom(queue[i]) {
			switch r.Kind {
			case Encloses, Contains, Calls, References:
			default:
				continue
			}
			if r.Target == target {
				return true
			}
			if !seen[r.Target] {
				seen[r.Target] = true
				queue = append(queue, r.Target)
			}
		}
	}
	return false
}

// +case:id=admission_order,desc=`Reversing or batching AddDocument leaves the final graph unchanged`,expect=`Stable nodes relations and evidence after rebinding`
func TestAdmissionDocumentOrder(t *testing.T) {
	docs := []Document{
		{Path: "app.ts", Content: []byte("import {run} from './target'; export function handle(){const local=run();}")},
		{Path: "target.ts", Content: []byte("export function run(){return 1;}")},
		{Path: "other.ts", Content: []byte("export function run(){return 2;}")},
	}
	ctx := context.Background()
	want, _, err := Build(ctx, "order", docs, Options{Admission: AnalysisAdmission()})
	if err != nil {
		t.Fatal(err)
	}
	for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		b, err := NewBuilder("order", Options{Admission: AnalysisAdmission()})
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range order {
			if _, err := b.AddDocument(ctx, docs[i]).Wait(); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Wait(ctx); err != nil {
				t.Fatal(err)
			}
		}
		got := b.Result()
		if !reflect.DeepEqual(want.Nodes(), got.Nodes()) || !reflect.DeepEqual(want.Relations(), got.Relations()) {
			t.Fatalf("order %v changed final graph", order)
		}
	}
}

// +case:id=parse_tags,desc=`Material tags control parsing and exclusion`,expect=`Classification survives skipped parsing and manifests retain metadata`
func TestAdmissionParseTags(t *testing.T) {
	docs := []Document{{Path: "app.go", Content: []byte("package app\nfunc Run() {}\n")}, {Path: "go.mod", Content: []byte("module example.org/app\ngo 1.26\n")}}
	g, _, err := Build(context.Background(), "tags", docs, Options{Admission: &AdmissionPolicy{ParseTags: []Tag{}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Find("app.go", Function, "Run")) != 0 {
		t.Fatal("parsed without a matching tag")
	}
	d, _ := g.Document("app.go")
	if d.Kind != DocumentNodeKind || !slices.Contains(d.Tags, SourceTag) {
		t.Fatal(d)
	}
	m, _ := g.Document("go.mod")
	if m.Manifest == nil || m.Manifest.Name != "example.org/app" {
		t.Fatal(m)
	}
	g, _, err = Build(context.Background(), "exclude-source", docs, Options{Admission: &AdmissionPolicy{ExcludedTags: []Tag{SourceTag}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := g.Document("app.go"); ok {
		t.Fatal("excluded material tag admitted")
	}
}

func selectDependencyBridges(ctx context.Context, nodes map[string]Node, relations map[string]Relation, selected map[string]Node, local map[string]bool) error {
	index := analysis.NewIndex(nil)
	selector := entitySelector{index: index, selected: selection{}, local: map[analysis.Ref]bool{}, limit: 100000}
	for id, n := range nodes {
		ref := analysis.SyntheticRef(id)
		index.RegisterEntities(analysis.Organization{Entities: []analysis.Entity{{Ref: ref, Kind: string(n.Kind)}}})
		if _, ok := selected[id]; ok {
			selector.selected[ref] = coreSelection
		}
		selector.local[ref] = local[id]
	}
	for _, e := range relations {
		index.Add(analysis.Edge{Source: analysis.SyntheticRef(e.Source), Target: analysis.SyntheticRef(e.Target), Kind: string(e.Kind)})
	}
	if err := selector.bridges(ctx); err != nil {
		return err
	}
	for id, n := range nodes {
		if _, ok := selector.selected[analysis.SyntheticRef(id)]; ok {
			selected[id] = n
		}
	}
	return nil
}
