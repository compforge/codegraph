"""Independent CPython syntax inventory plus reviewed source bindings.

Only parse source: importing the target package could run application code and
would conflate one execution with a complete static reference oracle.
"""

import argparse
import ast
import hashlib
import io
import json
import platform
import tokenize
from pathlib import Path


def key(site: dict) -> str:
    return f"{site['path']}:{site['start']}:{site['end']}"


class Source:
    def __init__(self, path: str, data: bytes):
        self.path = path
        # Byte identities must match the input supplied to CodeGraph. Non-UTF-8
        # files need an explicit mapping rather than silently changing offsets.
        self.text = data.decode("utf-8")
        self.lines = self.text.splitlines(keepends=True)
        self.offsets = [0]
        for line in self.lines:
            self.offsets.append(self.offsets[-1] + len(line.encode("utf-8")))
        self.tree = ast.parse(self.text, filename=path)
        self.tokens = list(tokenize.generate_tokens(io.StringIO(self.text).readline))

    def site(self, node: ast.AST) -> dict:
        return {
            "path": self.path,
            "start": self.offsets[node.lineno - 1] + node.col_offset,
            "end": self.offsets[node.end_lineno - 1] + node.end_col_offset,
        }

    def definition_span(self, node: ast.AST) -> dict:
        span = self.site(node)
        # CPython omits trailing comments while concrete syntax ranges include
        # them. Normalize only same-line trailing trivia using stdlib tokens.
        for token in self.tokens:
            if token.type == tokenize.COMMENT and token.start[0] == node.end_lineno:
                comment = self.token_site(token)
                between = self.text.encode("utf-8")[span["end"] : comment["start"]]
                if comment["start"] >= span["end"] and not between.strip():
                    span["end"] = comment["end"]
        return span

    def token_site(self, token: tokenize.TokenInfo) -> dict:
        def offset(position: tuple[int, int]) -> int:
            row, col = position
            return self.offsets[row - 1] + len(
                self.lines[row - 1][:col].encode("utf-8")
            )

        return {
            "path": self.path,
            "start": offset(token.start),
            "end": offset(token.end),
        }

    def identifier(self, node: ast.AST, name: str, *, last: bool = False) -> dict:
        bounds = self.site(node)
        matches = [
            self.token_site(t)
            for t in self.tokens
            if t.type == tokenize.NAME
            and t.string == name
            and bounds["start"] <= self.token_site(t)["start"]
            and self.token_site(t)["end"] <= bounds["end"]
        ]
        if not matches:
            raise ValueError(f"identifier {name!r} absent in {bounds}")
        return matches[-1 if last else 0]


