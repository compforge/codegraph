package codegraph

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/compforge/codegraph/internal/analysis"
	"github.com/compforge/codegraph/internal/pipeline"
)

// Builder collects complete Facts for one source snapshot and publishes a new
// immutable Graph on each successful Build. Failed builds leave Result intact.
// Snapshot-specific binding is never cached in Facts or Extractor.
type Builder struct {
	tagRules    []compiledTagRule
	resolution  analysis.ResolutionContext
	mu          sync.RWMutex
	buildMu     sync.Mutex
	snapshot    string
	opts        Options
	documents   map[string]analysis.Facts
	sourceBytes int64
	failures    map[string]Diagnostic
	result      *Graph
	// Document extraction scheduling has an independent, lazy lifetime.
	sessionOnce sync.Once
	session     *session
	sessionErr  error
}

func NewBuilder(snapshot string, opts Options) (*Builder, error) {
	if snapshot == "" {
		return nil, errors.New("snapshot identity is required")
	}
	if err := defaults(&opts); err != nil {
		return nil, err
	}
	resolution, err := compileResolution(opts.ResolutionContext, opts.MaxEvidence)
	if err != nil {
		return nil, err
	}
	tagRules, err := compileTagRules(opts.TagRules)
	if err != nil {
		return nil, err
	}
	opts.TagRules = nil // Only the detached compiled rules are used after construction.
	opts.ResolutionContext = ResolutionContext{}
	b := &Builder{snapshot: snapshot, opts: opts, documents: map[string]analysis.Facts{}, failures: map[string]Diagnostic{}}
	b.resolution = resolution
	b.tagRules = tagRules
	b.result = newGraph(snapshot, opts, map[string]Node{}, map[string]Relation{}, BuildReport{Snapshot: snapshot, Documents: []string{}})
	return b, nil
}

// Add atomically admits extracted material. No parser or query engine runs here.
// Identical versions are idempotent; changing a path's content requires a new builder.
func (b *Builder) Add(facts ...Facts) error {
	b.buildMu.Lock()
	defer b.buildMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	staged := make(map[string]analysis.Facts, len(facts))
	total := b.sourceBytes
	for _, f := range facts {
		if f.raw == nil {
			return fmt.Errorf("facts must be produced by an Extractor")
		}
		raw := *f.raw
		doc := Document{Path: raw.Path, Content: raw.Source, Gitlink: raw.Gitlink}
		if !b.allowed(raw.Path) {
			return fmt.Errorf("facts %s are outside allowed scope", raw.Path)
		}
		old, ok := staged[raw.Path]
		if !ok {
			old, ok = b.documents[raw.Path]
		}
		if ok {
			if !doc.matches(old) {
				return fmt.Errorf("%w: %s", ErrSnapshotChanged, raw.Path)
			}
			continue
		}
		if doc.size() > b.opts.MaxDocumentBytes || len(b.documents)+len(staged) >= b.opts.MaxDocuments || doc.size() > b.opts.MaxSourceBytes-total {
			return fmt.Errorf("%w: %s", ErrBuildBudget, raw.Path)
		}
		staged[raw.Path] = raw
		total += doc.size()
	}
	// A regular source only needs its ancestors checked. Opaque gitlinks also
	// check descendants. This keeps ordinary one-at-a-time admission linear in
	// path depth rather than copying and sorting the growing snapshot each time.
	for p, f := range staged {
		for parent := path.Dir(p); ; parent = path.Dir(parent) {
			if b.documents[parent].Gitlink != "" || staged[parent].Gitlink != "" {
				return fmt.Errorf("document %s is inside opaque gitlink %s", p, parent)
			}
			if parent == "." {
				break
			}
		}
		if f.Gitlink == "" {
			continue
		}
		for name := range b.documents {
			if strings.HasPrefix(name, p+"/") {
				return fmt.Errorf("gitlink %s contains document %s", p, name)
			}
		}
		for name := range staged {
			if strings.HasPrefix(name, p+"/") {
				return fmt.Errorf("gitlink %s contains document %s", p, name)
			}
		}
	}
	for p, f := range staged {
		b.documents[p] = f
	}
	b.sourceBytes = total
	for _, f := range facts {
		delete(b.failures, f.raw.Path)
	}
	return nil
}

