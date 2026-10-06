package corpus_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	cg "github.com/compforge/codegraph"
)

type corpusScorecard struct {
	SchemaVersion int               `json:"schemaVersion"`
	StartedAt     time.Time         `json:"startedAt"`
	FinishedAt    *time.Time        `json:"finishedAt,omitempty"`
	Status        string            `json:"status"`
	Repositories  []repositoryScore `json:"repositories"`
	Languages     []languageScore   `json:"languages"`
}

type repositoryScore struct {
	Name     string          `json:"name"`
	Language string          `json:"language"`
	Commit   string          `json:"commit"`
	Status   string          `json:"status"`
	Verdict  string          `json:"verdict,omitempty"`
	Error    string          `json:"error,omitempty"`
	Report   string          `json:"report"`
	Subject  SubjectIdentity `json:"subject"`
}

type languageScore struct {
	Language   string                        `json:"language"`
	Selected   int                           `json:"selected"`
	Measured   int                           `json:"measured"`
	Failed     int                           `json:"failed"`
	Documents  int                           `json:"documents"`
	Facts      map[string]factScore          `json:"facts"`
	Bindings   map[cg.RelationKind]*bindings `json:"bindings"`
	Unassessed map[string]int                `json:"unassessed"`
}

type factScore struct {
	measurement
	Precision *float64 `json:"precision"`
	Recall    *float64 `json:"recall"`
}

func ratio(n, d int) *float64 {
	if d == 0 {
		return nil
	}
	value := float64(n) / float64(d)
	return &value
}

func percentage(n, d int) string {
	r := ratio(n, d)
	if r == nil {
		return "N/A (0/0)"
	}
	return fmt.Sprintf("%.2f%% (%d/%d)", 100**r, n, d)
}

func summarizeLanguages(reports []runReport, started time.Time, finished bool) corpusScorecard {
	out := corpusScorecard{SchemaVersion: 1, StartedAt: started, Status: "running"}
	if finished {
		now := time.Now().UTC()
		out.FinishedAt = &now
		out.Status = "measured"
	}
	for _, language := range []string{"go", "python", "typescript"} {
		score := languageScore{Language: language, Facts: map[string]factScore{}, Bindings: map[cg.RelationKind]*bindings{}, Unassessed: map[string]int{}}
		for _, r := range reports {
			lang := r.Input.Repository.Language
			if lang == "" {
				lang = "go"
			}
			if lang != language {
				continue
			}
			score.Selected++
			repo := repositoryScore{Name: r.Input.Repository.Name, Language: lang, Commit: r.Input.Repository.Commit, Status: r.Status, Error: r.Error, Report: filepath.ToSlash(filepath.Join(r.Input.Repository.Name, "report.json")), Subject: r.Subject}
			if r.Evaluation != nil {
				repo.Verdict = r.Evaluation.Verdict
			}
			out.Repositories = append(out.Repositories, repo)
			if r.Status != "measured" || r.Evaluation == nil {
				if finished {
					out.Status = "error"
				}
				continue
			}
			score.Measured++
			score.Documents += r.Documents
			e := r.Evaluation
			if e.Verdict == "failed" {
				score.Failed++
				if finished && out.Status != "error" {
					out.Status = "failed"
				}
			}
			for name, m := range e.Measurements {
				total := score.Facts[name]
				total.Expected += m.Expected
				total.Found += m.Found
				total.Unexpected += m.Unexpected
				score.Facts[name] = total
			}
			for name, n := range e.Unassessed {
				score.Unassessed[name] += n
			}
			for kind, b := range e.Bindings {
				total := score.Bindings[kind]
				if total == nil {
					total = &bindings{ByConfidence: map[cg.Confidence]*tierBindings{}}
					score.Bindings[kind] = total
				}
				total.Expected += b.Expected
				total.Hit += b.Hit
				total.MaxTargets = max(total.MaxTargets, b.MaxTargets)
				total.SilentMissing += b.SilentMissing
				total.LocalizedMissing += b.LocalizedMissing
				total.DocumentGapMissing += b.DocumentGapMissing
				for confidence, tier := range b.ByConfidence {
					dest := total.tier(confidence)
					dest.Hit += tier.Hit
					dest.Other += tier.Other
					dest.Unassessed += tier.Unassessed
				}
			}
		}
		for name, m := range score.Facts {
			m.Recall = ratio(m.Found, m.Expected)
			// Contract buckets have no independent unexpected-output denominator.
			// Only complete /all inventories and structural metrics admit precision.
			if strings.HasSuffix(name, "/all") || strings.HasPrefix(name, "organizations/") || strings.HasPrefix(name, "organization_structure/") || strings.HasPrefix(name, "relation_occurrences/") {
				m.Precision = ratio(m.Found, m.Found+m.Unexpected)
			}
			score.Facts[name] = m
		}
		out.Languages = append(out.Languages, score)
	}
	return out
}

