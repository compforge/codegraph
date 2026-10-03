package golang

import (
	"github.com/compforge/codegraph/internal/analysis"
	"github.com/odvcencio/gotreesitter/grammars"
)

func Describe(entry grammars.LangEntry) analysis.Capability {
	return analysis.Capability{Documentation: true, SourceItems: []string{"Import"}, Language: "go", Organizations: []string{"Package"}, Declarations: []string{"Function", "Method", "Struct", "Interface", "Field", "Type", "TypeAlias", "Variable", "Constant"}, References: []string{"calls", "references", "extends"}, Relations: []string{"occurs_in", "aliases", "declares", "contains", "encloses", "imports", "calls", "references", "extends", "implements"}, Markers: []string{"spec", "case", "rule", "link", "doc"}, Limitations: []string{"documentation preserves AST-attached leading comment groups on supported declarations; multi-spec group comments are not assigned to individual declarations", "static package functions plus scoped receiver methods, embedded-interface method lookup and syntax-bound callable aliases; no runtime dispatch proof", "references use lexical binding or scoped name matches within Go packages and same-file module declarations", "variables and constants retain each declared name, including shared declarations", "members are extracted only from named struct/interface literals; anonymous nested types and promoted members are not expanded", "interface embedding binds extends; same-package direct method names produce heuristic implements without signature, pointer-set or promoted-method checking; empty and embedded/type-term interfaces are excluded from inference", "build tags and compiler type checking are not evaluated", "marker syntax: declaration comments using +kind=payload or +kind:payload"}}
}
