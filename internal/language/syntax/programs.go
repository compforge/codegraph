package syntax

import (
	"strings"
	"sync"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

type factProgramKey struct {
	language *gts.Language
	kinds    gts.FactKind
	name     string
}

type outlineProgramKey struct {
	language *gts.Language
	name     string
	query    string
}

// +why=Compiled programs contain grammar metadata, not snapshot state; share them while keeping each extraction's tree and result slices local.
var factPrograms sync.Map
var outlinePrograms sync.Map

// FactProgram reuses an immutable program for the exact grammar, rule owner and fact kinds.
// The grammar pointer matters: upstream rejects trees from another Language,
// even when the languages have the same name.
func FactProgram(lang *gts.Language, kinds gts.FactKind, entry grammars.LangEntry) (*gts.FactProgram, error) {
	key := factProgramKey{lang, kinds, entry.Name}
	if cached, ok := factPrograms.Load(key); ok {
		return cached.(func() (*gts.FactProgram, error))()
	}
	compile := sync.OnceValues(func() (*gts.FactProgram, error) {
		entry.Language = func() *gts.Language { return lang }
		return gts.NewFactProgramWithOptions(lang, kinds,
			gts.WithDeclarationRules(declarationRules(entry)),
			gts.WithSignatureRules(grammars.SignatureRules(entry)),
			gts.WithCallArgumentRules(grammars.CallArgumentRules(entry)))
	})
	cached, _ := factPrograms.LoadOrStore(key, compile)
	return cached.(func() (*gts.FactProgram, error))()
}

func outlineProgram(lang *gts.Language, entry grammars.LangEntry) (*gts.Outliner, error) {
	// Owner rules are selected by entry name; extensions can share a grammar
	// while using different rules or queries. Keep all three in the identity.
	key := outlineProgramKey{lang, strings.TrimSpace(entry.Name), entry.TagsQuery}
	if cached, ok := outlinePrograms.Load(key); ok {
		return cached.(func() (*gts.Outliner, error))()
	}
	compile := sync.OnceValues(func() (*gts.Outliner, error) {
		return gts.NewOutliner(lang, entry.TagsQuery,
			gts.WithOutlineOwnerRules(grammars.OutlineOwnerRules(entry)))
	})
	cached, _ := outlinePrograms.LoadOrStore(key, compile)
	return cached.(func() (*gts.Outliner, error))()
}
