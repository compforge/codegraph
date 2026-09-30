package model

// BuildReport describes the published graph and its local information gaps.
// Build/Wait errors describe execution failures; diagnostics do not invalidate
// unrelated graph facts or prescribe a consumer's fallback policy.
// +spec=`Candidate relations and local gaps remain usable analysis results`
type BuildReport struct {
	Snapshot         string       `json:"snapshot"`
	Documents        []string     `json:"documents"`
	Diagnostics      []Diagnostic `json:"diagnostics"`
	Nodes, Relations int
}

func CloneReport(r BuildReport) BuildReport {
	r.Documents = append([]string(nil), r.Documents...)
	r.Diagnostics = CloneDiagnostics(r.Diagnostics)
	return r
}
