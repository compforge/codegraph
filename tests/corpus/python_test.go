package corpus_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	cg "github.com/compforge/codegraph"
)

var pythonExecutable = flag.String("python", "python3", "CPython executable for Python corpus syntax oracle")

func loadPythonOracle(ctx context.Context, root, sourceRoot, bindingsFile string) (*oracle, []cg.Document, []inputFile, string, error) {
	// Isolated mode prevents repository modules or PYTHONPATH from replacing stdlib
	// imports. The script only parses target source; it never imports the package.
	cmd := exec.CommandContext(ctx, *pythonExecutable, "-I", "python/oracle.py", root, sourceRoot, bindingsFile)
	out, err := cmd.Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			return nil, nil, nil, "", fmt.Errorf("Python oracle failed: %w: %s", err, failure.Stderr)
		}
		return nil, nil, nil, "", fmt.Errorf("Python oracle failed: %w", err)
	}
	var result struct {
		Oracle *oracle     `json:"oracle"`
		Inputs []inputFile `json:"inputs"`
		Python string      `json:"python"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, nil, nil, "", fmt.Errorf("Python oracle output: %w", err)
	}
	if result.Oracle == nil || result.Python == "" {
		return nil, nil, nil, "", fmt.Errorf("Python oracle missing required evidence")
	}
	var docs []cg.Document
	for _, input := range result.Inputs {
		if input.State != "included" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, input.Path))
		if err != nil {
			return nil, nil, nil, result.Python, err
		}
		docs = append(docs, cg.Document{Path: input.Path, Content: data})
	}
	return result.Oracle, docs, result.Inputs, result.Python, nil
}

// +case:id=python-corpus,expect=`Reviewed bindings cannot be replaced by graph output; unknown dispatch remains unassessed`
func TestPythonOracleScoring(t *testing.T) {
	root := fixtureRoot(t, map[string]string{"src/main.py": "def target(): pass\ndef wrong(): pass\ndef entry(callback):\n    target()\n    callback()\n"})
	golden := `[{"relation":"calls","source":{"path":"src/main.py","line":4,"text":"target()","name":"target"},"class":"internal","target":{"path":"src/main.py","qualifiedName":"target"},"reason":"Direct module function, no rebinding."},{"relation":"calls","source":{"path":"src/main.py","line":5,"text":"callback()","name":"callback"},"class":"runtime_dispatch","reason":"Callable supplied by caller."}]`
	name := filepath.Join(t.TempDir(), "bindings.json")
	if err := os.WriteFile(name, []byte(golden), 0644); err != nil {
		t.Fatal(err)
	}
	o, docs, _, version, err := loadPythonOracle(context.Background(), root, "src", name)
	if err != nil {
		t.Fatal(err)
	}
	if version == "" || len(docs) != 1 {
		t.Fatal(version, len(docs))
	}
	a, err := observe(context.Background(), "", "fixture", docs)
	if err != nil {
		t.Fatal(err)
	}
	e := evaluate(o, a)
	if e.Bindings[cg.Calls].Expected != 1 || e.Bindings[cg.Calls].Hit != 1 || e.Verdict != "measured" {
		t.Fatal(e)
	}
	var wrong cg.Node
	for _, n := range a.Nodes {
		if n.Name == "wrong" {
			wrong = n
		}
	}
	for i, r := range a.Relations {
		if r.Kind == cg.Calls && r.Confidence == cg.Exact {
			a.Relations[i].Target = wrong.ID
		}
	}
	bad := evaluate(o, a)
	if bad.Bindings[cg.Calls].ExactWrong != 1 || bad.Verdict != "failed" {
		t.Fatal(bad)
	}
	empty := evaluate(o, observed{})
	if empty.Bindings[cg.Calls].Expected != 1 || empty.Bindings[cg.Calls].Hit != 0 || empty.Unassessed["calls/runtime_dispatch"] != 1 {
		t.Fatal(empty)
	}
	if empty.Measurements["call_expressions/all"].Expected != 2 {
		t.Fatal(empty.Measurements)
	}
	if err := os.WriteFile(filepath.Join(root, "src/main.py"), []byte("def broken(:"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := loadPythonOracle(context.Background(), root, "src", name); err == nil {
		t.Fatal("accepted syntax failure as empty truth")
	}
}

func TestPythonImportSpanNormalization(t *testing.T) {
	o := &oracle{Declarations: map[string]declaration{}, Imports: map[string]occurrence{
		"a": {Site: site{"m.py", 7, 8}, Name: "a"}, "b": {Site: site{"m.py", 10, 11}, Name: "b"},
	}}
	a := observed{Facts: []cg.Facts{{Imports: []cg.FactImport{
		{Path: "a", Location: cg.Location{Path: "m.py", StartByte: 0, EndByte: 11}},
		{Path: "b", Location: cg.Location{Path: "m.py", StartByte: 0, EndByte: 11}},
	}}}}
	e := evaluate(o, a)
	if m := e.Measurements["import_facts/all"]; m.Found != 2 || m.Unexpected != 0 {
		t.Fatal(m)
	}
}
