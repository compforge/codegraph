package pkg

// Relation kinds describe semantic operations and occurrence binding.
const (
	BindsTo      RelationKind = "binds_to"
	Reads        RelationKind = "reads"
	Writes       RelationKind = "writes"
	UsesType     RelationKind = "uses_type"
	Captures     RelationKind = "captures"
	UsesResource RelationKind = "uses_resource"
)

// Occurrence kinds support navigation without requiring an occurrence node for
// every dependency. Admission selects these independently of declarations.
const (
	CallSite     NodeKind = "CallSite"
	ReadSite     NodeKind = "ReadSite"
	WriteSite    NodeKind = "WriteSite"
	TypeUse      NodeKind = "TypeUse"
	CaptureSite  NodeKind = "CaptureSite"
	ResourceUse  NodeKind = "ResourceUse"
	DecoratorUse NodeKind = "DecoratorUse"
)

// IsPublic reports membership in the graph vocabulary. The string type remains
// open so internal analysis may extend it without exporting those intermediate kinds.
func (k NodeKind) IsPublic() bool {
	switch k {
	case DocumentNodeKind, DirectoryNodeKind, Import, Export, Struct, Interface,
		Field, Method, Function, Type, TypeAlias, Class, Constructor, Variable, Constant,
		Module, Package, Enum, Record, Namespace, Property, Trait, Macro, Union,
		CallSite, ReadSite, WriteSite, TypeUse, CaptureSite, ResourceUse, DecoratorUse:
		return true
	}
	return false
}

func (k RelationKind) IsPublic() bool {
	switch k {
	case InDirectory, InNamespace, Encloses, Contains, Declares, Imports, Calls,
		Reads, Writes, UsesType, Captures, UsesResource, Extends, Implements, OccursIn,
		Aliases, Exports, Decorates, BindsTo:
		return true
	}
	return false
}

// IsDependency identifies semantic reachability, independent of source structure.
func (k RelationKind) IsDependency() bool {
	switch k {
	case Calls, Reads, Writes, UsesType, Captures, UsesResource, Aliases, Extends, Implements:
		return true
	}
	return false
}

func (k NodeKind) IsOccurrence() bool {
	switch k {
	case CallSite, ReadSite, WriteSite, TypeUse, CaptureSite, ResourceUse, DecoratorUse:
		return true
	}
	return false
}

// OccurrenceKind returns the source view for a semantic operation.
func (k RelationKind) OccurrenceKind() NodeKind {
	switch k {
	case Calls:
		return CallSite
	case Reads:
		return ReadSite
	case Writes:
		return WriteSite
	case UsesType, Extends, Implements:
		return TypeUse
	case Captures:
		return CaptureSite
	case UsesResource:
		return ResourceUse
	case Decorates:
		return DecoratorUse
	}
	return ""
}
