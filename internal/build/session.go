package build

import (
	"sync"

	"github.com/alitto/pond/v2"
	"github.com/compforge/codegraph/internal/extract"
)

// Session coordinates the legacy document submission API. Plain builders never
// allocate its parser, task pool, pending batches or exploration cache.
type Session struct {
	*Builder

	extractor      *extract.Extractor
	asyncMu        sync.Mutex
	asyncPool      pond.ResultPool[extract.Facts]
	building       bool
	pending        []extract.Document
	pendingWorkers *sync.WaitGroup
	pendingWorks   []*buildWork
	latestWork     *buildWork
	documentTasks  map[string]documentTask
	factCacheMu    sync.Mutex
	factCache      map[string]factCacheEntry
}

func NewSession(b *Builder) (*Session, error) {
	e, err := extract.NewExtractor(extract.ExtractionOptions{Concurrency: b.opts.BuildConcurrency, MaxDocumentBytes: b.opts.MaxDocumentBytes, ParseTimeout: b.opts.ParseTimeout, Cache: b.opts.ExtractionCache})
	if err != nil {
		return nil, err
	}
	return &Session{Builder: b, extractor: e, documentTasks: map[string]documentTask{}, factCache: map[string]factCacheEntry{}}, nil
}

// Done observes the last admitted publication without triggering or waiting for it.
func (s *Session) Done() <-chan struct{} {
	s.asyncMu.Lock()
	defer s.asyncMu.Unlock()
	if s.latestWork == nil {
		return nil
	}
	return s.latestWork.done
}