func renderScorecard(s corpusScorecard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# CodeGraph evaluation\n\nStarted: %s · Status: **%s**\n\n", s.StartedAt.Format(time.RFC3339), s.Status)
	b.WriteString("Rates sum counts across measured repositories (micro averages). Unselected languages and zero denominators are N/A. Failed or unrun repositories never contribute fabricated zero scores.\n\n")
	b.WriteString("| Language | Measured / selected repositories | Failed quality gates | Documents |\n|---|---:|---:|---:|\n")
	for _, l := range s.Languages {
		fmt.Fprintf(&b, "| %s | %d / %d | %d | %d |\n", l.Language, l.Measured, l.Selected, l.Failed, l.Documents)
	}
	b.WriteString("\n## Symbols and syntax\n\nPrecision = found / (found + unexpected); recall = found / expected. Unexpected syntax outputs are oracle differences requiring inspection, not adjudicated defects.\n\n")
	b.WriteString("| Language | Fact | Oracle-match precision | Recall | Unexpected |\n|---|---|---:|---:|---:|\n")
	for _, l := range s.Languages {
		var contract measurement
		for name, m := range l.Facts {
			if strings.HasPrefix(name, "declarations/contract/") {
				contract.Found += m.Found
				contract.Expected += m.Expected
			}
		}
		fmt.Fprintf(&b, "| %s | declarations within contract | N/A | %s | see declarations/all |\n", l.Language, percentage(contract.Found, contract.Expected))
		for _, name := range []string{"declarations/all", "reference_facts/all", "call_expressions/all", "import_facts/all"} {
			m := l.Facts[name]
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %d |\n", l.Language, name, percentage(m.Found, m.Found+m.Unexpected), percentage(m.Found, m.Expected), m.Unexpected)
		}
	}
	b.WriteString("\n## Code organization\n\nNamespace identity includes kind, name, qualified name and contributing documents. Namespace nesting is scored separately from declaration membership; edge kinds, endpoints and source occurrences must match.\n\n")
	b.WriteString("| Language | Structure | Oracle-match precision | Recall | Unexpected |\n|---|---|---:|---:|---:|\n")
	for _, l := range s.Languages {
		keys := []string{"organization_structure/in_namespace", "organization_structure/contains"}
		for name := range l.Facts {
			if strings.HasPrefix(name, "organizations/") {
				keys = append(keys, name)
			}
		}
		slices.Sort(keys)
		for _, name := range keys {
			m := l.Facts[name]
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %d |\n", l.Language, name, percentage(m.Found, m.Found+m.Unexpected), percentage(m.Found, m.Expected), m.Unexpected)
		}
	}
	b.WriteString("\n## Call and reference targets\n\nTarget recall counts internal source occurrences with a correct target. Exact precision counts assessed exact edges; unknown targets are excluded and shown separately. Python target scores cover reviewed bindings only.\n\n")
	b.WriteString("| Language | Relation | Target recall | Exact precision | Wrong exact | Unknown / runtime / external occurrences | Unassessed emitted edges |\n|---|---|---:|---:|---:|---:|---:|\n")
	for _, l := range s.Languages {
		for _, kind := range []cg.RelationKind{cg.Calls, cg.References} {
			v := l.Bindings[kind]
			if v == nil {
				v = &bindings{}
			}
			exact := v.ByConfidence[cg.Exact]
			if exact == nil {
				exact = &tierBindings{}
			}
			unassessed := 0
			for _, tier := range v.ByConfidence {
				unassessed += tier.Unassessed
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %d / %d / %d | %d |\n", l.Language, kind, percentage(v.Hit, v.Expected), percentage(exact.Hit, exact.Hit+exact.Other), exact.Other, l.Unassessed[string(kind)+"/unknown"], l.Unassessed[string(kind)+"/runtime_dispatch"], l.Unassessed[string(kind)+"/external"], unassessed)
		}
	}
	b.WriteString("\n## Evidence and scope\n\n")
	for _, r := range s.Repositories {
		fmt.Fprintf(&b, "- [%s](%s): %s / %s; source `%s`; CodeGraph `%s`. %s\n", r.Name, r.Report, r.Status, r.Verdict, r.Commit, r.Subject.Revision, r.Error)
	}
	b.WriteString("\nGo uses AST plus type checking (the production adapter also uses Go AST). Python uses CPython ast plus reviewed binding samples. TypeScript uses its compiler on source workspaces; its corpus is not separate JavaScript coverage. Syntax inventories also measure extraction Facts; organization and target edges are read from the published graph. No single percentage certifies the whole graph. Missing dependencies, dynamic targets, unassessed relation kinds and confidence tiers remain in each report.\n")
	return b.String()
}

func writeScorecard(t *testing.T, dir string, reports []runReport, started time.Time, finished bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	scorecard := summarizeLanguages(reports, started, finished)
	writeJSON(t, filepath.Join(dir, "summary.json"), scorecard)
	text := renderScorecard(scorecard)
	if err := os.WriteFile(filepath.Join(dir, "summary.md"), []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	if finished {
		fmt.Print("\n" + text)
	}
}
