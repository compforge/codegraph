package corpus_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	cg "github.com/compforge/codegraph"
	"golang.org/x/mod/modfile"
)

func TestManifestNativeOracles(t *testing.T) {
	for _, source := range []string{
		"module example.org/demo\n\ngo 1.24\nrequire example.org/lib v1.2.3\n",
		"// comment\nmodule \"example.org/demo/v2\"\nreplace example.org/lib => ../lib\n",
	} {
		expected, err := modfile.Parse("go.mod", []byte(source), nil)
		if err != nil {
			t.Fatal(err)
		}
		got := manifestMetadata(t, "go.mod", source)
		if got.Name != expected.Module.Mod.Path {
			t.Fatalf("got %q, native parser %q", got.Name, expected.Module.Mod.Path)
		}
	}
	for _, source := range []string{
		"[project]\nname='demo'\nversion='1.2.3'\n",
		"project = {name='demo', version='1.2.3'}\n",
		"project.name='demo'\nproject.version='1.2.3'\n",
		"[\"project\"]\n'name'='demo'\n\"version\"='1.2.3'\n",
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, *pythonExecutable, "-I", "-c", `import json,sys,tomllib; print(json.dumps(tomllib.loads(sys.stdin.read())["project"]))`)
		cmd.Stdin = strings.NewReader(source)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("tomllib oracle: %v: %s", err, out)
		}
		var expected struct{ Name, Version string }
		if err := json.Unmarshal(out, &expected); err != nil {
			t.Fatal(err)
		}
		got := manifestMetadata(t, "pyproject.toml", source)
		if got.Name != expected.Name || got.Version != expected.Version {
			t.Fatalf("got %+v, tomllib %+v", got, expected)
		}
	}
	source := `{"name":"@app/dem\u006f","version":"1.2.3"}`
	var expected struct{ Name, Version string }
	if err := json.Unmarshal([]byte(source), &expected); err != nil {
		t.Fatal(err)
	}
	got := manifestMetadata(t, "package.json", source)
	if got.Name != expected.Name || got.Version != expected.Version {
		t.Fatalf("got %+v, JSON oracle %+v", got, expected)
	}
}

func manifestMetadata(t *testing.T, path, source string) *cg.ManifestMetadata {
	t.Helper()
	g, r, err := cg.Build(context.Background(), "oracle", []cg.Document{{Path: path, Content: []byte(source)}}, cg.Options{})
	if err != nil || len(r.Diagnostics) != 0 {
		t.Fatalf("%v %+v", err, r)
	}
	d, ok := g.Document(path)
	if !ok || d.Manifest == nil {
		t.Fatal("missing manifest", d)
	}
	return d.Manifest
}
