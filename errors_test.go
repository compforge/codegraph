package codegraph

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestDirectoryBuildBudgetError(t *testing.T) {
	ctx := context.Background()
	doc := Document{Path: "a/b/data.txt", Content: []byte("data")}
	full, report, err := Build(ctx, "directory-budget", []Document{doc}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	evidence := 0
	for _, edge := range full.Relations() {
		evidence += len(edge.Evidence)
	}
	if _, _, err := Build(ctx, "directory-budget", []Document{doc}, Options{MaxNodes: report.Nodes, MaxRelations: report.Relations, MaxEvidence: evidence}); err != nil {
		t.Fatal("exact directory budget rejected", err)
	}
	for _, tc := range []struct {
		resource string
		limit    int
		opts     Options
	}{
		{"MaxNodes", report.Nodes - 1, Options{MaxNodes: report.Nodes - 1}},
		{"MaxRelations", report.Relations - 1, Options{MaxRelations: report.Relations - 1}},
		{"MaxEvidence", evidence - 1, Options{MaxEvidence: evidence - 1}},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			b, err := NewBuilder("directory-budget", tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			previous := b.Result()
			_, err = b.addDocumentsSync(ctx, doc)
			wrapped := fmt.Errorf("build directory: %w", err)
			var budget *BuildBudgetError
			if !errors.Is(wrapped, ErrBuildBudget) || !errors.As(wrapped, &budget) {
				t.Fatalf("budget contract unavailable: %v", wrapped)
			}
			if budget.Stage != "directory" || budget.Resource != tc.resource || budget.Used != tc.limit || budget.Adding != 1 || budget.Limit != tc.limit {
				t.Fatalf("wrong budget details: %+v", budget)
			}
			if errors.Is(wrapped, ErrQueryBudget) {
				t.Fatal("build error matched query budget")
			}
			if b.Result() != previous {
				t.Fatal("failed directory publication replaced graph")
			}
		})
	}
}
