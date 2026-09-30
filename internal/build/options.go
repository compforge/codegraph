package build

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"time"

	"github.com/compforge/codegraph/internal/extract"
)

// Options bounds a graph's build and query work. Zero values select finite defaults.
// Scope contains snapshot-relative, slash-separated document paths or directory prefixes.
// Empty Scope allows any relative path; documents are only analyzed when supplied.
type Options struct {
	ResolutionContext ResolutionContext
	// ExtractionCache optionally shares raw facts across snapshots; graph budgets
	// and relationship binding still apply independently to each Graph.
	ExtractionCache *extract.ExtractionCache
	// BuildConcurrency bounds parallel document extraction within a batch.
	// Zero selects min(GOMAXPROCS, 4); one extracts serially.
	BuildConcurrency                     int
	ModulePath                           string
	Scope                                []string
	MaxDocuments, MaxNodes, MaxRelations int
	MaxEvidence                          int
	MaxDocumentBytes, MaxSourceBytes     int64
	ParseTimeout, QueryTimeout           time.Duration
	MaxQueryHops, MaxResultRows          int
	MaxResultBytes                       int64
}

func defaults(o *Options) error {
	for _, pair := range []struct {
		v   *int
		def int
	}{{&o.BuildConcurrency, min(runtime.GOMAXPROCS(0), 4)}, {&o.MaxDocuments, 256}, {&o.MaxNodes, 50000}, {&o.MaxRelations, 100000}, {&o.MaxEvidence, 1000000}, {&o.MaxQueryHops, 8}, {&o.MaxResultRows, 1000}} {
		if *pair.v < 0 {
			return errors.New("limits must not be negative")
		}
		if *pair.v == 0 {
			*pair.v = pair.def
		}
	}
	for _, pair := range []struct {
		v   *int64
		def int64
	}{{&o.MaxDocumentBytes, 2 << 20}, {&o.MaxSourceBytes, 32 << 20}, {&o.MaxResultBytes, 8 << 20}} {
		if *pair.v < 0 {
			return errors.New("limits must not be negative")
		}
		if *pair.v == 0 {
			*pair.v = pair.def
		}
	}
	if o.ParseTimeout < 0 || o.QueryTimeout < 0 {
		return errors.New("timeouts must not be negative")
	}
	if o.ParseTimeout == 0 {
		o.ParseTimeout = 2 * time.Second
	}
	if o.QueryTimeout == 0 {
		o.QueryTimeout = 5 * time.Second
	}
	o.Scope = append([]string(nil), o.Scope...)
	for _, p := range o.Scope {
		if !fs.ValidPath(p) {
			return fmt.Errorf("invalid scope %q", p)
		}
	}
	return nil
}
