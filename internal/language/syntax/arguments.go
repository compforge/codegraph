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

// NodesBySpan joins detached upstream captures to nodes during extraction only.
func NodesBySpan(tree *gts.Tree) map[Span]*gts.Node {
	nodes := map[Span]*gts.Node{}
	Walk(tree.RootNode(), func(n *gts.Node) { nodes[NodeSpan(n)] = n })
	return nodes
}
