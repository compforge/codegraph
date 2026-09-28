import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { analyze } from "./oracle.mjs";

const config = JSON.stringify({ compilerOptions: { target: "ES2022", module: "ESNext", moduleResolution: "bundler", strict: true }, include: ["src"] });
const profile = { projects: [{ config: "tsconfig.json", sourceRoot: "src" }] };
function fixture(t, files) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "typescript-oracle-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const [name, content] of Object.entries(files)) {
    const full = path.join(root, name);
    fs.mkdirSync(path.dirname(full), { recursive: true });
    fs.writeFileSync(full, content);
  }
  return root;
}
const occurrences = (o, field, name) => Object.values(o[field]).filter((x) => x.name === name);

test("compiler resolves aliases, re-exports, shorthand and signatures with UTF-8 byte sites", (t) => {
  const source = 'import { target as run } from "./barrel";\n// 中文😀\nexport function entry(cb: () => void) { const obj = { run }; run(); cb(); return obj; }';
  const root = fixture(t, { "tsconfig.json": config, "src/main.ts": source, "src/lib.ts": "export function target() {}", "src/barrel.ts": 'export {target} from "./lib";' });
  const { oracle: o } = analyze(root, profile);
  const call = occurrences(o, "calls", "run")[0];
  assert.equal(call.class, "internal");
  assert.equal(o.declarations[call.target].name, "target");
  assert.equal(call.site.start, Buffer.byteLength(source.slice(0, source.indexOf("run();"))));
  assert.equal(occurrences(o, "calls", "cb")[0].class, "runtime_dispatch");
  assert.ok(occurrences(o, "references", "run").every((x) => x.target === call.target));
  const entry = Object.values(o.declarations).find((x) => x.name === "entry");
  assert.equal(entry.span.start, Buffer.byteLength(source.slice(0, source.indexOf("function entry"))));
  assert.ok(Object.values(o.declarations).some((x) => x.name === "cb" && !x.supported));
});

test("unavailable dependencies are diagnostics and unknown; method signatures are not runtime targets", (t) => {
  const root = fixture(t, { "tsconfig.json": config, "src/main.ts": 'import { missing } from "absent-library"; interface I { run(): void; } export function use(x: I) { missing(); x.run(); }' });
  const result = analyze(root, profile);
  assert.ok(result.diagnostics.some((d) => d.code === 2307));
  assert.equal(occurrences(result.oracle, "calls", "missing")[0].class, "unknown");
  assert.equal(occurrences(result.oracle, "calls", "run")[0].class, "runtime_dispatch");
  assert.ok(Object.values(result.oracle.declarations).some((x) => x.name === "use" && x.supported));
});

test("source workspace exports resolve natively; excluded files remain inventoried", (t) => {
  const root = fixture(t, {
    "tsconfig.json": config,
    "src/main.ts": 'import { work } from "@fixture/lib"; export function entry(){ work(); }',
    "tests/ignored.ts": "throw new Error('must not execute')",
    "src/support.d.ts": "declare const support: string;",
    "lib/package.json": JSON.stringify({ name: "@fixture/lib", type: "module", exports: { ".": "./src/index.ts" } }),
    "lib/tsconfig.json": config,
    "lib/src/index.ts": "export function work() {}",
  });
  const result = analyze(root, { projects: [...profile.projects, { config: "lib/tsconfig.json", sourceRoot: "lib/src" }], workspaces: ["lib"] });
  const call = occurrences(result.oracle, "calls", "work")[0];
  assert.equal(call.class, "internal");
  assert.equal(result.oracle.declarations[call.target].nameSite.path, "lib/src/index.ts");
  assert.equal(result.inputs.find((i) => i.path === "src/support.d.ts").state, "declaration_support");
  assert.equal(result.inputs.find((i) => i.path === "tests/ignored.ts").state, "outside_source_roots");
  assert.ok(result.inputs.some((i) => i.path === "lib/package.json" && i.state === "configuration"));
});

test("syntax failure cannot become an empty oracle", (t) => {
  const root = fixture(t, { "tsconfig.json": config, "src/broken.ts": "export function {" });
  assert.throws(() => analyze(root, profile), /syntax diagnostics/);
});

test("module qualifiers are separate; merged type/value symbols are not guessed", (t) => {
  const root = fixture(t, { "tsconfig.json": config,
    "src/main.ts": 'import * as lib from "./lib"; interface Same {} const Same = 1; function use(x: Same){ return Same + lib.work(); }',
    "src/lib.ts": "export function work(){ return 1; }" });
  const result = analyze(root, profile);
  assert.ok(Object.values(result.oracle.excludedReferences).some((r) => r.name === "lib"));
  const refs = occurrences(result.oracle, "references", "Same");
  assert.equal(refs.length, 2);
  assert.ok(refs.every((r) => r.class === "unknown" && r.reason === "multiple_declarations"));
  assert.deepEqual(Object.values(result.oracle.declarations).filter((d) => d.name === "Same").map((d) => d.kind).sort(), ["Interface", "Variable"]);
});


test("constructors and computed names remain in the independent declaration inventory", (t) => {
  const root = fixture(t, { "tsconfig.json": config, "src/main.ts": "export class Box { constructor(readonly value: string) {} [Symbol.iterator]() { return [][Symbol.iterator](); } }" });
  const { oracle } = analyze(root, profile);
  const declarations = Object.values(oracle.declarations);
  const constructor = declarations.find((d) => d.name === "constructor");
  assert.equal(constructor.kind, "Method");
  assert.equal(constructor.supported, true);
  assert.ok(declarations.some((d) => d.reason === "computed_name" && d.name === "[Symbol.iterator]"));
  assert.ok(declarations.some((d) => d.name === "value" && d.reason === "parameter"));
});
