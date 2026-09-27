package corpus_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	cg "github.com/compforge/codegraph"
)

func fixtureRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	files["go.mod"] = "module example.org/corpus\n\ngo 1.26.0\n"
	for name, source := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// +spec=`Removing emitted facts cannot shrink the oracle denominator or turn unknown dispatch into a pass`
func TestIndependentOracleAndScoring(t *testing.T) {
	root := fixtureRoot(t, map[string]string{
		"p.go": `package corpus
type Box[T any] struct { Value T }
type Runner interface { Run() }
func Target() {}
func Wrong() {}
func Entry(r Runner) { Target(); f:=Target; f(); r.Run() }
var A,B = 1,2
`,
		"ignored_test.go":     "package corpus\nfunc OnlyInTest() {}\n",
		"platform_windows.go": "package corpus\nfunc WindowsOnly() {}\n",
	})
	o, docs, inventory, err := loadOracle(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || len(inventory) != 3 {
		t.Fatal(docs, inventory)
	}
	var dynamic, grouped int
	for _, c := range o.Calls {
		if c.Class == "runtime_dispatch" {
			dynamic++
		}
	}
	for _, d := range o.Declarations {
		if d.Reason == "multi_name_value_declaration" {
			grouped++
		}
	}
	if dynamic != 2 || grouped != 2 {
		t.Fatalf("dynamic=%d grouped=%d", dynamic, grouped)
	}
	a, err := observe(context.Background(), o.Module, "fixture", docs)
	if err != nil {
		t.Fatal(err)
	}
	e := evaluate(o, a)
	if e.Bindings[cg.Calls].Expected != 1 || e.Bindings[cg.Calls].Hit != 1 || e.Bindings[cg.Calls].ExactWrong != 0 {
		t.Fatal(e.Bindings)
	}
	var wrong cg.Node
	for _, n := range a.Nodes {
		if n.Name == "Wrong" {
			wrong = n
		}
	}
	mutated := a
	mutated.Relations = append([]cg.Relation(nil), a.Relations...)
	for i, r := range mutated.Relations {
		if r.Kind == cg.Calls && r.Confidence == cg.Exact {
			mutated.Relations[i].Target = wrong.ID
		}
	}
	bad := evaluate(o, mutated)
	if bad.Bindings[cg.Calls].ExactWrong != 1 || bad.Verdict != "failed" {
		t.Fatal(bad.Bindings)
	}
	empty := evaluate(o, observed{})
	if empty.Measurements["declarations/all"].Expected != e.Measurements["declarations/all"].Expected || empty.Bindings[cg.Calls].Expected != 1 || empty.Bindings[cg.Calls].SilentMissing != 1 {
		t.Fatal(empty)
	}
	if empty.Measurements["declarations/all"].Found != 0 {
		t.Fatal(empty)
	}
}

func TestOracleLoadFailureIsNotEmptyGroundTruth(t *testing.T) {
	root := fixtureRoot(t, map[string]string{"p.go": "package corpus\nfunc Broken(){ missing() }\n"})
	if _, _, _, err := loadOracle(context.Background(), root); err == nil {
		t.Fatal("invalid package accepted as oracle")
	}
}

func TestArchiveRejectsEscapeAndLinks(t *testing.T) {
	for _, h := range []*tar.Header{{Name: "root/../escape", Mode: 0644, Typeflag: tar.TypeReg}, {Name: "root/link", Linkname: "/tmp", Typeflag: tar.TypeSymlink}} {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		if err := unpack(buf.Bytes(), t.TempDir()); err == nil {
			t.Fatalf("accepted %s", h.Name)
		}
	}
}

func TestOracleDistinguishesFieldKeysAndNestedCalls(t *testing.T) {
	root := fixtureRoot(t, map[string]string{
		"p.go": `package corpus
type Role string
type Message struct { Role Role }
func New() Message { return Message{Role: Role("user")} }
func (Message) Done() {}
func Entry() { New().Done() }
`,
		"lib/lib.go": "package lib\nfunc Work() {}\n",
		"use.go":     "package corpus\nimport other \"example.org/corpus/lib\"\nfunc Use(){ other.Work() }\n",
	})
	o, _, _, err := loadOracle(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var fieldKey, nestedCall, imported bool
	for _, r := range o.References {
		if r.Name == "Role" && o.Declarations[r.Target].Kind == cg.Field {
			fieldKey = true
		}
	}
	for _, c := range o.Calls {
		if c.Name == "Done" {
			nestedCall = o.Declarations[c.Target].Name == "Done"
		}
		if c.Name == "Work" {
			imported = o.Declarations[c.Target].Span.Path == "lib/lib.go"
		}
	}
	if !fieldKey || !nestedCall || !imported {
		t.Fatalf("field=%v nested=%v import=%v", fieldKey, nestedCall, imported)
	}
}
