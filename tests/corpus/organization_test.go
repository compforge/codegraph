package corpus_test

import (
	"encoding/json"
	cg "github.com/compforge/codegraph"
	"sort"
)

func semanticKey(r semanticRelation) string { b, _ := json.Marshal(r); return string(b) }

// Complete source contribution/member edges from oracle-owned spans. Receiver
// ownership overrides come from go/types; nested ownership uses independent AST
// declaration containment. Neither production facts nor graph IDs are inputs.
func completeOrganizations(o *oracle, docs []cg.Document) {
	if o.Organizations == nil {
		return
	}
	roots := map[string]string{}
	for id, u := range o.Organizations {
		for path, at := range u.Contributions {
			roots[path] = id
			o.Relations = append(o.Relations, semanticRelation{"file:" + path, id, cg.Declares, at}, semanticRelation{"file:" + path, id, cg.InNamespace, at})
			if u.Parent != "" {
				o.Relations = append(o.Relations, semanticRelation{u.Parent, id, cg.Contains, at})
			}
		}
	}
	for key, d := range o.Declarations {
		if !d.Supported {
			continue
		}
		o.Relations = append(o.Relations, semanticRelation{"file:" + d.Span.Path, key, cg.Declares, d.Span})
		owner := roots[d.Span.Path]
		size := int(^uint(0) >> 1)
		if explicit := o.Owners[key]; explicit != "" {
			owner = explicit
		} else {
			for parent, p := range o.Declarations {
				if parent != key && p.Supported && p.Kind != cg.Field && p.Span.Path == d.Span.Path && p.Span.Start <= d.Span.Start && p.Span.End >= d.Span.End && p.Span.End-p.Span.Start < size {
					owner = parent
					size = p.Span.End - p.Span.Start
				}
			}
		}
		if owner != "" {
			o.Relations = append(o.Relations, semanticRelation{owner, key, cg.Contains, d.Span})
		}
	}
	for _, i := range o.Imports {
		if _, ok := o.Organizations[i.Target]; i.Class == "internal" && ok {
			o.Relations = append(o.Relations, semanticRelation{"file:" + i.Site.Path, i.Target, cg.Imports, i.Site})
		}
	}
	// Independent occurrence oracle also keeps references and calls at the same
	// source/target as distinct kinds, and repeated calls as distinct locations.
	for kind, uses := range map[cg.RelationKind]map[string]occurrence{cg.Calls: o.Calls, cg.References: o.References} {
		for _, u := range uses {
			if u.Class != "internal" {
				continue
			}
			source := "file:" + u.Site.Path
			size := int(^uint(0) >> 1)
			for k, d := range o.Declarations {
				if d.Supported && d.Span.Path == u.Site.Path && d.Span.Start <= u.Site.Start && d.Span.End >= u.Site.End && d.Span.End-d.Span.Start < size {
					source = k
					size = d.Span.End - d.Span.Start
				}
			}
			o.Relations = append(o.Relations, semanticRelation{source, u.Target, kind, u.Site})
		}
	}
	sort.Slice(o.Relations, func(i, j int) bool { return semanticKey(o.Relations[i]) < semanticKey(o.Relations[j]) })
}
func compareOrganizations(e *evaluation, o *oracle, a observed, nodeKeys map[string]string) {
	if o.Organizations == nil {
		return
	}
	docs := map[string]string{}
	for _, n := range a.Nodes {
		if n.Kind == cg.DocumentKind && n.Location != nil {
			docs[n.ID] = n.Location.Path
			nodeKeys[n.ID] = "file:" + n.Location.Path
		}
	}
	contributions := map[string]map[string]bool{}
	for _, r := range a.Relations {
		if r.Kind == cg.Declares && docs[r.Source] != "" {
			if contributions[r.Target] == nil {
				contributions[r.Target] = map[string]bool{}
			}
			contributions[r.Target][docs[r.Source]] = true
		}
	}
	found := map[string]bool{}
	for _, n := range a.Nodes {
		if n.Location != nil {
			continue
		}
		match := ""
		for id, u := range o.Organizations {
			if n.Kind != u.Kind || n.Name != u.Name || n.QualifiedName != u.QualifiedName || len(contributions[n.ID]) != len(u.Contributions) {
				continue
			}
			same := true
			for path := range u.Contributions {
				if !contributions[n.ID][path] {
					same = false
				}
			}
			if same {
				match = id
				break
			}
		}
		m := e.metric("organizations/" + string(n.Kind))
		if match == "" || found[match] {
			m.Unexpected++
			e.Findings = append(e.Findings, finding{Category: "unexpected_organization", Actual: n.QualifiedName})
		} else {
			found[match] = true
			nodeKeys[n.ID] = match
		}
	}
	for id, u := range o.Organizations {
		m := e.metric("organizations/" + string(u.Kind))
		m.Expected++
		if found[id] {
			m.Found++
		} else {
			e.Findings = append(e.Findings, finding{Category: "missing_organization", Expected: id})
		}
	}
}
func compareSemanticRelations(e *evaluation, o *oracle, a observed, nodeKeys map[string]string) {
	if o.Organizations == nil {
		return
	}
	organizationNodes := map[string]bool{}
	sourceItems := map[string]bool{}
	for _, n := range a.Nodes {
		if n.Kind == cg.Reference || n.Kind == cg.Import || n.Kind == cg.Export {
			sourceItems[n.ID] = true
		}
		if n.Location == nil {
			organizationNodes[n.ID] = true
		}
	}
	truth := map[string]semanticRelation{}
	for _, r := range o.Relations {
		truth[semanticKey(r)] = r
	}
	found := map[string]bool{}
	for _, r := range a.Relations {
		// This oracle compares declaration-level relations, not source-item projections.
		if sourceItems[r.Source] {
			e.Unassessed["source_relation/"+string(r.Kind)]++
			continue
		}
		if r.Kind == cg.Imports && !organizationNodes[r.Target] {
			e.Unassessed["relation/imports_symbol_or_boundary"]++
			continue
		}
		source, target := nodeKeys[r.Source], nodeKeys[r.Target]
		at := location(r.Location)
		got := semanticRelation{source, target, r.Kind, at}
		key := semanticKey(got)
		if r.Kind == cg.Imports {
			// Compiler/tokenizer import ranges differ. Normalize only same-endpoint,
			// nested ranges. A sibling import occurrence can never satisfy this one.
			for k, w := range truth {
				if w.Kind == r.Kind && w.Source == source && w.Target == target && w.Site.Path == at.Path && (w.Site.Start <= at.Start && w.Site.End >= at.End || at.Start <= w.Site.Start && at.End >= w.Site.End) {
					key = k
					break
				}
			}
		}
		if _, ok := truth[key]; ok {
			if found[key] {
				e.metric("relation_occurrences/"+string(r.Kind)).Unexpected++
			}
			found[key] = true
			continue
		}
		// Imports to external/unresolved targets and runtime call targets have no
		// independent answer in this profile; keep that boundary explicit.
		assessed := r.Kind == cg.Declares || r.Kind == cg.Contains || r.Kind == cg.InNamespace
		if r.Kind == cg.Imports {
			for _, w := range o.Relations {
				if w.Kind == r.Kind && w.Site.Path == at.Path && w.Site.Start <= at.End && w.Site.End >= at.Start {
					assessed = true
				}
			}
		}
		if r.Kind == cg.Calls || r.Kind == cg.References {
			uses := o.Calls
			if r.Kind == cg.References {
				uses = o.References
			}
			assessed = uses[at.key()].Class == "internal"
		}
		if assessed {
			e.metric("relation_occurrences/"+string(r.Kind)).Unexpected++
			e.Findings = append(e.Findings, finding{Category: "unexpected_relation/" + string(r.Kind), Site: at, Actual: source + " -> " + target})
		} else {
			e.Unassessed["relation/"+string(r.Kind)]++
		}
	}
	for key, r := range truth {
		m := e.metric("relation_occurrences/" + string(r.Kind))
		m.Expected++
		if found[key] {
			m.Found++
		} else {
			e.Findings = append(e.Findings, finding{Category: "missing_relation/" + string(r.Kind), Site: r.Site, Expected: r.Source + " -> " + r.Target})
		}
	}
}
