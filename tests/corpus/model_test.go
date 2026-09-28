package corpus_test

import (
	"fmt"

	cg "github.com/compforge/codegraph"
)

// The oracle records source identities, never CodeGraph-generated node IDs.
type site struct {
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

func (s site) key() string { return fmt.Sprintf("%s:%d:%d", s.Path, s.Start, s.End) }

type declaration struct {
	Name      string      `json:"name"`
	Kind      cg.NodeKind `json:"kind"`
	NameSite  site        `json:"nameSite"`
	Span      site        `json:"span"`
	Supported bool        `json:"supported"`
	Reason    string      `json:"reason,omitempty"`
}

type occurrence struct {
	Site   site   `json:"site"`
	Name   string `json:"name"`
	Target string `json:"target,omitempty"`
	Class  string `json:"class"`
	Reason string `json:"reason,omitempty"`
}

type organization struct {
	Kind          cg.NodeKind     `json:"kind"`
	Name          string          `json:"name"`
	QualifiedName string          `json:"qualifiedName"`
	Contributions map[string]site `json:"contributions"`
	Parent        string          `json:"parent,omitempty"`
}
type semanticRelation struct {
	Source string          `json:"source"`
	Target string          `json:"target"`
	Kind   cg.RelationKind `json:"kind"`
	Site   site            `json:"site"`
}

type oracle struct {
	Organizations      map[string]organization `json:"organizations,omitempty"`
	Owners             map[string]string       `json:"owners,omitempty"`
	Relations          []semanticRelation      `json:"relations,omitempty"`
	Module             string                  `json:"module"`
	Diagnostics        []compilerDiagnostic    `json:"diagnostics,omitempty"`
	Declarations       map[string]declaration  `json:"declarations"`
	References         map[string]occurrence   `json:"references"`
	Calls              map[string]occurrence   `json:"calls"`
	Imports            map[string]occurrence   `json:"imports"`
	ExcludedReferences map[string]occurrence   `json:"excludedReferences"`
}

type measurement struct {
	Expected   int `json:"expected"`
	Found      int `json:"found"`
	Unexpected int `json:"unexpected"`
}

type bindings struct {
	Expected           int                             `json:"expected"`
	Hit                int                             `json:"hit"`
	ByConfidence       map[cg.Confidence]*tierBindings `json:"byConfidence"`
	MaxTargets         int                             `json:"maxTargets"`
	SilentMissing      int                             `json:"silentMissing"`
	LocalizedMissing   int                             `json:"localizedMissing"`
	DocumentGapMissing int                             `json:"documentGapMissing"`
}

type finding struct {
	Category   string        `json:"category"`
	Site       site          `json:"site"`
	Expected   string        `json:"expected,omitempty"`
	Actual     string        `json:"actual,omitempty"`
	Confidence cg.Confidence `json:"confidence,omitempty"`
	Evidence   []cg.Evidence `json:"evidence,omitempty"`
}

type evaluation struct {
	Measurements map[string]*measurement       `json:"measurements"`
	Bindings     map[cg.RelationKind]*bindings `json:"bindings"`
	Unassessed   map[string]int                `json:"unassessed"`
	Diagnostics  map[string]int                `json:"diagnostics"`
	Findings     []finding                     `json:"findings"`
	Verdict      string                        `json:"verdict"`
}

func location(l cg.Location) site { return site{l.Path, l.StartByte, l.EndByte} }

func (e *evaluation) metric(name string) *measurement {
	if e.Measurements[name] == nil {
		e.Measurements[name] = &measurement{}
	}
	return e.Measurements[name]
}

// Legacy candidates remain ungraded during explicit artifact re-evaluation.
// They are never emitted by the current CodeGraph API.
const legacyCandidate cg.Confidence = "candidate"

type tierBindings struct {
	Hit        int `json:"hit"`
	Other      int `json:"other"`
	Unassessed int `json:"unassessed"`
}

func (b *bindings) tier(c cg.Confidence) *tierBindings {
	if b.ByConfidence == nil {
		b.ByConfidence = map[cg.Confidence]*tierBindings{}
	}
	if b.ByConfidence[c] == nil {
		b.ByConfidence[c] = &tierBindings{}
	}
	return b.ByConfidence[c]
}
func (b *bindings) otherTargets() int {
	n := 0
	for _, v := range b.ByConfidence {
		n += v.Other
	}
	return n
}
