package corpus_test

import (
	"sort"
	"strconv"
	"strings"

	cg "github.com/compforge/codegraph"
)

type observed struct {
	Nodes     []cg.Node      `json:"nodes"`
	Relations []cg.Relation  `json:"relations"`
	Facts     []cg.Facts     `json:"facts"`
	Report    cg.BuildReport `json:"report"`
}

// +spec=`Corpus scoring uses independent source identities and preserves unknown ground truth`
func evaluate(o *oracle, a observed) evaluation {
	e := evaluation{Measurements: map[string]*measurement{}, Bindings: map[cg.RelationKind]*bindings{}, Unassessed: map[string]int{}, Diagnostics: map[string]int{}, Verdict: "measured"}
	for _, d := range a.Report.Diagnostics {
		e.Diagnostics[d.Code]++
	}
	nodeKeys := map[string]string{}
	found := map[string]bool{}
	for _, n := range a.Nodes {
		if n.Location == nil {
			if o.Organizations == nil {
				e.Unassessed["organization/"+string(n.Kind)]++
			}
			continue
		}
		if n.Kind == cg.Import || n.Kind == cg.Export {
			e.Unassessed["module_item/"+string(n.Kind)]++
			continue
		}
		if n.Kind == cg.Reference {
			e.Unassessed["occurrence/"+string(n.ReferenceKind)]++
			continue
		}
		if n.Kind == cg.DocumentKind {
			continue
		}
		key := ""
		for k, d := range o.Declarations {
			if n.Name == d.Name && n.Kind == d.Kind && n.Location.Path == d.Span.Path && n.Location.StartByte == d.Span.Start && n.Location.EndByte == d.Span.End {
				key = k
				break
			}
		}
		if key == "" {
			e.metric("declarations/all").Unexpected++
			e.Findings = append(e.Findings, finding{Category: "unexpected_declaration", Site: location(*n.Location), Actual: n.Name})
		} else {
			if found[key] {
				e.metric("declarations/all").Unexpected++
			}
			nodeKeys[n.ID], found[key] = key, true
		}
	}
	for key, d := range o.Declarations {
		group := "declarations/outside_contract/" + d.Reason
		if d.Supported {
			group = "declarations/contract/" + string(d.Kind)
		}
		for _, name := range []string{"declarations/all", group} {
			m := e.metric(name)
			m.Expected++
			if found[key] {
				m.Found++
			}
		}
		if !found[key] {
			e.Findings = append(e.Findings, finding{Category: "missing_declaration/" + gap(a.Report.Diagnostics, d.Span, ""), Site: d.NameSite, Expected: string(d.Kind) + " " + d.Name})
		}
	}
	compareOrganizations(&e, o, a, nodeKeys)
	compareSemanticRelations(&e, o, a, nodeKeys)
	compareOrganizationStructure(&e, o, a, nodeKeys)
	references, calls, imports := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, f := range a.Facts {
		for _, r := range f.References {
			if r.Kind != "" && r.Kind != cg.SymbolReference {
				e.Unassessed["reference_fact/"+string(r.Kind)]++
				continue
			}
			references[location(r.Location).key()] = true
		}
		for _, c := range f.Calls {
			calls[location(c.Location).key()] = true
		}
		for _, i := range f.Imports {
			// Import facts can locate the module token or its containing statement.
			// CPython aliases and tree-sitter statements have different spans;
			// require the same module path and nested ranges, never nearby text.
			key := location(i.Location).key()
			for k, expected := range o.Imports {
				if expected.Site.Path == i.Location.Path && expected.Name == i.Path && (expected.Site.Start <= i.Location.StartByte && expected.Site.End >= i.Location.EndByte || i.Location.StartByte <= expected.Site.Start && i.Location.EndByte >= expected.Site.End) {
					key = k
					break
				}
			}
			imports[key] = true
		}
	}
	for k := range o.ExcludedReferences {
		if references[k] {
			delete(references, k)
			e.Unassessed["reference_fact/package_or_builtin"]++
		}
	}
	compareFacts(&e, "reference_facts", o.References, references)
	compareFacts(&e, "call_expressions", o.Calls, calls)
	compareFacts(&e, "import_facts", o.Imports, imports)
	compareBindings(&e, cg.References, o.References, nodeKeys, a)
	compareBindings(&e, cg.Calls, o.Calls, nodeKeys, a)
	// Imports, containment and type edges stay in the raw graph. Their semantic
	// comparison is not implemented by this profile and cannot count as passes.
	for _, r := range a.Relations {
		if o.Organizations == nil && r.Kind != cg.References && r.Kind != cg.Calls {
			e.Unassessed["relation/"+string(r.Kind)]++
		}
	}
	for _, b := range e.Bindings {
		if b.tier(cg.Exact).Other > 0 {
			e.Verdict = "failed"
		}
	}
	sort.Slice(e.Findings, func(i, j int) bool {
		a, b := e.Findings[i], e.Findings[j]
		if a.Site.key() != b.Site.key() {
			return a.Site.key() < b.Site.key()
		}
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		return a.Actual < b.Actual
	})
	return e
}