// AddFailure retains a document-local extraction gap without inventing facts.
// The caller must not pass context cancellation as a local parse failure.
func (b *Builder) AddFailure(doc Document, failure error) error {
	if err := doc.validate(); err != nil {
		return err
	}
	if failure == nil {
		return errors.New("extraction failure is required")
	}
	if errors.Is(failure, ErrBuildBudget) || errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
		return failure
	}
	b.buildMu.Lock()
	defer b.buildMu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.documents[doc.Path]; ok {
		return fmt.Errorf("facts already admitted for %s", doc.Path)
	}
	if !b.allowed(doc.Path) {
		return fmt.Errorf("document %s is outside allowed scope", doc.Path)
	}
	if doc.size() > b.opts.MaxDocumentBytes {
		return fmt.Errorf("%w: %s", ErrBuildBudget, doc.Path)
	}
	b.failures[doc.Path] = Diagnostic{Code: "parse_error", Message: failure.Error(), Subject: DocumentSubject, Location: location(pipeline.DocumentOnly(doc.Path, doc.Content), analysis.Span{End: len(doc.Content)})}
	return nil
}

// Build runs Organize, Bind and Resolve using only admitted facts. A caller can
// keep older results while adding more facts and publishing another result.
func (b *Builder) Build(ctx context.Context) (*Graph, BuildReport, error) {
	b.buildMu.Lock()
	defer b.buildMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, b.Report(), err
	}
	nodes, relations, report, err := b.assemble(ctx, b.documents, b.failures)
	if err != nil {
		return nil, b.Report(), err
	}
	if err := ctx.Err(); err != nil {
		return nil, b.Report(), err
	}
	result := newGraph(b.snapshot, b.opts, nodes, relations, report)
	b.mu.Lock()
	b.result = result
	b.mu.Unlock()
	return result, cloneReport(report), nil
}

// Result is the last successful publication (initially an empty graph).
func (b *Builder) Result() *Graph { b.mu.RLock(); defer b.mu.RUnlock(); return b.result }

func (b *Builder) Report() BuildReport { return b.Result().Report() }

// Options bounds a graph's build and query work. Zero values select finite defaults.
// Scope contains snapshot-relative, slash-separated document paths or directory prefixes.
// Empty Scope allows any relative path; documents are only analyzed when supplied.
type Options struct {
	// TagRules classifies Document and Directory paths without controlling parsing.
	// Nil selects BuiltinTagRules; an explicit empty slice disables all tags.
	TagRules          []TagRule
	ResolutionContext ResolutionContext
	// ExtractionCache optionally shares raw facts across snapshots; graph budgets
	// and relationship binding still apply independently to each Graph.
	ExtractionCache *ExtractionCache
	// BuildConcurrency bounds parallel document extraction within a batch.
	// Zero selects min(GOMAXPROCS, 4); one extracts serially.
	BuildConcurrency int
	// ModulePath supplies the root Go module identity for package organization
	// and import resolution. More specific ResolutionContext.GoModules win;
	// supplied go.mod declarations override same-root hints with diagnostics.
	ModulePath                           string
	Scope                                []string
	MaxDocuments, MaxNodes, MaxRelations int
	MaxEvidence                          int
	MaxDocumentBytes, MaxSourceBytes     int64
	ParseTimeout, QueryTimeout           time.Duration
	MaxQueryHops, MaxResultRows          int
	MaxResultBytes                       int64
}

func defaults(o *Options) error {
	for _, pair := range []struct {
		v   *int
		def int
	}{{&o.BuildConcurrency, min(runtime.GOMAXPROCS(0), 4)}, {&o.MaxDocuments, 256}, {&o.MaxNodes, 50000}, {&o.MaxRelations, 100000}, {&o.MaxEvidence, 1000000}, {&o.MaxQueryHops, 8}, {&o.MaxResultRows, 1000}} {
		if *pair.v < 0 {
			return errors.New("limits must not be negative")
		}
		if *pair.v == 0 {
			*pair.v = pair.def
		}
	}
	for _, pair := range []struct {
		v   *int64
		def int64
	}{{&o.MaxDocumentBytes, 2 << 20}, {&o.MaxSourceBytes, 32 << 20}, {&o.MaxResultBytes, 8 << 20}} {
		if *pair.v < 0 {
			return errors.New("limits must not be negative")
		}
		if *pair.v == 0 {
			*pair.v = pair.def
		}
	}
	if o.ParseTimeout < 0 || o.QueryTimeout < 0 {
		return errors.New("timeouts must not be negative")
	}
	if o.ParseTimeout == 0 {
		o.ParseTimeout = 2 * time.Second
	}
	if o.QueryTimeout == 0 {
		o.QueryTimeout = 5 * time.Second
	}
	o.Scope = append([]string(nil), o.Scope...)
	for _, p := range o.Scope {
		if !fs.ValidPath(p) {
			return fmt.Errorf("invalid scope %q", p)
		}
	}
	return nil
}

