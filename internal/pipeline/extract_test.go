package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// +case:id=parse_context_budget,desc=`Caller cancellation and document parse budget`,expect=`Context error without accepting parsed facts`
func TestAnalyzeContextAndBudget(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	for _, tt := range []struct {
		name    string
		ctx     context.Context
		timeout time.Duration
		want    error
	}{
		{"cancelled", cancelled, time.Second, context.Canceled},
		{"parent deadline", expired, time.Second, context.DeadlineExceeded},
		{"parse budget", context.Background(), time.Nanosecond, context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Analyze(tt.ctx, "main.go", []byte("package main\nfunc main() {}\n"), tt.timeout)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Analyze() error = %v, want %v", err, tt.want)
			}
		})
	}
}

// +case:id=parse_cancel_before_tokenization,desc=`Caller cancels while the token source is created`,expect=`No tokens consumed and context.Canceled returned`
func TestAnalyzeCancellationDuringParserSetup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := grammars.DetectLanguageByName("c")
	entry := *base
	entry.Name = "codegraph_cancel_test"
	entry.Extensions = []string{".codegraph_cancel_test"}
	entry.Shebangs = nil
	var tokens *countedTokens
	entry.TokenSourceFactory = func(source []byte, lang *gts.Language) gts.TokenSource {
		tokens = &countedTokens{TokenSource: base.TokenSourceFactory(source, lang)}
		cancel()
		return tokens
	}
	grammars.Register(entry)
	_, err := Analyze(ctx, "input.codegraph_cancel_test", []byte("int value;\n"), time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Analyze() error = %v, want context.Canceled", err)
	}
	if tokens == nil || tokens.count != 0 {
		t.Fatalf("token source = %+v, want initialized source with no tokens consumed", tokens)
	}
}

type countedTokens struct {
	gts.TokenSource
	count int
}

func (s *countedTokens) Next() gts.Token {
	s.count++
	return s.TokenSource.Next()
}

func TestAnalyzeStrictParseRoutes(t *testing.T) {
	for _, tt := range []struct {
		path   string
		source string
	}{
		{"main.go", "package main\nfunc main() {}\n"},
		{"main.c", "int main(void) { return 0; }\n"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			facts, err := Analyze(context.Background(), tt.path, []byte(tt.source), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if facts.Language == "" || facts.Path != tt.path {
				t.Fatalf("missing document facts: language=%q path=%q", facts.Language, facts.Path)
			}
		})
	}
	_, err := Analyze(context.Background(), "broken.go", []byte("package main\nfunc broken( {\n"), time.Second)
	if err == nil || !strings.Contains(err.Error(), "incomplete syntax tree") {
		t.Fatalf("Analyze() error = %v, want incomplete syntax tree", err)
	}
}
