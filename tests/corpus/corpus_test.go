package corpus_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	cg "github.com/compforge/codegraph"
)

var corpusNames = flag.String("corpus", "", "pinned repositories: all or comma-separated names; empty skips network corpus")
var reportDir = flag.String("report-dir", "../../.corpus-results", "corpus evidence output directory")
var baselineDir = flag.String("baseline-dir", "", "optional previously reviewed report directory for regression comparison")

type InputIdentity struct {
	Repository repository  `json:"repository"`
	Profile    string      `json:"profile"`
	Files      []inputFile `json:"files"`
}
type EvaluatorIdentity struct {
	SourceSHA256 string   `json:"sourceSHA256"`
	Toolchain    string   `json:"toolchain"`
	Dependencies []string `json:"dependencies"`
}
type SubjectIdentity struct {
	Revision     string `json:"revision"`
	DiffSHA256   string `json:"diffSHA256"`
	SourceSHA256 string `json:"sourceSHA256"`
	BuildInfo    string `json:"buildInfo"`
}
type artifactOrigin struct {
	SchemaVersion int    `json:"schemaVersion"`
	ReportSHA256  string `json:"reportSHA256"`
	GraphSHA256   string `json:"graphSHA256"`
}
type runReport struct {
	ReevaluatedFrom   *artifactOrigin   `json:"reevaluatedFrom,omitempty"`
	SchemaVersion     int               `json:"schemaVersion"`
	Input             InputIdentity     `json:"input"`
	Evaluator         EvaluatorIdentity `json:"evaluator"`
	Subject           SubjectIdentity   `json:"subject"`
	Status            string            `json:"status"`
	Error             string            `json:"error,omitempty"`
	Documents         int               `json:"documents"`
	OracleDiagnostics int               `json:"oracleDiagnostics,omitempty"`
	Regressions       []string          `json:"regressions,omitempty"`
	Evaluation        *evaluation       `json:"evaluation,omitempty"`
}

// +case:id=go-corpus,expect=`Pinned source facts are measured against compiler facts, with gaps and unknowns retained`
func TestRepositories(t *testing.T) {
	if *corpusNames == "" {
		t.Skip("opt-in network corpus: make test-corpus")
	}
	for _, sourceDir := range []string{*baselineDir, *reevaluateDir} {
		if sourceDir == "" {
			continue
		}
		base, err := filepath.Abs(sourceDir)
		if err != nil {
			t.Fatal(err)
		}
		out, err := filepath.Abs(*reportDir)
		if err != nil {
			t.Fatal(err)
		}
		if resolved, err := filepath.EvalSymlinks(base); err == nil {
			base = resolved
		}
		if resolved, err := filepath.EvalSymlinks(out); err == nil {
			out = resolved
		}
		if base == out {
			t.Fatal("evidence source directory must differ from output directory")
		}
	}
	repos, err := repositories()
	if err != nil {
		t.Fatal(err)
	}
	pythonRepos, err := readRepositories("python/repos.json")
	if err != nil {
		t.Fatal(err)
	}
	repos = append(repos, pythonRepos...)
	tsRepos, err := readRepositories("typescript/repos.json")
	if err != nil {
		t.Fatal(err)
	}
	repos = append(repos, tsRepos...)
	selected := map[string]bool{}
	for _, name := range strings.Split(*corpusNames, ",") {
		selected[name] = true
	}
	for name := range selected {
		known := name == "all"
		for _, repo := range repos {
			known = known || name == repo.Name
		}
		if !known {
			t.Fatalf("unknown corpus repository %q", name)
		}
	}
	for _, repo := range repos {
		if !selected["all"] && !selected[repo.Name] {
			continue
		}
		t.Run(repo.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			dir := filepath.Join(*reportDir, repo.Name)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			r := runReport{SchemaVersion: 3, Status: "error", Input: InputIdentity{Repository: repo, Profile: "linux/amd64 CGO_ENABLED=0; production packages; root module; -mod=readonly"}, Evaluator: EvaluatorIdentity{Toolchain: runtime.Version(), Dependencies: evaluatorDependencies(t)}}
			if repo.Language == "python" {
				r.Input.Profile = "CPython AST; reviewed bindings; UTF-8; source root=" + repo.SourceRoot
			}
			if repo.Language == "typescript" {
				r.Input.Profile = "TypeScript AST and static source bindings; original tsconfig; source workspaces only; no third-party packages; profile=typescript/" + repo.Name + ".json"
			}
			if info, ok := debug.ReadBuildInfo(); ok {
				r.Subject.BuildInfo = info.String()
			}
			r.Subject.Revision = gitOutput(t, "rev-parse", "HEAD")
			diff := gitOutput(t, "diff", "HEAD", "--", ".", ":!tests/corpus", ":!docs", ":!AGENTS.md", ":!README.md", ":!README.zh-CN.md")
			r.Subject.DiffSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(diff)))
			r.Evaluator.SourceSHA256 = evaluatorHash(t)
			r.Subject.SourceSHA256 = sourceHash(t)
			defer func() {
				writeJSON(t, filepath.Join(dir, "report.json"), r)
				if err := os.WriteFile(filepath.Join(dir, "summary.md"), []byte(renderSummary(r)), 0644); err != nil {
					t.Error(err)
				}
			}()
			root := t.TempDir()
			if err := prepare(ctx, repo, root); err != nil {
				r.Error = err.Error()
				t.Fatal(err)
			}
			var o *oracle
			var docs []cg.Document
			var inventory []inputFile
			if repo.Language == "python" {
				var version string
				o, docs, inventory, version, err = loadPythonOracle(ctx, root, repo.SourceRoot, "python/"+repo.Name+".json")
				r.Evaluator.Toolchain += "; CPython " + version
			} else if repo.Language == "typescript" {
				var version string
				o, docs, inventory, version, err = loadTypeScriptOracle(ctx, root, "typescript/"+repo.Name+".json")
				r.Evaluator.Toolchain += "; " + version
			} else {
				o, docs, inventory, err = loadOracle(ctx, root)
			}
			if err != nil {
				r.Error = err.Error()
				t.Fatal(err)
			}
			r.Input.Files, r.Documents = inventory, len(docs)
			r.OracleDiagnostics = len(o.Diagnostics)
			writeJSON(t, filepath.Join(dir, "oracle.json"), o)
			var a observed
			if *reevaluateDir != "" {
				a, r.Subject, r.ReevaluatedFrom, err = readObservation(filepath.Join(*reevaluateDir, repo.Name), r.Input)
			} else {
				a, err = observe(ctx, o.Module, repo.Commit, docs)
			}
			if err != nil {
				r.Error = err.Error()
				t.Fatal(err)
			}
			writeJSON(t, filepath.Join(dir, "graph.json"), a)
			e := evaluate(o, a)
			r.Status, r.Evaluation = "measured", &e
			if *baselineDir != "" {
				data, err := os.ReadFile(filepath.Join(*baselineDir, repo.Name, "report.json"))
				if err != nil {
					r.Status = "error"
					r.Error = err.Error()
					t.Fatal(err)
				}
				var baseline runReport
				if err := json.Unmarshal(data, &baseline); err != nil {
					r.Status = "error"
					r.Error = err.Error()
					t.Fatal(err)
				}
				r.Regressions, err = compareBaseline(baseline, r)
				if err != nil {
					r.Status = "error"
					r.Error = err.Error()
					t.Fatal(err)
				}
				if len(r.Regressions) > 0 {
					e.Verdict = "failed"
					t.Errorf("corpus regressions: %v", r.Regressions)
				}
			}
			t.Logf("%s; verdict=%s; evidence=%s", summary(e), e.Verdict, dir)
			if e.Verdict == "failed" {
				t.Error("corpus quality gate failed; inspect report.json")
			}
		})
	}
}

func gitOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func writeJSON(t *testing.T, name string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}

func observe(ctx context.Context, module, snapshot string, docs []cg.Document) (observed, error) {
	opts := cg.Options{ModulePath: module, MaxDocuments: 2000, MaxNodes: 100000, MaxRelations: 500000, MaxSourceBytes: 64 << 20, MaxResultRows: 500000, MaxResultBytes: 128 << 20}
	g, report, err := cg.Build(ctx, snapshot, docs, opts)
	if err != nil {
		return observed{}, err
	}
	a := observed{Nodes: g.Nodes(), Relations: g.Relations(), Report: report}
	failedDocuments := map[string]bool{}
	for _, d := range report.Diagnostics {
		if d.Code == "parse_error" {
			failedDocuments[d.Location.Path] = true
		}
	}
	for _, doc := range docs {
		// A local parser gap is measurable missing evidence, not an oracle or
		// whole-build failure. Do not retry its extraction and erase that distinction.
		if failedDocuments[doc.Path] {
			continue
		}
		facts, err := g.Extract(ctx, doc)
		if err != nil {
			return observed{}, err
		}
		a.Facts = append(a.Facts, facts)
	}
	// Check that query projection delivers the same published evidence as the
	// direct accessors. This verifies transport, not semantic ground truth.
	for _, check := range []struct {
		query, key string
		values     map[string]any
	}{
		{"MATCH (n) RETURN n", "n", nodeValues(a.Nodes)},
		{"MATCH ()-[r]->() RETURN r", "r", relationValues(a.Relations)},
	} {
		rows, err := g.Query(ctx, check.query, nil)
		if err != nil {
			return a, err
		}
		if len(rows) != len(check.values) {
			return a, fmt.Errorf("query projection count: %s", check.query)
		}
		seen := map[string]bool{}
		for _, row := range rows {
			value := row[check.key]
			var id string
			switch v := value.(type) {
			case cg.Node:
				id = v.ID
			case cg.Relation:
				id = v.ID
			}
			if seen[id] || !reflect.DeepEqual(check.values[id], value) {
				return a, fmt.Errorf("query projection differs: %s", id)
			}
			seen[id] = true
		}
	}
	return a, nil
}

func nodeValues(nodes []cg.Node) map[string]any {
	out := map[string]any{}
	for _, n := range nodes {
		out[n.ID] = n
	}
	return out
}
func relationValues(edges []cg.Relation) map[string]any {
	out := map[string]any{}
	for _, r := range edges {
		out[r.ID] = r
	}
	return out
}
