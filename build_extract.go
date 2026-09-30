package codegraph

import (
	"context"
	"fmt"
	"sync"

	"github.com/compforge/codegraph/internal/pipeline"

	"github.com/compforge/codegraph/internal/analysis"
)

// stageDocuments reserves capacity for each extraction window before starting
// workers. Failed parses release their reservations before the next window, so
// parallelism does not change which documents fit the existing source budget.
// Documents without a registered grammar are staged as file-level facts without
// parsing; the file still enters the graph and the coverage gap stays visible
// as an unsupported_language issue on that file.
// +spec=`Workers own independent extraction results and never mutate graph maps`
func (g *Builder) stageDocuments(ctx context.Context, documents []Document, staged map[string]analysis.Facts, failures map[string]Diagnostic, total int64, parseFailures map[string]error, prepared map[string]analysis.Facts) error {
	for next := 0; next < len(documents); {
		batch := make([]Document, 0, min(g.opts.BuildConcurrency, len(documents)-next))
		var reserved int64
		for next < len(documents) && len(batch) < g.opts.BuildConcurrency {
			if err := ctx.Err(); err != nil {
				return err
			}
			document := documents[next]
			name, data := document.Path, document.Content
			issue := func(code, message string) {
				failures[name] = Diagnostic{Code: code, Message: message, Subject: DocumentSubject,
					Location: location(pipeline.DocumentOnly(name, data), analysis.Span{End: len(data)})}
			}
			if !g.allowed(name) {
				issue("out_of_scope", "file is outside allowed scope")
				next++
				continue
			}
			if document.size() > g.opts.MaxDocumentBytes {
				return fmt.Errorf("%w: file %s exceeds byte limit", ErrBuildBudget, name)
			}
			if old, exists := staged[name]; exists {
				if !document.matches(old) {
					return fmt.Errorf("%w: %s", ErrSnapshotChanged, name)
				}
				delete(failures, name)
				next++
				continue
			}
			if parseErr, failed := parseFailures[name]; failed {
				issue("parse_error", parseErr.Error())
				next++
				continue
			}
			var budgetErr error
			if len(staged)+len(batch) >= g.opts.MaxDocuments {
				budgetErr = fmt.Errorf("%w: file limit %d", ErrBuildBudget, g.opts.MaxDocuments)
			} else if document.size() > g.opts.MaxSourceBytes-total-reserved {
				budgetErr = fmt.Errorf("%w: source byte limit", ErrBuildBudget)
			}
			if budgetErr != nil {
				if len(batch) == 0 {
					return budgetErr
				}
				// Pending parses may fail and free capacity. Finish them before
				// deciding whether this document exceeds the batch's budget.
				break
			}
			// Extraction results are passed directly across the phase boundary.
			facts, ok := prepared[name]
			if !ok {
				facts, ok = g.cachedFacts(document)
			} else {
				g.cachedFacts(document) // Release legacy exploration retention after admission.
			}
			if ok {
				staged[name] = facts
				total += document.size()
				next++
				continue
			}
			batch = append(batch, document)
			reserved += document.size()
			next++
		}
		results := g.extractBatch(ctx, batch)
		if err := ctx.Err(); err != nil {
			return err
		}
		// Merge in input order, independent of worker completion order. Assembly
		// and cross-document resolution only begin after all windows finish.
		for i, result := range results {
			name := batch[i].Path
			if result.err != nil {
				failures[name] = Diagnostic{Code: "parse_error", Message: result.err.Error(), Subject: DocumentSubject,
					Location: location(pipeline.DocumentOnly(name, batch[i].Content), analysis.Span{End: len(batch[i].Content)})}
				continue
			}
			staged[name] = result.facts
			total += int64(len(result.facts.Source) + len(result.facts.Gitlink))
			delete(failures, name)
		}
	}
	return ctx.Err()
}

type extractionResult struct {
	facts analysis.Facts
	err   error
}

// parseObserver, when set, receives the path of every document actually
// parsed in a batch. Test instrumentation for the Extract cache contract.
var parseObserver func(string)

func (g *Builder) extractBatch(ctx context.Context, documents []Document) []extractionResult {
	results := make([]extractionResult, len(documents))
	run := func(i int) {
		if err := ctx.Err(); err != nil {
			results[i].err = err
			return
		}
		document := documents[i]
		results[i].facts, results[i].err = g.extractor.extract(ctx, document)
	}
	if len(documents) == 1 {
		run(0)
		return results
	}
	var workers sync.WaitGroup
	for i := range documents {
		workers.Go(func() { run(i) })
	}
	// Parsers have individual timeouts. Join workers even on cancellation so no
	// parser, AST or source copy outlives this extraction window.
	workers.Wait()
	return results
}
