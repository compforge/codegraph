package golang

import (
	"github.com/compforge/codegraph/internal/analysis"
)

type GoType struct {
	Kind, Name, Module string
	Results            []*GoType // declared result types in signature order
	Result             int       // selected result of a call projection
	Element, Key       *GoType   // container element and map key types
	Inputs             []*GoType // operands of member, container and call-result projections
	Target             int       // same-file declaration, -1 when not represented
	Bound              bool      // a lexical binding shadows package-level names
}

func (*GoType) Language() string             { return "go" }
func typeShape(e analysis.Extension) *GoType { t, _ := e.(*GoType); return t }

type usageHints struct {
	Key       *GoType
	Receivers []*GoType
}

func (usageHints) Language() string         { return "go" }
func usage(e analysis.Extension) usageHints { h, _ := e.(usageHints); return h }
