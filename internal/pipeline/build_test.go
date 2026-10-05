package pipeline

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/compforge/codegraph/internal/analysis"
)

type organizerFunc func(context.Context, analysis.BuildScope) (analysis.Organization, error)

func (f organizerFunc) Organize(c context.Context, s analysis.BuildScope) (analysis.Organization, error) {
	return f(c, s)
}

type binderFunc func(context.Context, analysis.BuildScope, *analysis.Index, int) (analysis.BindResult, error)

func (f binderFunc) Bind(c context.Context, s analysis.BuildScope, i *analysis.Index, n int) (analysis.BindResult, error) {
	return f(c, s, i, n)
}

type resolverFunc func(context.Context, *analysis.Index, int) ([]analysis.Edge, []analysis.Gap, error)

func (f resolverFunc) Resolve(c context.Context, i *analysis.Index, n int) ([]analysis.Edge, []analysis.Gap, error) {
	return f(c, i, n)
}

// A synthetic language participates through the same contracts as built-ins.
// Its out-of-line member requires Bind; its call requires both languages' bindings.
func TestLanguageStagesAndSharedMembership(t *testing.T) {
	files := map[string]analysis.Facts{
		"a.probe": {Path: "a.probe", Language: "alpha", Declarations: []analysis.Declaration{
			{Name: "N", Kind: "namespace", Parent: -1, Span: analysis.Span{End: 30}}, {Name: "C", Kind: "class", Parent: 0, Span: analysis.Span{Start: 2, End: 20}},
		}},
		"b.probe": {Path: "b.probe", Language: "beta", Declarations: []analysis.Declaration{
			{Name: "run", Kind: "method", Parent: -1, Receiver: "C"},
		}},
	}
	owner, member := analysis.DeclarationRef("a.probe", 1), analysis.DeclarationRef("b.probe", 0)
	var events []string
	lookup := func(name string) analysis.Adapter {
		return analysis.Adapter{
			Organizer: organizerFunc(func(ctx context.Context, s analysis.BuildScope) (analysis.Organization, error) {
				events = append(events, "organize:"+name)
				p := s.Names[0]
				key := "package:" + name
				return analysis.Organization{Entities: []analysis.Entity{{Ref: analysis.SyntheticRef(key), Kind: "Package", Name: name, Language: name}}, Roots: map[string]analysis.Ref{p: analysis.SyntheticRef(key)}, Edges: []analysis.Edge{{Source: analysis.DocumentRef(p), Target: analysis.SyntheticRef(key), Kind: "declares", Path: p}}}, nil
			}),
			Binder: binderFunc(func(ctx context.Context, s analysis.BuildScope, index *analysis.Index, limit int) (analysis.BindResult, error) {
				events = append(events, "bind:"+name)
				if len(index.Roots) != 2 {
					t.Fatal("bind ran before all organizations were available")
				}
				result := analysis.BindResult{Resolver: resolverFunc(func(ctx context.Context, index *analysis.Index, limit int) ([]analysis.Edge, []analysis.Gap, error) {
					events = append(events, "resolve:"+name)
					if got := index.Namespace(owner).Members("run"); !reflect.DeepEqual(got, []analysis.Ref{member}) {
						t.Fatalf("receiver member unavailable: %v", got)
					}
					if name == "alpha" {
						return []analysis.Edge{{Source: owner, Target: member, Kind: "calls", Path: "a.probe", Confidence: "scoped", Basis: "probe_binding"}}, nil, nil
					}
					return nil, nil, nil
				})}
				if name == "beta" {
					result.Edges = []analysis.Edge{{Source: owner, Target: member, Kind: "contains", Path: "b.probe", Confidence: "exact", Basis: "receiver_declaration"}}
				}
				return result, nil
			}),
		}
	}
	index, _, err := Build(context.Background(), analysis.NewBuildScope(files, ""), 20, 20, 100, lookup)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"organize:alpha", "organize:beta", "bind:alpha", "bind:beta", "resolve:alpha", "resolve:beta"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("phase barriers: %v", events)
	}
	ns := analysis.DeclarationRef("a.probe", 0)
	if got := index.Namespace(analysis.SyntheticRef("package:alpha")).Members("N"); !reflect.DeepEqual(got, []analysis.Ref{ns}) {
		t.Fatalf("declared namespace membership: %v", got)
	}
	if got := index.Namespace(ns).Members("C"); !reflect.DeepEqual(got, []analysis.Ref{owner}) {
		t.Fatalf("class membership: %v", got)
	}
	if len(index.Namespace(analysis.DocumentRef("a.probe")).Members("N")) != 0 {
		t.Fatal("source ownership became semantic containment")
	}
	methods := analysis.NewMethodIndex(index)
	found, err := methods.Lookup(context.Background(), []analysis.BindingTarget{{Ref: owner, Confidence: analysis.Exact}}, "run", func(analysis.Ref) bool { return true }, 2)
	if err != nil || len(found) != 1 || found[0].Ref != member {
		t.Fatalf("method lookup did not use shared membership: %v %v", found, err)
	}
}

