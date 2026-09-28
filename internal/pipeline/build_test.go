package pipeline

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/compforge/codegraph/internal/analysis"
)

type organizerFunc func(context.Context, analysis.Scope) ([]analysis.Organization, []analysis.Edge, error)

func (f organizerFunc) Organize(c context.Context, s analysis.Scope) ([]analysis.Organization, []analysis.Edge, error) {
	return f(c, s)
}

type binderFunc func(context.Context, analysis.Scope, *analysis.Index, int) (analysis.Binding, error)

func (f binderFunc) Bind(c context.Context, s analysis.Scope, i *analysis.Index, n int) (analysis.Binding, error) {
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
			{Name: "N", Kind: "namespace", Parent: -1}, {Name: "C", Kind: "class", Parent: 0},
		}},
		"b.probe": {Path: "b.probe", Language: "beta", Declarations: []analysis.Declaration{
			{Name: "run", Kind: "method", Parent: -1, Receiver: "C"},
		}},
	}
	owner, member := analysis.DeclarationRef("a.probe", 1), analysis.DeclarationRef("b.probe", 0)
	var events []string
	lookup := func(name string) analysis.Adapter {
		return analysis.Adapter{
			Organizer: organizerFunc(func(ctx context.Context, s analysis.Scope) ([]analysis.Organization, []analysis.Edge, error) {
				events = append(events, "organize:"+name)
				p := s.Names[0]
				key := "package:" + name
				return []analysis.Organization{{Key: key, Kind: "Package", Name: name, Language: name, Documents: []string{p}}}, []analysis.Edge{{Source: analysis.DocumentRef(p), Target: analysis.OrganizationRef(key), Kind: "declares", Path: p}}, nil
			}),
			Binder: binderFunc(func(ctx context.Context, s analysis.Scope, index *analysis.Index, limit int) (analysis.Binding, error) {
				events = append(events, "bind:"+name)
				if len(index.Units) != 2 {
					t.Fatal("bind ran before all organizations were available")
				}
				result := analysis.Binding{Resolver: resolverFunc(func(ctx context.Context, index *analysis.Index, limit int) ([]analysis.Edge, []analysis.Gap, error) {
					events = append(events, "resolve:"+name)
					if got := index.Members[owner]["run"]; !reflect.DeepEqual(got, []analysis.Ref{member}) {
						t.Fatalf("receiver member unavailable: %v", got)
					}
					if name == "alpha" {
						return []analysis.Edge{{Source: owner, Target: member, Kind: "calls", Path: "a.probe", Confidence: "candidate", Basis: "probe_binding"}}, nil, nil
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
	index, _, err := Build(context.Background(), analysis.NewScope(files, ""), 20, 20, lookup)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"organize:alpha", "organize:beta", "bind:alpha", "bind:beta", "resolve:alpha", "resolve:beta"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("phase barriers: %v", events)
	}
	ns := analysis.DeclarationRef("a.probe", 0)
	if got := index.Members[analysis.OrganizationRef("package:alpha")]["N"]; !reflect.DeepEqual(got, []analysis.Ref{ns}) {
		t.Fatalf("declared namespace membership: %v", got)
	}
	if got := index.Members[ns]["C"]; !reflect.DeepEqual(got, []analysis.Ref{owner}) {
		t.Fatalf("class membership: %v", got)
	}
	if len(index.Members[analysis.DocumentRef("a.probe")]) != 0 {
		t.Fatal("source ownership became semantic containment")
	}
	methods := analysis.NewMethodIndex(index)
	found, err := methods.Lookup(context.Background(), []analysis.Ref{owner}, "run", func(analysis.Ref) bool { return true }, 2)
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
					Organizer: organizerFunc(func(context.Context, analysis.Scope) ([]analysis.Organization, []analysis.Edge, error) {
						if phase == "organize" {
							return nil, nil, failure
						}
						return nil, nil, nil
					}),
					Binder: binderFunc(func(context.Context, analysis.Scope, *analysis.Index, int) (analysis.Binding, error) {
						if phase == "bind" {
							return analysis.Binding{}, failure
						}
						return analysis.Binding{Resolver: resolverFunc(func(context.Context, *analysis.Index, int) ([]analysis.Edge, []analysis.Gap, error) {
							if phase == "resolve" {
								return nil, nil, failure
							}
							return nil, nil, nil
						})}, nil
					}),
				}
			}
			index, _, err := Build(ctx, analysis.NewScope(files, ""), nodes, edges, lookup)
			if index != nil || !errors.Is(err, expected) {
				t.Fatalf("index=%v error=%v want=%v", index, err, expected)
			}
		})
	}
}
