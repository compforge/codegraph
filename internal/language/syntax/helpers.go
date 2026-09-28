package syntax

import (
	"strings"

	gts "github.com/odvcencio/gotreesitter"
)

func ParseMarkers(raw string, base int) []Comment {
	var out []Comment
	for _, line := range strings.SplitAfter(raw, "\n") {
		text := strings.TrimSpace(line)
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(text, "//"), "/*"), "*/"))
		text = strings.TrimSpace(strings.TrimPrefix(text, "#"))
		text = strings.TrimSpace(strings.TrimPrefix(text, "*"))
		if strings.HasPrefix(text, "+") {
			kind, payload, ok := strings.Cut(text[1:], "=")
			if head, tail, found := strings.Cut(text[1:], ":"); found && (!ok || len(head) < len(kind)) {
				kind, payload, ok = head, tail, true
			}
			if ok && validMarker(kind) {
				payload = strings.TrimSpace(payload)
				if len(payload) >= 2 && payload[0] == '`' && payload[len(payload)-1] == '`' {
					payload = payload[1 : len(payload)-1]
				}
				if payload != "" {
					out = append(out, Comment{Kind: kind, Text: payload, Span: Span{Start: base, End: base + len(strings.TrimSuffix(line, "\n"))}})
				}
			}
		}
		base += len(line)
	}
	return out
}

func validMarker(k string) bool {
	switch k {
	case "spec", "case", "rule", "link", "doc":
		return true
	}
	return false
}
func Walk(root *gts.Node, visit func(*gts.Node)) {
	stack := []*gts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		visit(n)
		for i := n.NamedChildCount() - 1; i >= 0; i-- {
			stack = append(stack, n.NamedChild(i))
		}
	}
}
func ReferenceOwner(f *Facts, span Span) int {
	owner, size := -1, len(f.Source)+1
	for i, d := range f.Declarations {
		if d.Start <= span.Start && d.End >= span.End && d.End-d.Start < size {
			owner, size = i, d.End-d.Start
		}
	}
	return owner
}
