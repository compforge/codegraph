package graphstore

import (
	"fmt"

	"github.com/FlavioCFOliveira/GoGraph/cypher/ast"
	"github.com/FlavioCFOliveira/GoGraph/cypher/parser"
)

// checkQuery visits the upstream AST, including patterns inside subqueries and
// comprehensions. Regex checks would confuse strings/comments with syntax.
func checkQuery(q string, maxHops int) error {
	if len(q) > 64<<10 {
		return fmt.Errorf("%w: query exceeds 64 KiB", ErrBudget)
	}
	tree, err := parser.Parse(q)
	if err != nil {
		return err
	}
	for node := range ast.Walk(tree) {
		switch n := node.(type) {
		case *ast.Call:
			return fmt.Errorf("%w: procedures are not exposed", ErrReadOnly)
		case *ast.PathPattern:
			if err := checkPathHops(n, maxHops); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkPathHops(path *ast.PathPattern, maxHops int) error {
	remaining := int64(maxHops)
	for step := path.Head; step != nil; step = step.Next {
		if step.Relationship == nil {
			continue
		}
		hops := int64(1)
		if r := step.Relationship.Range; r != nil {
			if r.Max == nil {
				return fmt.Errorf("%w: variable paths require an upper bound", ErrBudget)
			}
			hops = *r.Max
		}
		if hops < 0 || hops > remaining {
			return fmt.Errorf("%w: path hop limit %d", ErrBudget, maxHops)
		}
		remaining -= hops
	}
	return nil
}