func TestStageFailureAndBudgetReturnNoIndex(t *testing.T) {
	failure := errors.New("adapter failed")
	files := map[string]analysis.Facts{"x": {Path: "x", Language: "probe", Declarations: []analysis.Declaration{{Name: "f", Kind: "function", Parent: -1}}}}
	for _, phase := range []string{"organize", "bind", "resolve", "cancel", "nodes", "edges"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			nodes, edges := 10, 10
			expected := failure
			if phase == "nodes" {
				nodes = 1
				expected = ErrNodeLimit
			}
			if phase == "edges" {
				edges = 0
				expected = analysis.ErrEdgeLimit
			}
			if phase == "cancel" {
				cancel()
				expected = context.Canceled
			}
			lookup := func(string) analysis.Adapter {
				return analysis.Adapter{
					Organizer: organizerFunc(func(context.Context, analysis.BuildScope) (analysis.Organization, error) {
						if phase == "organize" {
							return analysis.Organization{}, failure
						}
						return analysis.Organization{}, nil
					}),
					Binder: binderFunc(func(context.Context, analysis.BuildScope, *analysis.Index, int) (analysis.BindResult, error) {
						if phase == "bind" {
							return analysis.BindResult{}, failure
						}
						return analysis.BindResult{Resolver: resolverFunc(func(context.Context, *analysis.Index, int) ([]analysis.Edge, []analysis.Gap, error) {
							if phase == "resolve" {
								return nil, nil, failure
							}
							return nil, nil, nil
						})}, nil
					}),
				}
			}
			index, _, err := Build(ctx, analysis.NewBuildScope(files, ""), nodes, edges, 100, lookup)
			if index != nil || !errors.Is(err, expected) {
				t.Fatalf("index=%v error=%v want=%v", index, err, expected)
			}
		})
	}
}

// +case=`A declared namespace may be the semantic root; its members can be registered by a later language batch`
func TestDeclaredNamespaceRootAndLaterOrganization(t *testing.T) {
	files := map[string]analysis.Facts{
		"a.probe": {Path: "a.probe", Language: "alpha", Declarations: []analysis.Declaration{
			{Name: "N", Kind: "namespace", Parent: -1, Span: analysis.Span{End: 30}},
			{Name: "C", Kind: "class", Parent: -1},
		}},
		"b.probe": {Path: "b.probe", Language: "beta"},
	}
	root, child := analysis.DeclarationRef("a.probe", 0), analysis.SyntheticRef("module:b")
	lookup := func(name string) analysis.Adapter {
		return analysis.Adapter{Organizer: organizerFunc(func(context.Context, analysis.BuildScope) (analysis.Organization, error) {
			if name == "alpha" {
				return analysis.Organization{Roots: map[string]analysis.Ref{"a.probe": root}, Edges: []analysis.Edge{
					{Source: root, Target: child, Kind: "contains", Path: "a.probe"},
				}}, nil
			}
			return analysis.Organization{Entities: []analysis.Entity{{Ref: child, Kind: "Module", Name: "B", Language: name}}, Roots: map[string]analysis.Ref{"b.probe": child}, Edges: []analysis.Edge{
				{Source: analysis.DocumentRef("b.probe"), Target: child, Kind: "declares", Path: "b.probe"},
			}}, nil
		})}
	}
	index, _, err := Build(context.Background(), analysis.NewBuildScope(files, ""), 5, 9, 100, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Entities) != 5 {
		t.Fatalf("namespace view duplicated entities: %v", index.Entities)
	}
	ns := index.Namespace(root)
	if !reflect.DeepEqual(ns.Members("C"), []analysis.Ref{analysis.DeclarationRef("a.probe", 1)}) || !reflect.DeepEqual(ns.Members("B"), []analysis.Ref{child}) {
		t.Fatalf("declared and synthetic members diverged: %v %v", ns.Members("C"), ns.Members("B"))
	}
	if len(ns.Members("N")) != 0 {
		t.Fatal("semantic root contains itself")
	}
	if contributions := ns.Contributions(); len(contributions) != 1 || contributions[0].Path != "a.probe" {
		t.Fatalf("lost declared namespace provenance: %v", contributions)
	}
	if len(index.Namespace(child).Contributions()) != 1 || len(index.Namespace(child).Members("C")) != 0 {
		t.Fatal("empty module inherited ancestor members")
	}
	roots := map[analysis.Ref]analysis.Ref{}
	for _, e := range index.Edges {
		if e.Kind == "in_namespace" {
			roots[e.Source] = e.Target
		}
		if e.Kind == "contains" && e.Source.IsDocument() {
			t.Fatal("source material became a member owner")
		}
	}
	if roots[analysis.DocumentRef("a.probe")] != root || roots[analysis.DocumentRef("b.probe")] != child {
		t.Fatal("document roots not published", roots)
	}
}

func TestRelationAndEvidenceBudgetsAreSeparate(t *testing.T) {
	files := map[string]analysis.Facts{"a": {Path: "a", Language: "fixture"}, "b": {Path: "b", Language: "fixture"}}
	lookup := func(string) analysis.Adapter {
		return analysis.Adapter{Binder: binderFunc(func(_ context.Context, _ analysis.BuildScope, _ *analysis.Index, limit int) (analysis.BindResult, error) {
			if limit < 2 {
				return analysis.BindResult{}, analysis.ErrEvidenceLimit
			}
			e := analysis.Edge{Source: analysis.DocumentRef("a"), Target: analysis.DocumentRef("b"), Kind: "imports", Path: "a", Basis: "first", Confidence: "scoped"}
			other := e
			other.Basis = "second"
			return analysis.BindResult{Edges: []analysis.Edge{e, other}}, nil
		})}
	}
	index, _, err := Build(context.Background(), analysis.NewBuildScope(files, ""), 2, 1, 2, lookup)
	if err != nil || len(index.Edges) != 1 || index.EvidenceCount != 2 {
		t.Fatal(index, err)
	}
	if _, _, err := Build(context.Background(), analysis.NewBuildScope(files, ""), 2, 1, 1, lookup); err == nil {
		t.Fatal("evidence budget ignored")
	}
}
