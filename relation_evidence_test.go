package codegraph

import (
	"context"
	"errors"
	"testing"
)

func TestEvidenceBudgetIsAtomic(t *testing.T) {
	g, _ := New("budget", Options{MaxEvidence: 1})
	if err := g.AddDocuments(context.Background(), Document{Path: "a.go", Content: []byte("package a;func f(){}")}); err != nil {
		t.Fatal(err)
	}
	_, err := g.Wait(context.Background())
	if !errors.Is(err, ErrBuildBudget) || len(g.Nodes()) != 0 {
		t.Fatal(err, g.Nodes())
	}
}