func compareFacts(e *evaluation, name string, truth map[string]occurrence, actual map[string]bool) {
	for k, r := range truth {
		for _, group := range []string{name + "/all", name + "/" + r.Class} {
			m := e.metric(group)
			m.Expected++
			if actual[k] {
				m.Found++
			}
		}
		if !actual[k] {
			e.Findings = append(e.Findings, finding{Category: "missing_" + name, Site: r.Site, Expected: r.Name})
		}
	}
	for k := range actual {
		if _, ok := truth[k]; !ok {
			e.metric(name+"/all").Unexpected++
			e.Findings = append(e.Findings, finding{Category: "unexpected_" + name, Actual: k})
		}
	}
}

func compareBindings(e *evaluation, kind cg.RelationKind, truth map[string]occurrence, nodeKeys map[string]string, a observed) {
	b := &bindings{}
	for _, c := range []cg.Confidence{cg.Exact, cg.Scoped, cg.NameOnly, cg.Heuristic} {
		b.tier(c)
	}
	e.Bindings[kind] = b
	hits, candidates := map[string]bool{}, map[string]map[string]bool{}
	sourceUses := map[string]bool{}
	for _, n := range a.Nodes {
		if n.Kind == cg.Reference {
			sourceUses[n.ID] = true
		}
	}
	for _, edge := range a.Relations {
		if sourceUses[edge.Source] {
			continue
		}
		if edge.Kind != kind {
			continue
		}
		key := location(edge.Location).key()
		// Target-set size measures topology independently of confidence grading.
		if candidates[key] == nil {
			candidates[key] = map[string]bool{}
		}
		candidates[key][edge.Target] = true
		tier := b.tier(edge.Confidence)
		want, exists := truth[key]
		if !exists || want.Class == "runtime_dispatch" || want.Class == "unknown" {
			tier.Unassessed++
			e.Unassessed[string(kind)+"/unresolved_oracle"]++
			continue
		}
		actual := nodeKeys[edge.Target]
		correct := actual != "" && actual == want.Target
		if correct {
			hits[key] = true
		}
		if correct {
			tier.Hit++
		} else {
			tier.Other++
		}
		if !correct {
			e.Findings = append(e.Findings, finding{Category: "target_difference/" + string(kind), Site: want.Site, Expected: want.Target, Actual: edge.Target, Confidence: edge.Confidence, Evidence: edge.Evidence})
		}
	}
	for _, set := range candidates {
		b.MaxTargets = max(b.MaxTargets, len(set))
	}
	for k, r := range truth {
		if r.Class != "internal" {
			e.Unassessed[string(kind)+"/"+r.Class]++
			continue
		}
		b.Expected++
		if hits[k] {
			b.Hit++
			continue
		}
		g := gap(a.Report.Diagnostics, r.Site, kind)
		switch g {
		case "localized":
			b.LocalizedMissing++
		case "document":
			b.DocumentGapMissing++
		default:
			b.SilentMissing++
		}
		e.Findings = append(e.Findings, finding{Category: "missing_target/" + string(kind) + "/" + g, Site: r.Site, Expected: r.Target})
	}
}

func gap(diagnostics []cg.Diagnostic, s site, kind cg.RelationKind) string {
	coarse := false
	for _, d := range diagnostics {
		if d.Location.Path != s.Path {
			continue
		}
		if d.Subject == cg.DocumentSubject {
			coarse = true
			continue
		}
		if kind == "" && d.Subject != cg.DeclarationsSubject {
			continue
		}
		if kind != "" && (d.Subject != cg.RelationsSubject || d.Relation != kind) {
			continue
		}
		if d.Location.StartByte <= s.Start && d.Location.EndByte >= s.End {
			if d.Outline != nil {
				coarse = true
			} else {
				return "localized"
			}
		}
	}
	if coarse {
		return "document"
	}
	return "silent"
}

func summary(e evaluation) string {
	var lines []string
	var expected, found int
	for k, m := range e.Measurements {
		if strings.HasPrefix(k, "declarations/contract/") {
			expected += m.Expected
			found += m.Found
		}
	}
	lines = append(lines, fmtRatio("contract declarations", found, expected))
	for _, kind := range []cg.RelationKind{cg.References, cg.Calls} {
		b := e.Bindings[kind]
		lines = append(lines, fmtRatio(string(kind)+" target hits", b.Hit, b.Expected))
	}
	return strings.Join(lines, "; ")
}

func fmtRatio(name string, found, expected int) string {
	return name + ": " + strconv.Itoa(found) + "/" + strconv.Itoa(expected)
}
