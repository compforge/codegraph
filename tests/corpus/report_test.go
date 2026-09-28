package corpus_test

import (
	"fmt"
	"strings"

	cg "github.com/compforge/codegraph"
)

func renderSummary(r runReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s corpus evaluation\n\n- Source: [%s](%s/tree/%s)\n- CodeGraph: `%s`, source digest `%s`\n- Evaluator: `%s`\n- Toolchain: `%s`\n- Profile: %s\n- Execution: **%s**\n", r.Repository.Name, r.Repository.Commit, r.Repository.Repository, r.Repository.Commit, r.CodeGraphRevision, r.CodeGraphSourceSHA256, r.EvaluatorSHA256, r.Toolchain, r.Profile, r.Status)
	if r.Error != "" {
		fmt.Fprintf(&b, "\nError: %s\n", r.Error)
	}
	if r.Evaluation != nil {
		e := r.Evaluation
		fmt.Fprintf(&b, "- Verdict: **%s** (measurement is not completeness certification)\n- Included source documents: %d\n\n", e.Verdict, r.Documents)
		b.WriteString("| Fact | Found / expected | Unexpected |\n|---|---:|---:|\n")
		var declared measurement
		for k, m := range e.Measurements {
			if strings.HasPrefix(k, "declarations/contract/") {
				declared.Expected += m.Expected
				declared.Found += m.Found
			}
		}
		fmt.Fprintf(&b, "| Declarations within contract | %d / %d | see all declarations |\n", declared.Found, declared.Expected)
		for _, key := range []string{"declarations/all", "reference_facts/all", "call_expressions/all", "import_facts/all"} {
			if m := e.Measurements[key]; m != nil {
				fmt.Fprintf(&b, "| %s | %d / %d | %d |\n", key, m.Found, m.Expected, m.Unexpected)
			}
		}
		b.WriteString("\n| Relation | Internal target hits | Correct / wrong exact | Correct / other candidate | Unassessed edges | Silent missing targets |\n|---|---:|---:|---:|---:|---:|\n")
		for _, kind := range []cg.RelationKind{cg.References, cg.Calls} {
			v := e.Bindings[kind]
			fmt.Fprintf(&b, "| %s | %d / %d | %d / %d | %d / %d | %d | %d |\n", kind, v.Hit, v.Expected, v.ExactCorrect, v.ExactWrong, v.CandidateCorrect, v.CandidateOther, v.Unassessed, v.SilentMissing)
		}
		if r.Repository.Language == "python" {
			fmt.Fprintf(&b, "\nUnreviewed source occurrences: references=%d, calls=%d. Reviewed runtime-dispatch calls=%d, external calls=%d.\n", e.Unassessed["references/unknown"], e.Unassessed["calls/unknown"], e.Unassessed["calls/runtime_dispatch"], e.Unassessed["calls/external"])
			b.WriteString("\nPython target hits cover only reviewed source bindings, not all references or calls. CPython AST inventories syntax independently; unreviewed targets remain unknown. Store sites and parameters are outside the declaration contract. Dynamic dispatch and generated framework members are not certified. Source ownership, basis semantics and non-reference/call relations are not independently scored.\n")
		} else {
			b.WriteString("\nTarget recall excludes external and runtime-unknown targets; see report.json for all exclusions.\nParameters and short variable definitions remain in the overall declaration denominator.\nReference/call source ownership, basis semantics, type/containment/import bindings and markers are not independently scored.\n")
		}
		if len(r.Regressions) > 0 {
			b.WriteString("\n## Regressions\n\n")
			for _, s := range r.Regressions {
				fmt.Fprintf(&b, "- %s\n", s)
			}
		}
	}
	b.WriteString("\n## Evidence\n\n[Report and differences](report.json) · [Independent oracle](oracle.json) · [Actual graph and facts](graph.json)\n")
	return b.String()
}
