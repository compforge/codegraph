package analysis

type Span struct{ Start, End int }
type Declaration struct {
	Name, QualifiedName, Kind string
	// NameSpan preserves the identifier capture separately from the declaration.
	// A zero span means the extractor did not supply a name location.
	NameSpan Span
	// SignatureSpan is the declaration header without its implementation body.
	SignatureSpan Span
	// Parent is the enclosing declaration index, or -1 for a file-level declaration.
	Parent int
	// Receiver is an explicit nonlexical member owner, bound by the language adapter.
	Receiver string
	// Extension is detached evidence interpreted only by the owning language.
	Extension Extension
	Span
	Comments      []Comment
	Documentation []Documentation
}

// Documentation is an exact source fragment attached to a declaration.
// Text retains comment delimiters or string syntax; consumers own rendering.
type Documentation struct {
	Text string
	Span
}
type Comment struct {
	Kind, Text string
	Span
}

// ImportBinding distinguishes imported names from the local names they bind.
type ImportBinding struct {
	Name, Local         string
	Namespace, ReExport bool
	Span                // complete statement, for lexical scope and shadow checks
}

type Import struct {
	Alias, Path string
	From        string
	Relative    int
	// Binding is the name the import introduces in this lexical scope.
	Binding string
	// Names are the imported names before caller aliases; empty means the whole
	// module is imported. Bindings retain per-name aliases for resolution.
	Names    []string
	Bindings []ImportBinding
	Span
}

// CallTarget is a detached syntax hypothesis, not a runtime dispatch result.
type CallTarget struct {
	Name, ReceiverType, Module string
	Kind, Basis                string
}

type Call struct {
	Name, Receiver string
	Span
	// Blocked excludes the static-function path; Targets may still carry dispatch candidates.
	Blocked   bool
	Builtin   bool
	Imported  bool
	Targets   []CallTarget
	Extension Extension
}

// Reference is a lexical identifier use, independently of whether a target exists.
type Reference struct {
	Name, Receiver string
	// Extension carries language-owned evidence for this use.
	Extension Extension
	// Member distinguishes selectors from bare identifiers, even without type hints.
	Member bool
	Span
	Owner  int  // enclosing declaration, -1 for the file
	Target int  // same-file declaration when syntax proves binding, -1 otherwise
	Bound  bool // a lexical binding exists; do not substitute a same-named declaration
}

// TypeRelation records an explicit base/interface name before scope binding.
type TypeRelation struct {
	Owner                     int
	Name, Module, Kind, Basis string
	Span
}

type Facts struct {
	Path, Package, Language string
	Gitlink                 string
	Lexical                 *Lexicon
	PackageSpan             Span
	Source                  []byte
	LineStarts              []int
	Declarations            []Declaration
	Imports                 []Import
	Calls                   []Call
	References              []Reference
	TypeRelations           []TypeRelation
	Issues                  []Issue
	// Exports maps a public alias to its local name for explicit export
	// aliases; extraction records them for producer-side module resolution.
	Exports map[string]string
	// Statements preserve statement order and scope; their
	// bounded expression vocabulary supports producer-side dependency exploration.
	Statements []Statement
}

type Issue struct {
	Code, Message     string
	Subject, Relation string
	Span
	Outline *OutlineCoverage
}

// OutlineCoverage is a detached receipt for the declaration query. Keeping
// this value independent of the parser makes cached facts safe to publish.
type OutlineCoverage struct {
	Symbols                    int    `json:"symbols"`
	OmittedNoName              int    `json:"omittedNoName,omitempty"`
	OmittedDuplicate           int    `json:"omittedDuplicate,omitempty"`
	OmittedNameConflict        int    `json:"omittedNameConflict,omitempty"`
	OmittedConflict            int    `json:"omittedConflict,omitempty"`
	OmittedOverlap             int    `json:"omittedOverlap,omitempty"`
	OmittedInvalidNameRange    int    `json:"omittedInvalidNameRange,omitempty"`
	OmittedMultipleDefinitions int    `json:"omittedMultipleDefinitions,omitempty"`
	OwnerRuleMisses            int    `json:"ownerRuleMisses,omitempty"`
	DeclineReason              string `json:"declineReason,omitempty"`
	Truncated                  bool   `json:"truncated,omitempty"`
}

// Extension is immutable evidence interpreted only by its owning language adapter.
type Extension interface{ Language() string }
type Statement struct {
	Kind, Name    string
	Line          int
	Imports       []Import
	Target, Value Expression
	Prelude       []Expression
	Body, Else    []Statement
}

type Expression struct {
	Kind, Text string
	Children   []Expression
}
