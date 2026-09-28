package corpus_test

import (
	"fmt"
	"sort"
	"strings"

	cg "github.com/compforge/codegraph"
)

func renderSummary(r runReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s corpus evaluation\n\n- Source: [%s](%s/tree/%s)\n- CodeGraph: `%s`, source digest `%s`\n- Evaluator: `%s`\n- Toolchain: `%s`\n- Profile: %s\n- Execution: **%s**\n", r.Input.Repository.Name, r.Input.Repository.Commit, r.Input.Repository.Repository, r.Input.Repository.Commit, r.Subject.Revision, r.Subject.SourceSHA256, r.Evaluator.SourceSHA256, r.Evaluator.Toolchain, r.Input.Profile, r.Status)
	if r.ReevaluatedFrom != nil {
		fmt.Fprintf(&b, "\nRe-evaluated retained graph `%s` with the current oracle; original report `%s` remains unchanged.\n", r.ReevaluatedFrom.GraphSHA256, r.ReevaluatedFrom.ReportSHA256)
	}
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
		var organizationKeys []string
		for key := range e.Measurements {
			if strings.HasPrefix(key, "organizations/") || strings.HasPrefix(key, "relation_occurrences/") {
				organizationKeys = append(organizationKeys, key)
			}
		}
		sort.Strings(organizationKeys)
		for _, key := range organizationKeys {
			m := e.Measurements[key]
			fmt.Fprintf(&b, "| %s | %d / %d | %d |\n", key, m.Found, m.Expected, m.Unexpected)
		}
		b.WriteString("\n| Relation | Internal target hits | Correct / wrong exact | Correct / other candidate | Unassessed edges | Silent missing targets |\n|---|---:|---:|---:|---:|---:|\n")
		for _, kind := range []cg.RelationKind{cg.References, cg.Calls} {
			v := e.Bindings[kind]
			fmt.Fprintf(&b, "| %s | %d / %d | %d / %d | %d / %d | %d | %d |\n", kind, v.Hit, v.Expected, v.ExactCorrect, v.ExactWrong, v.CandidateCorrect, v.CandidateOther, v.Unassessed, v.SilentMissing)
		}
		if r.Input.Repository.Language == "python" {
			fmt.Fprintf(&b, "\nUnreviewed source occurrences: references=%d, calls=%d. Reviewed runtime-dispatch calls=%d, external calls=%d.\n", e.Unassessed["references/unknown"], e.Unassessed["calls/unknown"], e.Unassessed["calls/runtime_dispatch"], e.Unassessed["calls/external"])
			b.WriteString("\nPython target hits cover only reviewed source bindings, not all references or calls. CPython AST inventories syntax independently; unreviewed targets remain unknown. Store sites and parameters are outside the declaration contract. Dynamic dispatch and generated framework members are not certified. Organization identities, source contributions, direct membership and supported source imports are scored separately. Evidence derivation semantics, type relations and runtime behavior remain outside the oracle.\n")
		} else if r.Input.Repository.Language == "typescript" {
			fmt.Fprintf(&b, "\nTypeScript compiler diagnostics: %d (retained in oracle.json). Only loaded, uniquely resolved source targets are scored. Missing external dependencies remain unknown; method/callback runtime dispatch is unassessed.\n", r.OracleDiagnostics)
			b.WriteString("\nTypeScript syntax inventories are independent of CodeGraph. Parameters, property signatures and other unsupported declarations remain in the overall denominator. The compiler reads the original project configurations with source workspace links, without installing or executing the target application's external dependencies. Declaration files are compiler support, outside the graph input. Module identities, source contributions, membership and compiler-resolved module imports are scored separately. Symbol imports and type relations remain unassessed.\n")
		} else {
			b.WriteString("\nTarget recall excludes external and runtime-unknown targets; see report.json for all exclusions.\nParameters and short variable definitions remain in the overall declaration denominator.\nPackage identities, receiver ownership, source contributions, membership and internal package imports are independently scored. Evidence derivation semantics, type relations and markers remain unassessed.\n")
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