// ResolutionContext supplies snapshot-specific repository knowledge. It never
// loads files: only supplied Facts can become graph endpoints. Omitted imports
// use language defaults; an explicit empty Targets list means known unresolved.
type ResolutionContext struct {
	// GoModules maps snapshot-relative module roots to module import paths.
	GoModules map[string]string
	// Imports supplies lexical module targets for Python and JavaScript/TypeScript.
	Imports []ImportResolution
}

// ImportResolution identifies a lexical import occurrence and supplies its
// candidate module documents. Targets exclude merely incidental dependencies
// (for example Python ancestor initializers). Confidence caps every derived
// binding; it is required even when no target exists.
type ImportResolution struct {
	Document   string
	Import     FactImport
	Targets    []string
	Confidence Confidence
	Basis      string
}

func compileResolution(context ResolutionContext, limit int) (analysis.ResolutionContext, error) {
	out := analysis.ResolutionContext{GoModules: map[string]string{}, Imports: map[analysis.ImportKey]analysis.ImportResolution{}}
	used := len(context.GoModules)
	for root, module := range context.GoModules {
		if !fs.ValidPath(root) || strings.TrimSpace(module) == "" {
			return out, fmt.Errorf("invalid Go module mapping %q", root)
		}
		out.GoModules[root] = module
	}
	for _, entry := range context.Imports {
		used += 1 + len(entry.Targets)
		if used > limit {
			return out, fmt.Errorf("%w: resolution context", ErrBuildBudget)
		}
		if !fs.ValidPath(entry.Document) || entry.Import.Location.StartByte < 0 || !entry.Confidence.Valid() {
			return out, fmt.Errorf("invalid import resolution for %q", entry.Document)
		}
		targets := make([]string, 0, len(entry.Targets))
		seen := map[string]bool{}
		for _, p := range entry.Targets {
			if !fs.ValidPath(p) {
				return out, fmt.Errorf("invalid import target %q", p)
			}
			if !seen[p] {
				targets = append(targets, p)
				seen[p] = true
			}
		}
		key := analysis.ImportKey{Document: entry.Document, Start: entry.Import.Location.StartByte, Path: entry.Import.Path, From: entry.Import.From, Binding: entry.Import.Binding}
		if _, exists := out.Imports[key]; exists {
			return out, fmt.Errorf("duplicate import resolution for %s at %d", entry.Document, key.Start)
		}
		out.Imports[key] = analysis.ImportResolution{Targets: targets, Confidence: analysis.Confidence(entry.Confidence), Basis: entry.Basis}
	}
	if used > limit {
		return out, fmt.Errorf("%w: resolution context", ErrBuildBudget)
	}
	return out, nil
}

// SetResolutionContext replaces repository knowledge before the next Build.
// It copies all input collections; previously published graphs remain unchanged.
func (b *Builder) SetResolutionContext(context ResolutionContext) error {
	resolution, err := compileResolution(context, b.opts.MaxEvidence)
	if err != nil {
		return err
	}
	b.buildMu.Lock()
	defer b.buildMu.Unlock()
	b.resolution = resolution
	return nil
}

