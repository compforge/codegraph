package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
)

// +case:id=publication_parity,desc=`Pin semantic publication identities across language and admission modes`
func TestPublicationParity(t *testing.T) {
	cases := []struct {
		name string
		docs []Document
	}{
		{"ts", []Document{{Path: "target.ts", Content: []byte(`export function run() { return 1; }`)}, {Path: "app.ts", Content: []byte(`import {run} from './target'; export function handle() { const local = run(); return local; } class Box { value = run(); }`)}}},
		{"python", []Document{{Path: "lib.py", Content: []byte("def run():\n    return 1\n")}, {Path: "app.py", Content: []byte("from lib import run\ndef wrap(fn):\n    return fn\n@wrap\ndef handle():\n    local = run()\n    return local\n")}}},
		{"go", []Document{{Path: "app.go", Content: []byte("package app\ntype Box struct { Value int }\nfunc Run() int { return 1 }\nfunc Handle(b Box) int { local := Run(); return local + b.Value }\n")}}},
	}
	policies := []struct {
		name string
		p    *AdmissionPolicy
	}{{"full", nil}, {"analysis", AnalysisAdmission()}, {"references", &AdmissionPolicy{NodeKinds: []NodeKind{ReadSite, WriteSite, CallSite, TypeUse, CaptureSite, ResourceUse, DecoratorUse}}}, {"directories", &AdmissionPolicy{NodeKinds: []NodeKind{DirectoryNodeKind}}}, {"functions", &AdmissionPolicy{NodeKinds: []NodeKind{Function}}}}
	expected := map[string]string{"ts/full": "0dbc0a7a27ba001576b2bd509f65d3ebaf877e03f83e1adf49525f5892378c6e", "ts/analysis": "53f7c80833ec252b847def683dcef79c533827eea614801bd2eb3bfd3104c7b7", "ts/references": "9d17b3bc61994f5b7575f8349808deaf42974c073caf3d9bb62b6961a4b7b05d", "ts/directories": "36d3c392e384bb5468a6d200cd6b8f8add40fce25d24f81b6b654c93d6404154", "ts/functions": "73e61e50952832d26e0350eb99b2af3e556e440f4d2d7b3cd9dc30c7a6d4f705", "python/full": "80e1e894ad5754e0366830ecb67424c9f9bdbe52228f43d7e07356e1ce9c2808", "python/analysis": "8aee8b6b4b844a214fda1be63be9b10a6cc0cc7814415ae562380a7f4d6bd6c9", "python/references": "876bdf602b851459ccc0d0e6671633684e311d210a8c62c8ae54e5f4f3523d60", "python/directories": "36d3c392e384bb5468a6d200cd6b8f8add40fce25d24f81b6b654c93d6404154", "python/functions": "2f1f8ff0175812f12f7b9e693330b6ad80588ba195af0a0dc49494708330200f", "go/full": "c016f7cc2150d73ad35368a33973cd05ee3ef9da9dccd8e0148d77bd22b6f268", "go/analysis": "f6b715bb0543c4a0fcf1630e473ee27e8968fcf041ed7da1091cea8748f4dd3f", "go/references": "6cbaf83ad87d95d6e36ba1a7a842b697dcda7c435a23cecdfe89721ac6c55aab", "go/directories": "36d3c392e384bb5468a6d200cd6b8f8add40fce25d24f81b6b654c93d6404154", "go/functions": "619bbb7dda169f225ffd2ace1b0512d99e584427dc40ca44dfb10ecd89f94fba"}
	for _, tc := range cases {
		for _, policy := range policies {
			t.Run(tc.name+"/"+policy.name, func(t *testing.T) {
				g, r, err := Build(context.Background(), "parity", tc.docs, Options{Admission: policy.p})
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(struct {
					Nodes       []Node
					Relations   []Relation
					Diagnostics []Diagnostic
				}{g.Nodes(), g.Relations(), r.Diagnostics})
				if err != nil {
					t.Fatal(err)
				}
				if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != expected[tc.name+"/"+policy.name] {
					t.Fatalf("publication changed: got %s, want %s", got, expected[tc.name+"/"+policy.name])
				}
			})
		}
	}
}
