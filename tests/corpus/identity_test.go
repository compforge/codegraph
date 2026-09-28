package corpus_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// The oracle owns this import root and its transitive modules. Subject-only
// dependencies (the parser and graph engine) must not invalidate the evaluator.
// Both identities retain shared dependencies when their import closures overlap.
func evaluatorDependencies(t *testing.T) []string {
	t.Helper()
	data, err := exec.Command("go", "list", "-deps", "-json", "golang.org/x/tools/go/packages").Output()
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var result []string
	for {
		var p dependencyPackage
		if err := decoder.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if p.Module == nil {
			continue
		} // standard library identity is the toolchain
		identity, err := dependencyIdentity(p)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, identity)
	}
	sort.Strings(result)
	return result
}

type dependencyModule struct {
	Path, Version, Sum string
	Replace            *dependencyModule
}
type dependencyPackage struct {
	ImportPath, Dir               string
	GoFiles, CgoFiles, EmbedFiles []string
	Module                        *dependencyModule
}

func dependencyIdentity(p dependencyPackage) (string, error) {
	h := sha256.New()
	files := append(append(append([]string{}, p.GoFiles...), p.CgoFiles...), p.EmbedFiles...)
	sort.Strings(files)
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(p.Dir, name))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%x\n", name, sha256.Sum256(data))
	}
	module := p.Module
	version := module.Path + "@" + module.Version + "#" + module.Sum
	if module.Replace != nil {
		r := module.Replace
		version += "=>" + r.Version + "#" + r.Sum
	} // local replacement content is hashed above
	return fmt.Sprintf("%s %s source:%x", p.ImportPath, version, h.Sum(nil)), nil
}
func TestEvaluatorDependencyContentIdentity(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "oracle.go")
	if err := os.WriteFile(file, []byte("package oracle"), 0600); err != nil {
		t.Fatal(err)
	}
	p := dependencyPackage{ImportPath: "oracle", Dir: root, GoFiles: []string{"oracle.go"}, Module: &dependencyModule{Path: "oracle", Replace: &dependencyModule{Path: root}}}
	before, err := dependencyIdentity(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("package oracle;const changed=true"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := dependencyIdentity(p)
	if err != nil || before == after {
		t.Fatal("local oracle replacement drift hidden", err)
	}
}

func TestIdentitySeparatesSubjectFromOracle(t *testing.T) {
	base := runReport{SchemaVersion: 3, Status: "measured", Evaluation: &evaluation{}, Evaluator: EvaluatorIdentity{SourceSHA256: "oracle", Dependencies: []string{"tools@v1"}}}
	changed := base
	changed.Subject = SubjectIdentity{Revision: "new", BuildInfo: "parser@new"}
	if _, err := compareBaseline(base, changed); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*runReport){
		func(r *runReport) { r.Input.Profile = "other" },
		func(r *runReport) { r.Evaluator.SourceSHA256 = "other" },
		func(r *runReport) { r.Evaluator.Toolchain = "other" },
		func(r *runReport) { r.Evaluator.Dependencies = []string{"tools@v2"} },
		func(r *runReport) { r.SchemaVersion++ },
	} {
		changed = base
		mutate(&changed)
		if _, err := compareBaseline(base, changed); err == nil {
			t.Fatal("accepted drift")
		}
	}
}
