package codegraph

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/adapters/manifest"
	"github.com/compforge/codegraph/internal/pipeline"
)

// AdmissionPolicy separates material selection from graph publication. NodeKinds
// never restrict extraction or binding. Nil ParseTags and NodeKinds admit all;
// empty slices admit none. ExcludedTags apply to documents and ancestor directories.
// Recognized manifests still supply metadata when their tags are not selected for parsing.
// +spec=Resolve complete admitted source facts before selecting published nodes and endpoint-closed relations.
// +link=component://docs/admission.md
type AdmissionPolicy struct {
	ParseTags    []Tag
	ExcludedTags []Tag
	// NodeKinds selects the initial nodes; dependency bridges may add candidates.
	NodeKinds []NodeKind
	// GlobalVariablesOnly omits initial variable/constant selections in local or type
	// scopes, except function values identified by upstream signatures. It does
	// not change their kind or remove their binding facts.
	GlobalVariablesOnly bool
	// IncludeDependencyBridges retains local values and fields that connect
	// distinct selected code entities through supported dependency relations.
	IncludeDependencyBridges bool
}

// AnalysisAdmission returns a fresh policy for code understanding and impact
// consumers. Documents, directories and language namespaces remain as context.
// Git ignore evaluation belongs to the snapshot owner, not this library.
func AnalysisAdmission() *AdmissionPolicy {
	return &AdmissionPolicy{
		ParseTags:    []Tag{SourceTag},
		ExcludedTags: []Tag{DependencyTag, GeneratedTag, BuildOutputTag, CacheTag, MinifiedTag, ToolingTag, TestFixtureTag},
		NodeKinds: []NodeKind{DocumentNodeKind, DirectoryNodeKind, Module, Package, Namespace,
			Struct, Interface, Class, Type, TypeAlias, Enum, Record, Trait, Union,
			Function, Method, Constructor, Variable, Constant},
		GlobalVariablesOnly:      true,
		IncludeDependencyBridges: true,
	}
}

func cloneAdmission(p *AdmissionPolicy) *AdmissionPolicy {
	if p == nil {
		return nil
	}
	out := *p
	out.ParseTags = slices.Clone(p.ParseTags)
	out.ExcludedTags = slices.Clone(p.ExcludedTags)
	out.NodeKinds = slices.Clone(p.NodeKinds)
	return &out
}

func (b *Builder) excludedMaterial(d Document) string {
	p := b.opts.Admission
	if p == nil {
		return ""
	}
	if tag := b.tagMatcher.ExcludedBy(d, p.ExcludedTags); tag != "" {
		return fmt.Sprintf("%s excluded by %s material or ancestor tag", d.Path, tag)
	}
	return ""
}

func (b *Builder) parsesDocument(d Document) bool {
	if !b.allowed(d.Path) || b.excludedMaterial(d) != "" {
		return false
	}
	p := b.opts.Admission
	if p == nil || p.ParseTags == nil || manifest.Format(d.Path) != "" {
		return true
	}
	return slices.ContainsFunc(b.tagMatcher.DocumentTags(d), func(tag Tag) bool { return slices.Contains(p.ParseTags, tag) })
}

func documentFacts(d Document) analysis.Facts {
	f := pipeline.DocumentOnly(d.Path, bytes.Clone(d.Content))
	f.Gitlink = d.Gitlink
	return f
}

// admittedFiles also enforces material policy for externally extracted Facts.
// Raw extraction cache entries stay complete and independent of Builder policy.
func (b *Builder) admittedFiles(files map[string]analysis.Facts) (map[string]analysis.Facts, []Diagnostic) {
	if b.opts.Admission == nil {
		return files, nil
	}
	out := make(map[string]analysis.Facts, len(files))
	var excluded []Diagnostic
	for name, f := range files {
		d := Document{Path: name, Content: f.Source, Gitlink: f.Gitlink}
		if reason := b.excludedMaterial(d); reason != "" {
			excluded = append(excluded, Diagnostic{Code: "excluded_material", Subject: DocumentSubject,
				Message: reason, Location: location(f, analysis.Span{End: len(f.Source)})})
			continue
		}
		if !b.parsesDocument(d) {
			f = documentFacts(d)
		}
		out[name] = f
	}
	return out, excluded
}

func globalDeclaration(f analysis.Facts, i int, scopes map[int]int) bool {
	d := f.Declarations[i]
	// Lexical bindings handle anonymous function bodies and block-scoped variables,
	// neither of which necessarily has a declaration node as its parent.
	if scopeIndex, ok := scopes[i]; ok {
		scope := f.Lexical.Scopes[scopeIndex]
		if scope.Kind == "module" || scope.Kind == "namespace" {
			return true
		}
		// A namespace body has its own lexical block; nested blocks do not.
		return scope.Kind == "block" && scope.Parent >= 0 && f.Lexical.Scopes[scope.Parent].Kind == "namespace"
	}
	for parent := d.Parent; parent >= 0; parent = f.Declarations[parent].Parent {
		switch f.Declarations[parent].Kind {
		case "module", "namespace":
		default:
			return false
		}
	}
	return true
}

