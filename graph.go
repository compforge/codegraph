package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/compforge/codegraph/internal/graphstore"
)

// Graph is a read-only publication with a lazily materialized query index.
// Every constructor returns a fixed snapshot of nodes and relations.
// +spec=Node and Relation values are the sole code-fact source for graph consumers; derived views never read extraction artifacts.
type Graph struct {
	snapshot       string
	nodes          map[string]Node
	relations      map[string]Relation
	report         BuildReport
	limits         graphstore.Limits
	timeout        time.Duration
	storeMu        sync.Mutex
	store          *graphstore.Store
	namespaceMu    sync.Mutex
	namespaceIndex *namespaceIndex
}

func (g *Graph) Snapshot() string { return g.snapshot }

// Nodes returns independent values in source-ID order.
func (g *Graph) Nodes() []Node {
	out := make([]Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, cloneNode(n))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g *Graph) Relations() []Relation {
	out := make([]Relation, 0, len(g.relations))
	for _, r := range g.relations {
		out = append(out, cloneRelation(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g *Graph) Report() BuildReport {
	return cloneReport(g.report)
}

// Find returns declaration nodes in source order. An empty kind matches every
// declaration kind; an empty qualifiedName matches every name. Source uses,
// Documents and organizations without a single Location are excluded. Follow declares from a
// Document to find its package/module contributions.
// The returned nodes are detached values and can be safely modified.
func (g *Graph) Find(path string, kind NodeKind, qualifiedName string) []Node {
	out := make([]Node, 0)
	for _, node := range g.nodes {
		if node.Kind == DocumentNodeKind || node.Kind == Reference || node.Kind == Import || node.Kind == Export || node.Location == nil || node.Location.Path != path {
			continue
		}
		if kind != "" && node.Kind != kind {
			continue
		}
		if qualifiedName != "" && node.QualifiedName != qualifiedName {
			continue
		}
		out = append(out, cloneNode(node))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Location.StartByte != out[j].Location.StartByte {
			return out[i].Location.StartByte < out[j].Location.StartByte
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Node returns a detached node by its source identity.
func (g *Graph) Node(id string) (Node, bool) {
	node, ok := g.nodes[id]
	if !ok {
		return Node{}, false
	}
	return cloneNode(node), true
}

// RelationsFrom and RelationsTo expose bounded adjacency without requiring a
// consumer to parse Cypher. If kinds is empty, all relation kinds are returned.
func (g *Graph) RelationsFrom(id string, kinds ...RelationKind) []Relation {
	return g.adjacent(id, false, kinds...)
}

func (g *Graph) RelationsTo(id string, kinds ...RelationKind) []Relation {
	return g.adjacent(id, true, kinds...)
}

func (g *Graph) adjacent(id string, incoming bool, kinds ...RelationKind) []Relation {
	allowed := make(map[RelationKind]bool, len(kinds))
	for _, kind := range kinds {
		allowed[kind] = true
	}
	var relations []Relation
	for _, relation := range g.relations {
		if incoming && relation.Target != id || !incoming && relation.Source != id {
			continue
		}
		if len(allowed) > 0 && !allowed[relation.Kind] {
			continue
		}
		relations = append(relations, cloneRelation(relation))
	}
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].Location.Path != relations[j].Location.Path {
			return relations[i].Location.Path < relations[j].Location.Path
		}
		if relations[i].Location.StartByte != relations[j].Location.StartByte {
			return relations[i].Location.StartByte < relations[j].Location.StartByte
		}
		return relations[i].ID < relations[j].ID
	})
	return relations
}

// Query returns detached Go values: Node, Relation, Path, scalar values,
// []any and map[string]any. Integer scalars are int64. No partial rows are
// returned when execution fails. Variable paths require explicit upper bounds.
// +rule=`Query entities must use source identities; Cypher id(n) is opaque and not portable between batches`
func (g *Graph) Query(ctx context.Context, q string, params map[string]any) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	// Serialize index construction only. Failed/canceled materialization leaves
	// no index, so a later query can retry against the same immutable snapshot.
	g.storeMu.Lock()
	if g.store == nil {
		store, err := g.materialize(ctx, g.nodes, g.relations)
		if err != nil {
			g.storeMu.Unlock()
			return nil, err
		}
		g.store = store
	}
	store := g.store
	g.storeMu.Unlock()
	rows, err := store.Query(ctx, q, params)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		for k, v := range row {
			x, err := g.project(v)
			if err != nil {
				return nil, err
			}
			row[k] = x
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func (g *Graph) project(v any) (any, error) {
	switch v := v.(type) {
	case graphstore.Entity:
		if v.Relation {
			r, ok := g.relations[v.ID]
			if !ok {
				return nil, fmt.Errorf("unknown relation %s", v.ID)
			}
			return cloneRelation(r), nil
		}
		n, ok := g.nodes[v.ID]
		if !ok {
			return nil, fmt.Errorf("unknown node %s", v.ID)
		}
		return cloneNode(n), nil
	case graphstore.Path:
		p := Path{}
		for _, n := range v.Nodes {
			x, err := g.project(n)
			if err != nil {
				return nil, err
			}
			p.Nodes = append(p.Nodes, x.(Node))
		}
		for _, r := range v.Relations {
			x, err := g.project(r)
			if err != nil {
				return nil, err
			}
			p.Relations = append(p.Relations, x.(Relation))
		}
		return p, nil
	case []any:
		for i, x := range v {
			p, err := g.project(x)
			if err != nil {
				return nil, err
			}
			v[i] = p
		}
		return v, nil
	case map[string]any:
		for k, x := range v {
			p, err := g.project(x)
			if err != nil {
				return nil, err
			}
			v[k] = p
		}
		return v, nil
	default:
		return v, nil
	}
}

func (g *Graph) materialize(ctx context.Context, nodes map[string]Node, relations map[string]Relation) (*graphstore.Store, error) {
	s := graphstore.New(g.limits)
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		props := map[string]any{"id": n.ID, "kind": string(n.Kind), "name": n.Name, "qualifiedName": n.QualifiedName, "language": n.Language, "snapshot": g.snapshot}
		if n.Kind == Reference {
			props["receiver"] = n.Receiver
			props["referenceKind"] = string(n.ReferenceKind)
		}
		if n.Binding != nil {
			props["specifier"] = n.Binding.Specifier
			props["importedName"] = n.Binding.ImportedName
			props["localName"] = n.Binding.LocalName
			props["exportedName"] = n.Binding.ExportedName
			props["form"] = n.Binding.Form
			props["typeOnly"] = n.Binding.TypeOnly
		}
		if n.Kind == DocumentNodeKind {
			props["documentKind"] = string(n.DocumentKind)
		}
		if m := n.Manifest; m != nil {
			props["manifestFormat"], props["manifestName"], props["manifestVersion"] = m.Format, m.Name, m.Version
			props["manifestProject"], props["manifestBuildSystem"], props["manifestWorkspace"] = m.Project, m.BuildSystem, m.Workspace
			for key, loc := range map[string]*Location{"manifestName": m.NameLocation, "manifestVersion": m.VersionLocation} {
				if loc != nil {
					props[key+"StartByte"], props[key+"EndByte"] = loc.StartByte, loc.EndByte
					props[key+"Line"], props[key+"Column"] = loc.Line, loc.Column
					props[key+"EndLine"], props[key+"EndColumn"] = loc.EndLine, loc.EndColumn
				}
			}
		}
		if n.Gitlink != "" {
			props["gitlink"] = n.Gitlink
		}
		if n.Location != nil {
			props["path"], props["line"], props["column"] = n.Location.Path, n.Location.Line, n.Location.Column
			props["startByte"], props["endByte"] = n.Location.StartByte, n.Location.EndByte
			props["endLine"], props["endColumn"] = n.Location.EndLine, n.Location.EndColumn
		}
		if loc := n.SignatureLocation; loc != nil {
			props["signature"] = n.Signature
			props["signatureStartByte"], props["signatureEndByte"] = loc.StartByte, loc.EndByte
			props["signatureLine"], props["signatureColumn"] = loc.Line, loc.Column
			props["signatureEndLine"], props["signatureEndColumn"] = loc.EndLine, loc.EndColumn
		}
		if loc := n.NameLocation; loc != nil {
			props["nameStartByte"], props["nameEndByte"] = loc.StartByte, loc.EndByte
			props["nameLine"], props["nameColumn"] = loc.Line, loc.Column
			props["nameEndLine"], props["nameEndColumn"] = loc.EndLine, loc.EndColumn
		}
		kinds := []string{}
		seen := map[MarkerKind]bool{}
		for _, kind := range []MarkerKind{Spec, Case, Rule, Link, Doc} {
			props[string(kind)] = []string{}
		}
		for _, m := range n.Markers {
			if !seen[m.Kind] {
				kinds = append(kinds, string(m.Kind))
				seen[m.Kind] = true
			}
			key := string(m.Kind)
			props[key] = append(props[key].([]string), m.Text)
		}
		props["markers"] = kinds
		encoded, err := json.Marshal(n.Markers)
		if err != nil {
			return nil, err
		}
		props["markerData"] = string(encoded)
		documentation := make([]string, 0, len(n.Documentation))
		for _, doc := range n.Documentation {
			documentation = append(documentation, doc.Text)
		}
		props["documentation"] = documentation
		encoded, err = json.Marshal(n.Documentation)
		if err != nil {
			return nil, err
		}
		props["documentationData"] = string(encoded)
		if err := s.AddNode(n.ID, string(n.Kind), props); err != nil {
			return nil, err
		}
	}
	for _, r := range relations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		props := map[string]any{"id": r.ID, "kind": string(r.Kind), "source": r.Source, "target": r.Target, "confidence": string(r.Confidence), "bases": evidenceBases(r), "evidenceData": evidenceJSON(r), "path": r.Location.Path, "line": r.Location.Line, "column": r.Location.Column, "startByte": r.Location.StartByte, "endByte": r.Location.EndByte}
		if err := s.AddEdge(r.Source, r.Target, string(r.Kind), props); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// GoGraph properties support scalar lists, not nested maps. Full evidence is
// available on RETURN r and as JSON; bases is a queryable scalar projection.
func evidenceBases(r Relation) []string {
	var bases []string
	for _, e := range r.Evidence {
		if len(bases) == 0 || bases[len(bases)-1] != e.Basis {
			bases = append(bases, e.Basis)
		}
	}
	return bases
}

func evidenceJSON(r Relation) string { b, _ := json.Marshal(r.Evidence); return string(b) }

// newGraph takes ownership of the completed values; callers must not mutate them.
func newGraph(snapshot string, opts Options, nodes map[string]Node, relations map[string]Relation, report BuildReport) *Graph {
	return &Graph{snapshot: snapshot, nodes: nodes, relations: relations, report: report, limits: graphstore.Limits{Rows: opts.MaxResultRows, Bytes: opts.MaxResultBytes, Hops: opts.MaxQueryHops}, timeout: opts.QueryTimeout}
}
