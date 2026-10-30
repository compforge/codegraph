package codegraph

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/compforge/codegraph/internal/analysis"
)

// ResourceReferenceRule declares an API that forms a file URL from a relative
// path argument and the current module URL argument. This is supplied semantic
// knowledge, not an inference that a function reads or writes the file.
// Module empty matches an unshadowed global; otherwise it is an import specifier.
// Rules are opt-in, copied at construction, and never affect extraction caches.
// +spec=Core resource references require explicit semantics and never infer file dependencies from path-like strings alone.
// +link=component://docs/resource-references.md
type ResourceReferenceRule struct {
	Callable
	Constructor                bool
	PathArgument, BaseArgument int
	Basis                      string
}

type resourceRuleKey struct {
	Callable
	Constructor bool
}

func validateResourceRules(rules []ResourceReferenceRule, limit int) error {
	if len(rules) > limit {
		return fmt.Errorf("%w: resource rules exceed analysis evidence limit", ErrBuildBudget)
	}
	seen := map[resourceRuleKey]bool{}
	for _, r := range rules {
		if r.Name == "" || r.Basis == "" || r.PathArgument < 0 || r.BaseArgument < 0 || r.PathArgument == r.BaseArgument {
			return fmt.Errorf("invalid resource reference rule for %q", r.Name)
		}
		key := resourceRuleKey{r.Callable, r.Constructor}
		if seen[key] {
			return fmt.Errorf("duplicate resource reference rule for %q/%q", r.Module, r.Name)
		}
		seen[key] = true
	}
	return nil
}

// Material facts are immutable. Derived references belong only to this build;
// consumers using the same cache with different rules must not affect each other.
func applyResourceRules(ctx context.Context, files map[string]analysis.Facts, rules []ResourceReferenceRule) (map[string]analysis.Facts, error) {
	if len(rules) == 0 {
		return files, ctx.Err()
	}
	matched := map[resourceRuleKey]ResourceReferenceRule{}
	for _, rule := range rules {
		matched[resourceRuleKey{rule.Callable, rule.Constructor}] = rule
	}
	out := make(map[string]analysis.Facts, len(files))
	for name, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f.ResourceReferences = nil
		for _, call := range f.Invocations {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if call.Target == nil {
				continue
			}
			key := resourceRuleKey{Callable{Module: call.Target.Module, Name: call.Target.Name}, call.Constructor}
			rule, ok := matched[key]
			if !ok {
				continue
			}
			ref := analysis.ResourceReference{Name: call.Name, Receiver: call.Receiver, Span: call.Span, Owner: call.Owner, Basis: rule.Basis}
			positionsKnown := true
			for i, arg := range call.Arguments {
				if i > max(rule.PathArgument, rule.BaseArgument) {
					break
				}
				if arg.Spread {
					positionsKnown = false
					break
				}
			}
			if rule.PathArgument < len(call.Arguments) {
				arg := call.Arguments[rule.PathArgument]
				ref.Expression = arg.Text
				if positionsKnown && arg.Literal != nil && rule.BaseArgument < len(call.Arguments) && call.Arguments[rule.BaseArgument].ModuleURL {
					ref.Path = moduleRelativeResource(name, *arg.Literal)
				}
			}
			if !positionsKnown {
				ref.Expression = string(f.Source[call.Start:call.End])
			}
			f.ResourceReferences = append(f.ResourceReferences, ref)
		}
		out[name] = f
	}
	return out, nil
}

// Accept only the URL subset whose logical path normalization is unambiguous.
func moduleRelativeResource(document, value string) string {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(strings.SplitN(value, "/", 2)[0], ":") {
		return ""
	}
	if strings.ContainsAny(value, "\\%?#\x00\r\n\t") || strings.TrimSpace(value) != value {
		return ""
	}
	last := value[strings.LastIndexByte(value, '/')+1:]
	if last == "" || last == "." || last == ".." {
		return ""
	}
	target := path.Join(path.Dir(document), value)
	if target == "." || !fs.ValidPath(target) || target == ".." || strings.HasPrefix(target, "../") {
		return ""
	}
	return target
}
