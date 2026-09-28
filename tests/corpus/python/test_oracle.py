"""Regression contracts for the independent Python reference collector."""

import copy
import tempfile
import unittest
from pathlib import Path

from oracle import apply_bindings, collect


class OracleTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "src").mkdir()

    def write(self, path, source):
        dest = self.root / path
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(source, encoding="utf-8")

    def test_utf8_async_decorators_comments_and_scope(self):
        source = """# 中文
@decorate
class Box:
    @classmethod
    async def run(cls, callback):
        café = "你好"
        await callback(café)  # trailing
"""
        self.write("src/model.py", source)
        result = collect(self.root, "src")
        declarations = result["oracle"]["declarations"]
        run = declarations[result["symbols"]["src/model.py:Box.run"][0]]
        self.assertEqual(run["kind"], "Method")
        content = source.encode()
        self.assertEqual(
            content[run["nameSite"]["start"] : run["nameSite"]["end"]], b"run"
        )
        self.assertTrue(
            content[run["span"]["start"] : run["span"]["end"]].endswith(b"# trailing")
        )
        refs = result["oracle"]["references"].values()
        use = next(r for r in refs if r["name"] == "café")
        self.assertEqual(
            content[use["site"]["start"] : use["site"]["end"]], "café".encode()
        )
        self.assertEqual(len(result["oracle"]["calls"]), 1)
        self.assertTrue(
            all(c["class"] == "unknown" for c in result["oracle"]["calls"].values())
        )

    def test_reviewed_bindings_require_unique_source_and_target(self):
        self.write("src/main.py", "def target(): pass\ndef entry(): target()\n")
        result = collect(self.root, "src")
        item = {
            "relation": "calls",
            "source": {
                "path": "src/main.py",
                "line": 2,
                "text": "target()",
                "name": "target",
            },
            "class": "internal",
            "target": {"path": "src/main.py", "qualifiedName": "target"},
            "reason": "Direct local function without rebinding.",
        }
        apply_bindings(result, [item])
        self.assertEqual(
            next(iter(result["oracle"]["calls"].values()))["class"], "internal"
        )
        with self.assertRaisesRegex(ValueError, "duplicate"):
            apply_bindings(result, [item, item])
        stale = copy.deepcopy(item)
        stale["source"]["line"] = 3
        with self.assertRaisesRegex(ValueError, "source must match once"):
            apply_bindings(result, [stale])
        stale = copy.deepcopy(item)
        stale["target"]["qualifiedName"] = "absent"
        with self.assertRaisesRegex(ValueError, "target must match once"):
            apply_bindings(result, [stale])

    def test_no_execution_and_explicit_source_scope(self):
        self.write("src/main.py", "raise RuntimeError('must never execute')\n")
        self.write("tests/broken.py", "this is not valid Python !")
        result = collect(self.root, "src")
        self.assertEqual(
            [i["state"] for i in result["inputs"]], ["included", "outside_source_root"]
        )
        self.assertEqual(len(result["oracle"]["calls"]), 1)
        with self.assertRaises(ValueError):
            collect(self.root, "../elsewhere")

    def test_syntax_failure_is_not_empty_truth(self):
        self.write("src/broken.py", "def broken(:\n")
        with self.assertRaises(SyntaxError):
            collect(self.root, "src")

    def test_import_inventory_keeps_aliases_relative_paths_and_star(self):
        self.write(
            "src/main.py",
            "import a, b as other\nfrom .model import Box, run\nfrom package import *\n",
        )
        result = collect(self.root, "src")
        self.assertEqual(
            [i["name"] for i in result["oracle"]["imports"].values()],
            ["a", "b", ".model", "package"],
        )
        self.assertEqual(result["oracle"]["references"], {})

    def test_package_organization_and_import_root(self):
        self.write("src/pkg/__init__.py", "")
        self.write("src/pkg/nested/__init__.py", "")
        self.write(
            "src/pkg/nested/work.py",
            "from . import util\nimport asyncio\nfrom pkg.nested import util\n",
        )
        self.write("src/pkg/nested/util.py", "def run(): pass\n")
        self.write("src/pkg/asyncio/__init__.py", "")
        result = collect(self.root, "src/pkg")["oracle"]
        unit = result["organizations"]["unit:src/pkg/nested/work.py"]
        self.assertEqual(unit["qualifiedName"], "pkg.nested.work")
        self.assertEqual(unit["parent"], "unit:src/pkg/nested/__init__.py")
        imports = list(result["imports"].values())
        self.assertEqual(imports[0]["target"], "unit:src/pkg/nested/__init__.py")
        self.assertEqual(imports[1]["class"], "unknown")
        self.assertEqual(imports[2]["target"], "unit:src/pkg/nested/__init__.py")


if __name__ == "__main__":
    unittest.main()
