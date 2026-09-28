package corpus_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	cg "github.com/compforge/codegraph"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var nodeExecutable = flag.String("node", "node", "Node executable for the fixed TypeScript compiler oracle")

type compilerDiagnostic struct {
	Project string `json:"project"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Site    *site  `json:"site,omitempty"`
}

func loadTypeScriptOracle(ctx context.Context, root, profile string) (*oracle, []cg.Document, []inputFile, string, error) {
	cmd := exec.CommandContext(ctx, *nodeExecutable, "typescript/oracle.mjs", root, profile)
	out, err := cmd.Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			return nil, nil, nil, "", fmt.Errorf("TypeScript oracle: %w: %s", err, failure.Stderr)
		}
		return nil, nil, nil, "", fmt.Errorf("TypeScript oracle: %w", err)
	}
	var result struct {
		Oracle      *oracle              `json:"oracle"`
		Inputs      []inputFile          `json:"inputs"`
		Toolchain   string               `json:"toolchain"`
		Diagnostics []compilerDiagnostic `json:"diagnostics"`
	}
	if err = json.Unmarshal(out, &result); err != nil {
		return nil, nil, nil, "", err
	}
	if result.Oracle == nil || result.Toolchain == "" {
		return nil, nil, nil, "", fmt.Errorf("TypeScript oracle omitted required evidence")
	}
	result.Oracle.Diagnostics = result.Diagnostics
	var docs []cg.Document
	for _, input := range result.Inputs {
		if input.State != "included" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, input.Path))
		if err != nil {
			return nil, nil, nil, result.Toolchain, err
		}
		docs = append(docs, cg.Document{Path: input.Path, Content: data})
	}
	return result.Oracle, docs, result.Inputs, result.Toolchain, nil
}

// +case=`Compiler-bound aliases are independent of graph output; callbacks and missing dependencies remain unassessed`
func TestTypeScriptOracleScoring(t *testing.T) {
	root := fixtureRoot(t, map[string]string{
		"tsconfig.json": `{"compilerOptions":{"target":"ES2022","module":"ESNext","moduleResolution":"bundler","strict":true},"include":["src"]}`,
		"src/lib.ts":    "export function target() {}\nexport function wrong() {}\n",
		"src/app.ts":    "import {target as run} from './lib';\nexport function entry(callback: () => void) { run(); callback(); }\n",
	})
	profile := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(profile, []byte(`{"projects":[{"config":"tsconfig.json","sourceRoot":"src"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	o, docs, _, version, err := loadTypeScriptOracle(context.Background(), root, profile)
	if err != nil {
		t.Fatal(err)
	}
	if version == "" || len(docs) != 2 {
		t.Fatal(version, len(docs))
	}
	a, err := observe(context.Background(), "", "fixture", docs)
	if err != nil {
		t.Fatal(err)
	}
	e := evaluate(o, a)
	if e.Bindings[cg.Calls].Expected != 1 || e.Bindings[cg.Calls].Hit != 1 || e.Verdict != "measured" {
		t.Fatalf("unexpected compiler comparison: %+v; findings=%v", e, e.Findings)
	}
	for i, edge := range a.Relations {
		if edge.Kind == cg.Calls && edge.Confidence == cg.Exact {
			for _, n := range a.Nodes {
				if n.Name == "wrong" {
					a.Relations[i].Target = n.ID
				}
			}
		}
	}
	if bad := evaluate(o, a); bad.Bindings[cg.Calls].ExactWrong != 1 || bad.Verdict != "failed" {
		t.Fatal(bad)
	}
	empty := evaluate(o, observed{})
	if empty.Bindings[cg.Calls].Expected != 1 || empty.Unassessed["calls/runtime_dispatch"] != 1 {
		t.Fatal(empty)
	}
	if err := os.WriteFile(filepath.Join(root, "src/lib.ts"), []byte("export function {"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err = loadTypeScriptOracle(context.Background(), root, profile); err == nil {
		t.Fatal("accepted compiler parse failure")
	}
}
