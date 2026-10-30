package ecmascript

import (
	"context"
	"io/fs"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/language/syntax"
	gts "github.com/odvcencio/gotreesitter"
)

// +spec=Only lexically bound Node fs reads produce resource uses; strings alone are not dependencies.
// +link=component://docs/language-support.md
func extractResourceReads(ctx context.Context, f *Facts, tree *gts.Tree, extracted gts.FactSet) error {
	arguments := syntax.ArgumentsByCall(extracted.CallArguments)
	nodes := syntax.NodesBySpan(tree)
	for _, call := range f.Calls {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !nodeAPI(*f, call.Name, call.Receiver, call.Span, "fs", "readFileSync") {
			continue
		}
		read := analysis.ResourceRead{Name: call.Name, Receiver: call.Receiver, Span: call.Span, Owner: analysis.EnclosingDeclaration(*f, call.Span)}
		args := arguments[call.Span]
		if len(args) > 0 {
			arg := args[0]
			read.Expression = string(f.Source[arg.StartByte:arg.EndByte])
			if !arg.Spread {
				n := nodes[analysis.Span{Start: int(arg.StartByte), End: int(arg.EndByte)}]
				read.Path = resourceURL(*f, tree, n, arguments)
			}
		}
		f.ResourceReads = append(f.ResourceReads, read)
	}
	return nil
}

// nodeAPI verifies import identity, including aliases and lexical shadowing.
// It does not infer API identity from a method name or a source string.
func nodeAPI(f Facts, name, receiver string, use analysis.Span, module, member string) bool {
	local := name
	if receiver != "" {
		local = receiver
	}
	ref, ok := analysis.LocalImport(f, local, use)
	if !ok {
		return false
	}
	for _, item := range analysis.ModuleItems(f) {
		b := item.Binding
		if item.Ref != ref || b.TypeOnly || (b.Specifier != module && b.Specifier != "node:"+module) {
			continue
		}
		if receiver == "" {
			return b.Form == "named" && b.ImportedName == member
		}
		return name == member && (b.Form == "namespace" || b.Form == "default")
	}
	return false
}

func resourceURL(f Facts, tree *gts.Tree, n *gts.Node, arguments map[analysis.Span][]gts.CallArgumentFact) string {
	if n == nil || n.Type(tree.Language()) != "new_expression" {
		return ""
	}
	constructor := n.ChildByFieldName("constructor", tree.Language())
	if constructor == nil || constructor.Type(tree.Language()) != "identifier" {
		return ""
	}
	name, span := constructor.Text(f.Source), syntax.NodeSpan(n)
	// Node's global URL is recognized only when no lexical binding shadows it.
	if !(name == "URL" && len(f.Lexical.Lookup(name, span)) == 0) && !nodeAPI(f, name, "", span, "url", "URL") {
		return ""
	}
	args := arguments[span]
	if len(args) != 2 || args[0].Spread || args[1].Spread {
		return ""
	}
	base := string(f.Source[args[1].StartByte:args[1].EndByte])
	if base != "import.meta.url" {
		return ""
	}
	raw := string(f.Source[args[0].StartByte:args[0].EndByte])
	if args[0].NodeType != "string" || len(raw) < 2 || (raw[0] != '\'' && raw[0] != '"') || raw[len(raw)-1] != raw[0] {
		return ""
	}
	value := raw[1 : len(raw)-1]
	// This first slice does not emulate JS escaping or WHATWG URL normalization.
	// Reject forms where filesystem cleaning would disagree with URL semantics.
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(strings.SplitN(value, "/", 2)[0], ":") {
		return ""
	}
	if strings.ContainsAny(value, "\\%?#\x00\r\n\t") || strings.TrimSpace(value) != value {
		return ""
	}
	// A directory URL must not collapse onto a file with the same spelling.
	last := value[strings.LastIndexByte(value, '/')+1:]
	if last == "" || last == "." || last == ".." {
		return ""
	}
	target := path.Join(path.Dir(f.Path), value)
	if target == "." || !fs.ValidPath(target) || target == ".." || strings.HasPrefix(target, "../") {
		return ""
	}
	return target
}

// Bind against supplied Documents only. Missing materials remain discoverable
// through ResourceRead.Path; the analyzer never opens the host filesystem.
func bindResourceReads(ctx context.Context, scope analysis.BuildScope, index *analysis.Index, result *analysis.BindResult) error {
	for _, name := range scope.Names {
		f := scope.Files[name]
		for _, read := range f.ResourceReads {
			if err := ctx.Err(); err != nil {
				return err
			}
			target := analysis.DocumentRef(read.Path)
			_, exists := index.Entities[target]
			if read.Path == "" || !exists || index.Files[read.Path].Gitlink != "" {
				code, detail := "unresolved_resource", "read path is not statically anchored: "+read.Expression
				if read.Path != "" {
					code, detail = "resource_not_loaded", "read target is not a supplied file Document: "+read.Path
				}
				result.Issues = append(result.Issues, analysis.Gap{Code: code, Reference: detail, Relation: "reads", Path: f.Path, Span: read.Span})
				continue
			}
			result.Edges = append(result.Edges, analysis.Edge{Source: analysis.SourceRef(f.Path, read.Owner), Target: target, Kind: "reads", Path: f.Path, Span: read.Span, Confidence: analysis.Exact, Basis: "node_fs_read"})
		}
	}
	return nil
}