// projectAdmission operates only after the complete graph has been understood.
// It never resolves names or joins arbitrary paths through excluded endpoints.
func (b *Builder) projectAdmission(ctx context.Context, files map[string]analysis.Facts, nodes map[string]Node, relations map[string]Relation) (map[string]Node, map[string]Relation, error) {
	p := b.opts.Admission
	if p == nil {
		return nodes, relations, nil
	}
	local := map[string]bool{}
	if p.GlobalVariablesOnly || p.IncludeDependencyBridges {
		for name, f := range files {
			scopes := map[int]int{}
			if f.Lexical != nil {
				for _, binding := range f.Lexical.Bindings {
					if _, exists := scopes[binding.Target]; !exists {
						scopes[binding.Target] = binding.Scope
					}
				}
			}
			for i, d := range f.Declarations {
				kind := NodeKind(analysis.ConcreteKind(d.Kind))
				if (kind == Variable || kind == Constant) && !d.Callable && !globalDeclaration(f, i, scopes) {
					local[declarationID(name, kind, d.QualifiedName, d.Start)] = true
				}
			}
		}
	}
	selected := make(map[string]Node)
	for id, n := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if !(p.GlobalVariablesOnly && local[id]) && (p.NodeKinds == nil || slices.Contains(p.NodeKinds, n.Kind)) {
			selected[id] = n
		}
	}
	if p.IncludeDependencyBridges {
		if err := selectDependencyBridges(ctx, nodes, relations, selected, local); err != nil {
			return nil, nil, err
		}
	}
	edges := make(map[string]Relation)
	for id, r := range relations {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if _, ok := selected[r.Source]; !ok {
			continue
		}
		if _, ok := selected[r.Target]; !ok {
			continue
		}
		edges[id] = r
	}
	return selected, edges, nil
}

// selectDependencyBridges keeps dependency corridors between distinct core nodes.
// Structural context never seeds traversal. Ownership may enter an implementation
// declaration, but only a semantic dependency can lead out to another core node.
// +spec=Bridge selection preserves original relations and evidence without manufacturing calls across paths.
// +link=component://docs/admission.md
func selectDependencyBridges(ctx context.Context, nodes map[string]Node, relations map[string]Relation, selected map[string]Node, local map[string]bool) error {
	anchors, candidates := map[string]bool{}, map[string]bool{}
	for id, n := range nodes {
		if _, ok := selected[id]; ok {
			switch n.Kind {
			case DocumentNodeKind, DirectoryNodeKind, Module, Package, Namespace, Import, Export, Reference:
			default:
				anchors[id] = true
			}
		} else if local[id] || n.Kind == Field || n.Kind == Property {
			candidates[id] = true
		}
	}
	forward, backward := map[string][]string{}, map[string][]string{}
	for _, r := range relations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !(anchors[r.Source] || candidates[r.Source]) || !(anchors[r.Target] || candidates[r.Target]) {
			continue
		}
		switch r.Kind {
		case Calls, References, Aliases, Extends, Implements, Decorates:
		case Encloses, Contains:
			if !anchors[r.Source] || !candidates[r.Target] {
				continue
			}
		default:
			continue
		}
		forward[r.Source] = append(forward[r.Source], r.Target)
		backward[r.Target] = append(backward[r.Target], r.Source)
	}
	// Two distinct labels suffice to establish different endpoints. Each candidate
	// propagates at most twice in each direction, including in cyclic graphs.
	reach := func(adjacency map[string][]string) (map[string][]string, error) {
		type step struct{ node, anchor string }
		labels := map[string][]string{}
		var queue []step
		add := func(node, anchor string) {
			if candidates[node] && len(labels[node]) < 2 && !slices.Contains(labels[node], anchor) {
				labels[node] = append(labels[node], anchor)
				queue = append(queue, step{node, anchor})
			}
		}
		for anchor := range anchors {
			for _, next := range adjacency[anchor] {
				add(next, anchor)
			}
		}
		for i := 0; i < len(queue); i++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for _, next := range adjacency[queue[i].node] {
				add(next, queue[i].anchor)
			}
		}
		return labels, nil
	}
	from, err := reach(forward)
	if err != nil {
		return err
	}
	to, err := reach(backward)
	if err != nil {
		return err
	}
	for id := range candidates {
		for _, source := range from[id] {
			for _, target := range to[id] {
				if source != target {
					selected[id] = nodes[id]
				}
			}
		}
	}
	return nil
}
