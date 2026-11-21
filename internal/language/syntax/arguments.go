package syntax

import gts "github.com/odvcencio/gotreesitter"

func ArgumentsByCall(arguments []gts.CallArgumentFact) map[Span][]gts.CallArgumentFact {
	calls := map[Span][]gts.CallArgumentFact{}
	for _, arg := range arguments {
		span := Span{Start: int(arg.CallStartByte), End: int(arg.CallEndByte)}
		calls[span] = append(calls[span], arg)
	}
	return calls
}

// NodeAtExactRange joins a fact to its named syntax node. An enclosing node
// alone cannot identify the argument or binding.
func NodeAtExactRange(parent *gts.Node, start, end uint32) *gts.Node {
	n := parent.NamedDescendantForByteRange(start, end)
	if n != nil && n.StartByte() == start && n.EndByte() == end {
		return n
	}
	return nil
}
