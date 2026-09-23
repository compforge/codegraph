package codegraph

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

func TestBuildConcurrencyOptions(t *testing.T) {
	if _, err := New("rev", Options{BuildConcurrency: -1}); err == nil {
		t.Fatal("negative concurrency accepted")
	}
	g, err := New("rev", Options{})
	if err != nil || g.opts.BuildConcurrency != min(runtime.GOMAXPROCS(0), 4) {
		t.Fatalf("automatic concurrency: %v, %v", g, err)
	}
}

func TestParallelDocumentsEquivalent(t *testing.T) {
	docs := documents(fixture(), "main.go", "helper.go", "lib/work.go", "entry_test.go")
	docs = append(docs,
		Document{Path: "worker.py", Content: []byte("def work():\n    pass\ndef entry():\n    work()\n")},
		Document{Path: "app.ts", Content: []byte("function work() {}\nfunction entry() { work(); }\n")},
		Document{Path: "bad.go", Content: []byte("package p\nfunc (")},
		docs[0],
	)
	var baseline *Graph
	for _, concurrency := range []int{1, 2, 4, 8} {
		g, _, err := Build(context.Background(), "rev", docs, Options{BuildConcurrency: concurrency, ModulePath: "example.org/demo"})
		if err != nil {
			t.Fatal(err)
		}
		if baseline == nil {
			baseline = g
		} else if !reflect.DeepEqual(g.Nodes(), baseline.Nodes()) || !reflect.DeepEqual(g.Relations(), baseline.Relations()) || !reflect.DeepEqual(g.Report(), baseline.Report()) {
			t.Fatalf("concurrency %d changed facts or diagnostics", concurrency)
		}
	}
}

func TestParallelFailedParseReleasesBudget(t *testing.T) {
	good := []byte("package p\nfunc Work() {}\n")
	docs := []Document{{Path: "bad.go", Content: []byte("package p\nfunc (")},
		{Path: "a.go", Content: good}, {Path: "b.go", Content: good}}
	for _, concurrency := range []int{1, 4} {
		g, report, err := Build(context.Background(), "rev", docs, Options{
			BuildConcurrency: concurrency, MaxFiles: 2, MaxSourceBytes: int64(2 * len(good)),
		})
		if err != nil || len(report.Files) != 2 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "parse_error" {
			t.Fatalf("concurrency %d: %+v, %v", concurrency, report, err)
		}
		nodes, relations := g.Nodes(), g.Relations()
		if _, err := g.addDocumentsSync(context.Background(), Document{Path: "c.go", Content: good}); !errors.Is(err, ErrBuildBudget) {
			t.Fatalf("expected exhausted budget: %v", err)
		}
		if !reflect.DeepEqual(report, g.Report()) || !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(relations, g.Relations()) {
			t.Fatal("budget failure published staged facts")
		}
	}
}

func TestParallelExtractionBoundAndAtomicPublication(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", canceled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{}, 10)
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			var active, peak atomic.Int32
			entry := *grammars.DetectLanguageByName("python")
			loader := entry.Language
			ext := fmt.Sprintf(".cgparallel%v", canceled)
			entry.Name, entry.Extensions = "codegraph-parallel-"+ext, []string{ext}
			entry.Language = func() *gts.Language {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				started <- struct{}{}
				<-release
				return loader()
			}
			grammars.Register(entry)
			g, _, err := Build(ctx, "rev", []Document{{Path: "seed.go", Content: []byte("package p\nfunc Seed() {}")}}, Options{BuildConcurrency: 3})
			if err != nil {
				t.Fatal(err)
			}
			nodes, report := g.Nodes(), g.Report()
			var docs []Document
			for i := 0; i < 7; i++ {
				docs = append(docs, Document{Path: fmt.Sprintf("f%d%s", i, ext), Content: []byte("def work():\n    pass\n")})
			}
			done := make(chan error, 1)
			go func() { _, err := g.addDocumentsSync(ctx, docs...); done <- err }()
			for i := 0; i < 3; i++ {
				select {
				case <-started:
				case <-time.After(10 * time.Second):
					t.Fatal("document workers did not run concurrently")
				}
			}
			if !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(report, g.Report()) {
				t.Fatal("in-flight extraction changed published graph")
			}
			if rows := query(t, g, "MATCH (n:Function {name:'Seed'}) RETURN n", nil); len(rows) != 1 {
				t.Fatal("old graph unavailable during build")
			}
			if canceled {
				cancel()
			}
			unblock()
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("workers did not finish")
			}
			if canceled {
				// Wait cancellation is independent of background worker lifetime.
				g.asyncMu.Lock()
				work := g.latestWork
				g.asyncMu.Unlock()
				select {
				case <-work.done:
				case <-time.After(10 * time.Second):
					t.Fatal("canceled build did not stop its workers")
				}
			}
			if peak.Load() != 3 || active.Load() != 0 {
				t.Fatalf("worker bound/lifetime: peak=%d active=%d", peak.Load(), active.Load())
			}
			if canceled {
				if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(nodes, g.Nodes()) || !reflect.DeepEqual(report, g.Report()) {
					t.Fatalf("canceled batch changed graph: %v", err)
				}
			} else if err != nil || len(g.Report().Files) != 8 {
				t.Fatalf("batch did not publish: %+v, %v", g.Report(), err)
			}
		})
	}
}

// BenchmarkDocumentBuild compares the same mixed-language workset across worker
// counts, including resolution and graph storage rather than parser time alone.
func BenchmarkDocumentBuild(b *testing.B) {
	var docs []Document
	for i := 0; i < 48; i++ {
		docs = append(docs,
			Document{Path: fmt.Sprintf("p%d/app.go", i), Content: []byte("package p\nfunc Work() {}\nfunc Entry() { Work() }\n")},
			Document{Path: fmt.Sprintf("p%d/app.py", i), Content: []byte("def work():\n    pass\ndef entry():\n    work()\n")},
			Document{Path: fmt.Sprintf("p%d/app.ts", i), Content: []byte("function work() {}\nfunction entry() { work(); }\n")},
		)
	}
	// Grammar loading is process-wide and lazy; warm every language before
	// comparing worker counts so the first configuration does not pay alone.
	if _, _, err := Build(context.Background(), "warmup", docs, Options{BuildConcurrency: 1}); err != nil {
		b.Fatal(err)
	}
	for _, concurrency := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers=%d", concurrency), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, report, err := Build(context.Background(), "bench", docs, Options{BuildConcurrency: concurrency})
				if err != nil || !report.Complete {
					b.Fatalf("%+v, %v", report, err)
				}
			}
		})
	}
}
