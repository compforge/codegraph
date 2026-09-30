package extract

import (
	"errors"
	"sync"

	"github.com/compforge/codegraph/internal/analysis"
)

// ExtractionCache reuses detached single-document facts across Graphs. It never
// retains bound relationships or graph state. The caller owns its lifetime;
// keep the grammar registry unchanged while sharing it. It is safe for concurrent
// Graphs, but concurrent misses may extract independently.
// Capacity bounds retained document versions and their source bytes, not total
// heap usage. A full cache skips new entries without changing build behavior.
type ExtractionCache struct {
	mu                          sync.Mutex
	entries                     map[extractionKey]analysis.Facts
	maxDocuments                int
	maxSourceBytes, sourceBytes int64
}

type extractionKey struct {
	path   string
	digest [32]byte
}

// NewExtractionCache creates a bounded, in-memory extraction cache. Zero limits
// select 256 document versions and 32 MiB of source; negative limits are invalid.
func NewExtractionCache(maxDocuments int, maxSourceBytes int64) (*ExtractionCache, error) {
	if maxDocuments < 0 || maxSourceBytes < 0 {
		return nil, errors.New("cache limits must not be negative")
	}
	if maxDocuments == 0 {
		maxDocuments = 256
	}
	if maxSourceBytes == 0 {
		maxSourceBytes = 32 << 20
	}
	return &ExtractionCache{entries: map[extractionKey]analysis.Facts{}, maxDocuments: maxDocuments, maxSourceBytes: maxSourceBytes}, nil
}

func (c *ExtractionCache) get(key extractionKey) (analysis.Facts, bool) {
	if c == nil {
		return analysis.Facts{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	facts, ok := c.entries[key]
	return facts, ok
}

func (c *ExtractionCache) put(key extractionKey, facts analysis.Facts) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		return
	}
	size := int64(len(facts.Source) + len(facts.Gitlink))
	if len(c.entries) >= c.maxDocuments || size > c.maxSourceBytes-c.sourceBytes {
		return
	}
	// Raw facts are immutable after extraction. Each Graph creates its own
	// binding index, and the public Facts projection returns independent values.
	c.entries[key] = facts
	c.sourceBytes += size
}
