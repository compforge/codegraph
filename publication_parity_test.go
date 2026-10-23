package codegraph

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"
)

// +case:id=publication_parity,desc=`Preserve pre-selection node and relation identities across language and admission modes`
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
	}{{"full", nil}, {"analysis", AnalysisAdmission()}, {"references", &AdmissionPolicy{NodeKinds: []NodeKind{Reference}}}, {"directories", &AdmissionPolicy{NodeKinds: []NodeKind{DirectoryNodeKind}}}, {"functions", &AdmissionPolicy{NodeKinds: []NodeKind{Function}}}}
	expected := map[string]string{"ts/full": "e8b4efa06cddd17e97d27995c08401fea3fc1b87c77a670e1798a2f5e9d92319", "ts/analysis": "47a82290a4bb9666f8bdb9091d6cc46a069f89a85ac64d68bbb7c0ae6d3be3b0", "ts/references": "bde9fa46d6d1b16e7afc77ee3e61b2ad7ef77247582fe1b6d6715e81743fccdc", "ts/directories": "36d3c392e384bb5468a6d200cd6b8f8add40fce25d24f81b6b654c93d6404154", "ts/functions": "73e61e50952832d26e0350eb99b2af3e556e440f4d2d7b3cd9dc30c7a6d4f705", "python/full": "d1ac378b0859cc80375ecff796b86fe49683ed9c0324c180e6c1f0eca5b76e03", "python/analysis": "d1937099f9a578095bf8964807b9c40d0c803e3f9b6b0e839fc169459ca7dd93", "python/references": "c80ab6a73e934526eff771387d9b4a6117b65d8aeb46048fad7086e4d359cfb3", "python/directories": "e265c615034353b63bfd4ee39eb2771cf9898c8fc02a4a0564d2eebcfb5ac321", "python/functions": "f458edf3349ecd531a357ce3125ab64f4228bd0ae93114a610844235b768288d", "go/full": "ca0c8d2a2cee13297f13010f368874955eb80543d6921826702b1ddfd1bcfd9e", "go/analysis": "3a484849ea509f2236e6e026e09afe69aed7a7e237b1983ad40d33a372df9648", "go/references": "d707dd9cb814a41c0ff46e0d25723dcb4507247472b5d64fdf39aae03742ece9", "go/directories": "36d3c392e384bb5468a6d200cd6b8f8add40fce25d24f81b6b654c93d6404154", "go/functions": "49e912bb050c6dfd846d7e90bd0e3cfc50b800898a362f719f273d9cee97c19b"}
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
