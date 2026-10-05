package corpus_test

import cg "github.com/compforge/codegraph"

// Keep organization ancestry distinct from the much larger declaration-member
// denominator. A high contains score must not hide broken namespace parents.
func compareOrganizationStructure(e *evaluation, o *oracle, a observed, nodeKeys map[string]string) {
	if o.Organizations == nil {
		return
	}
	namespaceKind := func(kind cg.NodeKind) bool { return kind == cg.Package || kind == cg.Module || kind == cg.Namespace }
	expectedNamespaces := map[string]bool{}
	for key := range o.Organizations {
		expectedNamespaces[key] = true
	}
	for key, d := range o.Declarations {
		if namespaceKind(d.Kind) {
			expectedNamespaces[key] = true
		}
	}
	actualNamespaces := map[string]bool{}
	for _, n := range a.Nodes {
		if namespaceKind(n.Kind) {
			actualNamespaces[n.ID] = true
		}
	}
	truth := map[string]semanticRelation{}
	for _, r := range o.Relations {
		if r.Kind == cg.InNamespace || r.Kind == cg.Contains && expectedNamespaces[r.Source] && expectedNamespaces[r.Target] {
			truth[semanticKey(r)] = r
		}
	}
	found := map[string]bool{}
	for _, r := range a.Relations {
		if r.Kind != cg.InNamespace && !(r.Kind == cg.Contains && actualNamespaces[r.Source] && actualNamespaces[r.Target]) {
			continue
		}
		got := semanticRelation{nodeKeys[r.Source], nodeKeys[r.Target], r.Kind, location(r.Location)}
		key := semanticKey(got)
		if _, ok := truth[key]; ok && !found[key] {
			found[key] = true
			continue
		}
		e.metric("organization_structure/"+string(r.Kind)).Unexpected++
		e.Findings = append(e.Findings, finding{Category: "unexpected_organization_structure/" + string(r.Kind), Site: got.Site, Actual: got.Source + " -> " + got.Target})
	}
	for key, r := range truth {
		m := e.metric("organization_structure/" + string(r.Kind))
		m.Expected++
		if found[key] {
			m.Found++
		} else {
			e.Findings = append(e.Findings, finding{Category: "missing_organization_structure/" + string(r.Kind), Site: r.Site, Expected: r.Source + " -> " + r.Target})
		}
	}
}