def collect(root: Path, source_root: str) -> dict:
    prefix = Path(source_root)
    if prefix.is_absolute() or ".." in prefix.parts or not (root / prefix).is_dir():
        raise ValueError(f"invalid source root: {source_root}")
    oracle = {
        name: {}
        for name in (
            "declarations",
            "references",
            "calls",
            "imports",
            "excludedReferences",
        )
    }
    oracle["module"] = ""
    inventory, symbols, selectors = [], {}, {"references": {}, "calls": {}}

    for path in sorted(root.rglob("*.py")):
        relative = path.relative_to(root)
        data = path.read_bytes()
        included = relative.is_relative_to(prefix)
        inventory.append(
            {
                "path": relative.as_posix(),
                "state": "included" if included else "outside_source_root",
                "sha256": hashlib.sha256(data).hexdigest(),
            }
        )
        if not included:
            continue
        source = Source(relative.as_posix(), data)

        def occurrence(
            group: str, node: ast.AST, name: str, site: dict | None = None
        ) -> None:
            location = site or source.site(node)
            k = key(location)
            oracle[group][k] = {"site": location, "name": name, "class": "unknown"}
            if group in selectors:
                selectors[group][k] = {
                    "path": source.path,
                    "line": node.lineno,
                    "text": ast.get_source_segment(source.text, node),
                    "name": name,
                }

        def declaration(
            node: ast.AST, name: str, kind: str, supported: bool, qualified: str = ""
        ) -> None:
            name_site = source.identifier(node, name)
            d = {
                "name": name,
                "kind": kind,
                "nameSite": name_site,
                "span": source.definition_span(node)
                if supported
                else source.site(node),
                "supported": supported,
            }
            if not supported:
                d["reason"] = "non_graph_binding"
            oracle["declarations"][key(name_site)] = d
            if qualified:
                symbols.setdefault(source.path + ":" + qualified, []).append(
                    key(name_site)
                )

        def visit(node: ast.AST, owners: tuple = ()) -> None:
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef, ast.ClassDef)):
                kind = (
                    "Class"
                    if isinstance(node, ast.ClassDef)
                    else "Method"
                    if owners and owners[-1][1] == "Class"
                    else "Function"
                )
                qualified = ".".join([name for name, _ in owners] + [node.name])
                declaration(node, node.name, kind, True, qualified)
                owners = (*owners, (node.name, kind))
            elif isinstance(node, ast.arg):
                declaration(node, node.arg, "Variable", False)
            elif isinstance(node, ast.Name):
                if isinstance(node.ctx, ast.Store):
                    declaration(node, node.id, "Variable", False)
                elif isinstance(node.ctx, ast.Load):
                    occurrence("references", node, node.id)
            elif isinstance(node, ast.Attribute) and isinstance(node.ctx, ast.Load):
                occurrence(
                    "references",
                    node,
                    node.attr,
                    source.identifier(node, node.attr, last=True),
                )
            elif isinstance(node, ast.Call):
                occurrence("calls", node, ast.unparse(node.func))
            elif isinstance(node, ast.Import):
                for alias in node.names:
                    occurrence("imports", alias, alias.name)
            elif isinstance(node, ast.ImportFrom):
                occurrence("imports", node, "." * node.level + (node.module or ""))
            for child in ast.iter_child_nodes(node):
                visit(child, owners)

        visit(source.tree)
    if not any(item["state"] == "included" for item in inventory):
        raise ValueError("no Python source documents")
    return {
        "oracle": oracle,
        "inputs": inventory,
        "symbols": symbols,
        "selectors": selectors,
        "python": platform.python_version(),
    }


def apply_bindings(result: dict, reviewed: list[dict]) -> None:
    """Require each reviewed selection and target to survive the pinned snapshot."""
    used = set()
    for item in reviewed:
        group = item["relation"]
        if group not in ("references", "calls") or not item.get("reason"):
            raise ValueError(f"invalid reviewed binding: {item}")
        selected = [
            k for k, v in result["selectors"][group].items() if v == item["source"]
        ]
        if len(selected) != 1:
            raise ValueError(
                f"reviewed source must match once: {item['source']} ({len(selected)} matches)"
            )
        k = selected[0]
        if (group, k) in used:
            raise ValueError(f"duplicate reviewed source: {group} {k}")
        used.add((group, k))
        occurrence = result["oracle"][group][k]
        classification = item["class"]
        if classification == "internal":
            target = item["target"]
            candidates = result["symbols"].get(
                target["path"] + ":" + target["qualifiedName"], []
            )
            if len(candidates) != 1:
                raise ValueError(f"reviewed target must match once: {target}")
            occurrence["target"] = candidates[0]
        elif classification not in ("external", "runtime_dispatch") or "target" in item:
            raise ValueError(f"invalid reviewed classification: {item}")
        occurrence["class"] = classification


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("source_root")
    parser.add_argument("bindings", type=Path)
    args = parser.parse_args()
    result = collect(args.root, args.source_root)
    reviewed = json.loads(args.bindings.read_text())
    if not reviewed:
        raise ValueError("reviewed binding list must not be empty")
    apply_bindings(result, reviewed)
    # Selectors are retained as evidence for manual auditing; never derive truth
    # from CodeGraph output or use these to auto-accept new bindings.
    print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    main()
