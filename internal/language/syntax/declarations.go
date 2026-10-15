package syntax

import (
	"sort"

	"github.com/compforge/codegraph/internal/analysis"
	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// +why=Grammar-owned facts own syntax extraction; adapter rows only supply scopes absent from the upstream defaults.
// +link=component://docs/language-pipeline.md
func declarationRules(entry grammars.LangEntry) []gts.DeclarationRule {
	rules := grammars.DeclarationRules(entry)
	switch entry.Name {
	case "go":
		rules = append(rules,
			gts.DeclarationRule{NodeType: "var_spec", Kind: "variable", NameField: "name", NameNodeType: "identifier"},
			gts.DeclarationRule{NodeType: "const_spec", Kind: "constant", NameField: "name", NameNodeType: "identifier"})
	case "javascript", "typescript", "tsx":
		// Preserve local and namespace bindings with the same upstream selectors.
		for _, rule := range rules {
			switch rule.NodeType {
			case "variable_declarator":
				rule.Ancestors = rule.Ancestors[:1]
			case "property_signature", "method_signature", "public_field_definition", "field_definition", "method_definition",
				"class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration":
				// Local/namespaced declarations and anonymous object-type members
				// use the upstream name selectors without top-level owner gates.
				rule.Ancestors = nil
				rule.ContainerPath = nil
			default:
				continue
			}
			rules = append(rules, rule)
		}
		rules = append(rules, gts.DeclarationRule{NodeType: "catch_clause", Kind: "variable", NameField: "parameter", NameNodeType: "identifier"})
		for _, token := range []string{"var", "let", "const"} {
			rules = append(rules, gts.DeclarationRule{NodeType: "for_in_statement", Kind: "variable", NameField: "left", NameNodeType: "identifier", TypeField: "kind", TypeNodeType: token})
		}
		if entry.Name != "javascript" {
			rules = append(rules, gts.DeclarationRule{NodeType: "internal_module", Kind: "namespace", NameField: "name", NameNodeType: "identifier"})
		}
	}
	return rules
}

// Declarations projects syntax facts into graph declarations. Name captures
// distinguish sibling identities even when their construct ranges coincide.
func Declarations(f *Facts, facts gts.FactSet) {
	type identity struct {
		name      Span
		container string
	}
	byName := map[identity]int{}
	for i, d := range f.Declarations {
		byName[identity{d.NameSpan, d.Container}] = i
	}
	covered := map[Span]bool{}
	add := func(d Declaration) {
		if i, ok := byName[identity{d.NameSpan, d.Container}]; ok {
			f.Declarations[i] = d
			return
		}
		byName[identity{d.NameSpan, d.Container}] = len(f.Declarations)
		f.Declarations = append(f.Declarations, d)
	}
	for _, d := range facts.Declarations {
		if d.Kind == "embed" {
			continue
		}
		kind := d.Kind
		// ECMAScript const binds a variable; enum members remain constants.
		if d.NodeType == "variable_declarator" && kind == "constant" {
			kind = "variable"
		}
		if d.NodeType == "property_signature" {
			kind = "property"
		}
		if kind == "type" {
			switch d.Shape {
			case "alias":
				kind = "type_alias"
			case "struct", "interface", "class", "enum":
				kind = d.Shape
			}
		}
		if analysis.ConcreteKind(kind) == "" {
			continue
		}
		span := Span{Start: int(d.StartByte), End: int(d.EndByte)}
		covered[span] = true
		add(Declaration{Name: d.Name, Kind: kind, Parent: -1, Span: span,
			Container: d.Container, ContainerSpan: Span{Start: int(d.ContainerStartByte), End: int(d.ContainerEndByte)},
			NameSpan: Span{Start: int(d.NameStartByte), End: int(d.NameEndByte)}})
	}
	for _, d := range facts.Definitions {
		span := Span{Start: int(d.StartByte), End: int(d.EndByte)}
		if covered[span] || analysis.ConcreteKind(d.Kind) == "" {
			continue
		}
		// Legacy method spans accept computed expressions as names. Rich rows
		// own named-member coverage; a missing row retains the semantic gap.
		if d.NodeType == "method_definition" {
			continue
		}
		add(Declaration{Name: d.Name, Kind: d.Kind, Parent: -1, Span: span,
			NameSpan: Span{Start: int(d.NameStartByte), End: int(d.NameEndByte)}})
	}
	for _, s := range facts.Signatures {
		name := Span{Start: int(s.NameStartByte), End: int(s.NameEndByte)}
		i, ok := byName[identity{name, s.Container}]
		if !ok {
			i, ok = byName[identity{name, ""}]
		}
		if ok {
			if s.NodeType == "variable_declarator" && s.Kind == "function" {
				f.Declarations[i].Callable = true
			}
			if s.Kind == "method" && len(s.Receiver) > 0 {
				f.Declarations[i].Receiver = s.Container
			}
			// Signature rows correct legacy method captures in modifier grammars.
			f.Declarations[i].Name = s.Name
		} else if s.NodeType != "variable_declarator" && !(f.Language == "go" && s.NodeType == "method_elem" && s.Container == "") {
			add(Declaration{Name: s.Name, Kind: s.Kind, Parent: -1, NameSpan: name,
				Span: Span{Start: int(s.StartByte), End: int(s.EndByte)}})
		}
	}
	// Control bindings denote the target, rather than the surrounding statement.
	for _, raw := range facts.Declarations {
		if raw.NodeType != "for_in_statement" && raw.NodeType != "catch_clause" {
			continue
		}
		name := Span{Start: int(raw.NameStartByte), End: int(raw.NameEndByte)}
		if i, ok := byName[identity{name, ""}]; ok {
			f.Declarations[i].Span = name
		}
	}
	AssignDeclarationParents(f.Declarations, f.Language)
}

// +spec=Only strict syntax containment establishes declaration ownership; shared ranges remain siblings and lexical visibility is bound separately.
func AssignDeclarationParents(decls []Declaration, language string) {
	sort.SliceStable(decls, func(i, j int) bool {
		a, b := decls[i], decls[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End > b.End
		}
		return a.NameSpan.Start < b.NameSpan.Start
	})
	var stack []int
	for i := range decls {
		d := &decls[i]
		d.Parent, d.QualifiedName = -1, d.Name
		for len(stack) > 0 {
			p := decls[stack[len(stack)-1]]
			if p.Start <= d.Start && p.End >= d.End && p.End-p.Start > d.End-d.Start {
				break
			}
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			d.Parent = stack[len(stack)-1]
			d.QualifiedName = decls[d.Parent].QualifiedName + "." + d.Name
			if d.Kind == "function" && decls[d.Parent].Kind == "class" {
				d.Kind = "method"
			}
		} else if d.Receiver != "" {
			d.QualifiedName = d.Receiver + "." + d.Name
		}
		if d.Container != "" {
			for j := 0; j < i; j++ {
				p := decls[j]
				container := p.Name
				if p.Container != "" {
					container = p.Container + "." + p.Name
				}
				if p.Span == d.ContainerSpan && container == d.Container && p.Start <= d.Start && p.End >= d.End && p.End-p.Start > d.End-d.Start {
					d.Parent = j
					d.QualifiedName = p.QualifiedName + "." + d.Name
					break
				}
			}
		}
		if language == "go" && (d.Kind == "field" || d.Kind == "variable" || d.Kind == "constant") {
			continue
		}
		stack = append(stack, i)
	}
}
