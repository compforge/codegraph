package pipeline

import (
	"context"
	"testing"
	"time"
)

// BenchmarkAnalyze includes parsing and detached fact extraction. Repeating the
// source measures compiled-program reuse without a document Facts cache.
func BenchmarkAnalyze(b *testing.B) {
	for _, tc := range []struct{ path, source string }{
		{"app.go", "package p\nfunc Work() {}\nfunc Entry() { Work() }\n"},
		{"app.py", "def work():\n    pass\ndef entry():\n    work()\n"},
		{"app.ts", "function work() {}\nfunction entry() { work(); }\n"},
		{"app.rs", "fn work() {}\nfn entry() { work(); }\n"},
	} {
		b.Run(tc.path, func(b *testing.B) {
			source := []byte(tc.source)
			if _, err := Analyze(context.Background(), tc.path, source, 5*time.Second); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				facts, err := Analyze(context.Background(), tc.path, source, 5*time.Second)
				if err != nil || len(facts.Declarations) != 2 {
					b.Fatalf("declarations=%d, error=%v", len(facts.Declarations), err)
				}
			}
		})
	}
}
