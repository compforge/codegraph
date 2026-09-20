package graphstore

import (
	"fmt"
	"reflect"

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
	return visit(reflect.ValueOf(tree), maxHops)
}

// GoGraph has no public AST visitor. Keep this structural walk at the adapter
// boundary and contract-test nested forms when upgrading the pinned dependency.
func visit(v reflect.Value, maxHops int) error {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		if v.CanInterface() {
			switch n := v.Interface().(type) {
			case ast.UpdatingClause:
				return ErrReadOnly
			case *ast.Call:
				return fmt.Errorf("%w: procedures are not exposed", ErrReadOnly)
			case *ast.PathPattern:
				hops := int64(0)
				for p := n.Head; p != nil; p = p.Next {
					if p.Relationship == nil {
						continue
					}
					r := p.Relationship.Range
					if r == nil {
						hops++
					} else {
						if r.Max == nil {
							return fmt.Errorf("%w: variable paths require an upper bound", ErrBudget)
						}
						if *r.Max < 0 || *r.Max > int64(maxHops) {
							return fmt.Errorf("%w: path hop limit %d", ErrBudget, maxHops)
						}
						hops += *r.Max
					}
					if hops > int64(maxHops) {
						return fmt.Errorf("%w: path hop limit %d", ErrBudget, maxHops)
					}
				}
			}
		}
		return visit(v.Elem(), maxHops)
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if err := visit(v.Field(i), maxHops); err != nil {
				return err
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if err := visit(v.Index(i), maxHops); err != nil {
				return err
			}
		}
	case reflect.Map:
		it := v.MapRange()
		for it.Next() {
			if err := visit(it.Value(), maxHops); err != nil {
				return err
			}
		}
	}
	return nil
}