func (g *Builder) assemble(ctx context.Context, files map[string]analysis.Facts, failures map[string]Diagnostic) (map[string]Node, map[string]Relation, BuildReport, error) {
	nodes := map[string]Node{}
	relations := map[string]Relation{}
	ids := map[analysis.Ref]string{}
	report := BuildReport{Snapshot: g.snapshot, Documents: sortedFiles(files)}
	for _, d := range failures {
		report.Diagnostics = append(report.Diagnostics, d)
		// A failed parser cannot erase the identity of a supplied document.
		// Keep the Document and its path structure; no language declarations are inferred.
		if d.Code == "parse_error" {
			id := DocumentID(d.Location.Path)
			nodes[id] = Node{ID: id, Kind: DocumentNodeKind, Name: path.Base(d.Location.Path),
				Language: Language(d.Location.Path), DocumentKind: materialKind(d.Location.Path, ""), Location: &d.Location}
		}
	}
	index, issues, err := pipeline.BuiltinsWithResolution(ctx, files, g.opts.ModulePath, g.resolution, g.opts.MaxNodes-len(nodes), g.opts.MaxRelations, g.opts.MaxEvidence)
	if err != nil {
		if errors.Is(err, analysis.ErrEvidenceLimit) || errors.Is(err, analysis.ErrEdgeLimit) || errors.Is(err, pipeline.ErrNodeLimit) {
			err = fmt.Errorf("%w: %v", ErrBuildBudget, err)
		}
		return nil, nil, report, err
	}

	for ref, e := range index.Entities {
		if err := ctx.Err(); err != nil {
			return nil, nil, report, err
		}
		var id string
		switch {
		case ref.IsDocument():
			id = DocumentID(ref.Path)
		case ref.IsDeclaration():
			id = declarationID(ref.Path, NodeKind(e.Kind), e.QualifiedName, e.Location.Start)
		default:
			id = "node:" + identity(ref.SyntheticKey())
		}
		n := Node{ID: id, Kind: NodeKind(e.Kind), Name: e.Name, QualifiedName: e.QualifiedName, Language: e.Language}
		if e.Binding != nil {
			b := ModuleBinding(*e.Binding)
			n.Binding = &b
		}
		if ref.IsDocument() {
			n.Gitlink = files[ref.Path].Gitlink
			n.DocumentKind = materialKind(ref.Path, n.Gitlink)
			n.Manifest = projectManifest(files[ref.Path])
		}
		if e.Location != nil {
			f := files[e.Location.Path]
			n.Location = locationPtr(f, e.Location.Span)
			n.Documentation = projectDocumentation(f, e.Documentation)
			for _, m := range e.Comments {
				n.Markers = append(n.Markers, Marker{Kind: MarkerKind(m.Kind), Text: m.Text, Location: location(f, m.Span)})
			}
		}
		if e.SignatureLocation != nil {
			f := files[e.SignatureLocation.Path]
			span := e.SignatureLocation.Span
			n.Signature = string(f.Source[span.Start:span.End])
			n.SignatureLocation = locationPtr(f, span)
		}
		if e.NameLocation != nil {
			n.NameLocation = locationPtr(files[e.NameLocation.Path], e.NameLocation.Span)
		}
		ids[ref], nodes[id] = id, n
	}
	for _, p := range report.Documents {
		f := files[p]
		for _, issue := range f.Issues {
			report.Diagnostics = append(report.Diagnostics, extractionDiagnostic(f, issue))
		}
	}
	edges := index.Edges
	for _, e := range edges {
		if ids[e.Source] == "" || ids[e.Target] == "" {
			return nil, nil, report, fmt.Errorf("unpublished relation endpoint: %+v", e)
		}
		loc := location(files[e.Path], e.Span)
		id := identity(ids[e.Source], ids[e.Target], e.Kind, loc.Path, loc.StartByte, loc.EndByte)
		r := Relation{ID: id, Source: ids[e.Source], Target: ids[e.Target], Kind: RelationKind(e.Kind), Confidence: Confidence(e.Confidence), Location: loc}
		for _, proof := range e.Evidence {
			evidence := Evidence{Basis: proof.Basis, Confidence: Confidence(proof.Confidence)}
			if proof.Location != nil {
				evidence.Location = locationPtr(files[proof.Location.Path], proof.Location.Span)
			}
			r.Evidence = append(r.Evidence, evidence)
		}
		relations[id] = r
	}
	if err := g.publishDocumentStructure(ctx, nodes, relations); err != nil {
		return nil, nil, report, err
	}
	if err := publishReferences(ctx, files, ids, nodes, relations, g.opts); err != nil {
		return nil, nil, report, err
	}
	for _, i := range issues {
		report.Diagnostics = append(report.Diagnostics, Diagnostic{Code: i.Code, Message: i.Reference,
			Subject: RelationsSubject, Relation: RelationKind(i.Relation), Location: location(files[i.Path], i.Span)})
	}
	if len(nodes) > g.opts.MaxNodes || len(relations) > g.opts.MaxRelations {
		return nil, nil, report, fmt.Errorf("%w: nodes=%d relations=%d", ErrBuildBudget, len(nodes), len(relations))
	}
	sort.Slice(report.Diagnostics, func(i, j int) bool {
		a, b := report.Diagnostics[i], report.Diagnostics[j]
		if a.Location.Path != b.Location.Path {
			return a.Location.Path < b.Location.Path
		}
		if a.Location.StartByte != b.Location.StartByte {
			return a.Location.StartByte < b.Location.StartByte
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	report.Nodes, report.Relations = len(nodes), len(relations)
	return nodes, relations, report, nil
}

func (g *Builder) allowed(p string) bool {
	for _, s := range g.opts.Scope {
		if s == "." || p == s || strings.HasPrefix(p, s+"/") {
			return true
		}
	}
	return len(g.opts.Scope) == 0
}

func (g *Builder) allowedDir(p string) bool { return g.allowed(p) }

func sortedFiles(files map[string]analysis.Facts) []string {
	out := make([]string, 0, len(files))
	for p := range files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
