package build

import (
	"runtime"
	"testing"
)

func TestBuildConcurrencyOptions(t *testing.T) {
	if _, err := NewBuilder("rev", Options{BuildConcurrency: -1}); err == nil {
		t.Fatal("negative concurrency accepted")
	}
	g, err := NewBuilder("rev", Options{})
	if err != nil || g.opts.BuildConcurrency != min(runtime.GOMAXPROCS(0), 4) {
		t.Fatalf("automatic concurrency: %v, %v", g, err)
	}
}
