package codegraph

import (
	"context"
	"path"
	"strings"
)

// publishSymlinks records immediate entry references, not a dereferenced view.
// +spec=Link chains and cycles preserve entry identity; missing targets never create nodes.
// +why=Filesystem discovery and dereference policy belong to the snapshot consumer.
// +link=component://docs/document.md
func (a *graphAssembler) publishSymlinks(ctx context.Context, documents map[string]Document) error {
	hasLinks := false
	for _, doc := range documents {
		if doc.Symlink != "" {
			hasLinks = true
			break
		}
	}
	if !hasLinks {
		return nil
	}
	a.stage = "symlink"
	directories := map[string]bool{".": true}
	for name := range documents {
		for parent := path.Dir(name); ; parent = path.Dir(parent) {
			if directories[parent] {
				break
			}
			directories[parent] = true
		}
	}
	for name, doc := range documents {
		if err := ctx.Err(); err != nil {
			return err
		}
		if doc.Symlink == "" {
			continue
		}
		target, reason := symlinkTarget(doc, documents, directories)
		if reason != "" {
			a.diagnostics = append(a.diagnostics, Diagnostic{Code: "unresolved_symlink", Subject: RelationsSubject, Relation: LinksTo,
				Location: Location{Path: name}, Message: "symlink target " + doc.Symlink + ": " + reason})
			continue
		}
		targetID := DirectoryID(target)
		if _, ok := documents[target]; ok {
			targetID = DocumentID(target)
		}
		if err := a.addRelation(Relation{Source: DocumentID(name), Target: targetID, Kind: LinksTo, Confidence: Exact,
			Evidence: []Evidence{{Basis: "symlink_target", Confidence: Exact}}}); err != nil {
			return err
		}
	}
	return nil
}

// symlinkTarget walks known directory entries before interpreting '..'. Cleaning
// the target first would incorrectly resolve paths through missing entries or links.
func symlinkTarget(doc Document, documents map[string]Document, directories map[string]bool) (string, string) {
	if path.IsAbs(doc.Symlink) {
		return "", "absolute target is outside snapshot-relative resolution"
	}
	current := path.Dir(doc.Path)
	for _, part := range strings.Split(doc.Symlink, "/") {
		if entry, ok := documents[current]; ok && current != "." {
			if entry.Symlink != "" {
				return "", "target traverses a symlink entry: " + current
			}
			if entry.Gitlink != "" {
				return "", "target traverses an opaque gitlink: " + current
			}
			return "", "target traverses a file entry: " + current
		}
		if !directories[current] {
			return "", "directory is not supplied: " + current
		}
		switch part {
		case "", ".":
		case "..":
			if current == "." {
				return "", "target escapes the snapshot"
			}
			current = path.Dir(current)
		default:
			current = path.Join(current, part)
		}
	}
	if _, ok := documents[current]; ok {
		return current, ""
	}
	if directories[current] {
		return current, ""
	}
	return "", "target entry is not supplied: " + current
}
