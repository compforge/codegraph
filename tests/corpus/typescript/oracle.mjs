// Independent TypeScript source oracle. Target application code is never loaded.
import ts from "typescript";
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";

const pinnedVersion = "5.6.3";
const key = (s) => `${s.path}:${s.start}:${s.end}`;
const sourceExtension = /\.(?:ts|tsx|mts|cts)$/;
const declarationExtension = /\.d\.(?:ts|mts|cts)$/;
const slash = (p) => p.split(path.sep).join("/");

function walk(root) {
  const files = [];
  for (const entry of fs.readdirSync(root, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    if (entry.name === "node_modules" || entry.name === ".git") continue;
    const name = path.join(root, entry.name);
    if (entry.isDirectory()) files.push(...walk(name));
    else if (entry.isFile()) files.push(name);
  }
  return files;
}

function within(root, name) {
  const relative = path.relative(root, name);
  return relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative);
}

// Only verified source workspace links are materialized. TypeScript's native
// resolver still owns exports, conditions and tsconfig path interpretation.
function linkWorkspaces(root, workspaces) {
  for (const directory of workspaces) {
    const target = path.resolve(root, directory);
    if (!within(root, target)) throw new Error(`workspace escapes root: ${directory}`);
    const manifest = JSON.parse(fs.readFileSync(path.join(target, "package.json"), "utf8"));
    if (!/^(?:@[a-zA-Z0-9._-]+\/)?[a-zA-Z0-9._-]+$/.test(manifest.name)) throw new Error("invalid workspace package name");
    const link = path.join(root, "node_modules", manifest.name);
    fs.mkdirSync(path.dirname(link), { recursive: true });
    if (fs.existsSync(link)) {
      if (fs.realpathSync(link) !== fs.realpathSync(target)) throw new Error(`unexpected workspace link: ${link}`);
    } else fs.symlinkSync(target, link, "dir");
  }
}

function declarationShape(node) {
  if (ts.isFunctionDeclaration(node)) return ["Function", !!node.body, "overload_or_ambient"];
  if (ts.isClassDeclaration(node)) return ["Class", true, ""];
  if (ts.isInterfaceDeclaration(node)) return ["Interface", true, ""];
  if (ts.isTypeAliasDeclaration(node)) return ["TypeAlias", true, ""];
  if (ts.isEnumDeclaration(node)) return ["Enum", true, ""];
  if (ts.isModuleDeclaration(node)) return ["Module", true, ""];
  if (ts.isConstructorDeclaration(node)) return ["Method", !!node.body, "constructor_signature"];
  if (ts.isMethodDeclaration(node)) return ["Method", !!node.body, "method_signature"];
  if (ts.isMethodSignature(node)) return ["Method", false, "method_signature"];
  if (ts.isGetAccessorDeclaration(node) || ts.isSetAccessorDeclaration(node)) return ["Method", true, ""];
  if (ts.isVariableDeclaration(node)) return ["Variable", ts.isIdentifier(node.name), "destructuring"];
  if (ts.isPropertyDeclaration(node) || ts.isPropertySignature(node)) return ["Property", false, "property"];
  if (ts.isEnumMember(node)) return ["Constant", false, "enum_member"];
  if (ts.isParameter(node)) return ["Parameter", false, "parameter"];
  if (ts.isTypeParameterDeclaration(node)) return ["TypeParameter", false, "type_parameter"];
  if (ts.isBindingElement(node)) return ["Variable", false, "destructuring"];
  if (ts.isPropertyAssignment(node) || ts.isShorthandPropertyAssignment(node)) return ["Property", false, "object_property"];
  if (ts.isFunctionExpression(node) || ts.isClassExpression(node)) return ["ExpressionName", false, "expression_name"];
  return null;
}

function declarationStart(node, sf) {
  let start = node.getStart(sf);
  // export/default belong to the surrounding module export statement; the
  // declaration's own async/abstract/accessibility modifiers remain in its span.
  for (const modifier of ts.canHaveModifiers(node) ? ts.getModifiers(node) ?? [] : []) {
    if (modifier.kind !== ts.SyntaxKind.ExportKeyword && modifier.kind !== ts.SyntaxKind.DefaultKeyword) break;
    const scanner = ts.createScanner(ts.ScriptTarget.Latest, true, sf.languageVariant, sf.text, undefined, modifier.end);
    scanner.scan();
    start = scanner.getTokenPos();
  }
  return start;
}

export function analyze(root, profile) {
  if (ts.version !== pinnedVersion) throw new Error(`TypeScript ${pinnedVersion} required; got ${ts.version}`);
  root = fs.realpathSync(root);
  if (!profile.projects?.length) throw new Error("profile requires projects");
  linkWorkspaces(root, profile.workspaces ?? []);
  const oracle = { organizations: {}, module: "", declarations: {}, references: {}, calls: {}, imports: {}, excludedReferences: {} };
  // JavaScript is an explicit profile choice; existing TS corpus denominators stay fixed.
  const sourceFiles = walk(root).filter((p) => sourceExtension.test(p) || profile.includeJavaScript === true && /\.(?:js|jsx|mjs|cjs)$/.test(p));
  const inputs = new Map();
  const included = new Set();
  const hashInput = (absolute, state) => {
    if (!within(root, absolute)) return;
    const relative = slash(path.relative(root, absolute));
    inputs.set(relative, { path: relative, state, sha256: createHash("sha256").update(fs.readFileSync(absolute)).digest("hex") });
  };
  for (const file of sourceFiles) hashInput(file, declarationExtension.test(file) ? "declaration_support" : "outside_source_roots");
  for (const file of walk(root)) if (["package.json", "tsconfig.json"].includes(path.basename(file))) hashInput(file, "configuration");
  const programs = [];
  const diagnostics = [];
  for (const project of profile.projects) {
    const config = path.resolve(root, project.config);
    const sourceRoot = path.resolve(root, project.sourceRoot);
    if (!within(root, config) || !within(root, sourceRoot)) throw new Error("project escapes root");
    const read = (name) => {
      if (fs.existsSync(name) && within(root, name) && name.endsWith(".json")) hashInput(name, "configuration");
      return within(root, name) ? ts.sys.readFile(name) : undefined;
    };
    const configFile = ts.readConfigFile(config, read);
    if (configFile.error) throw new Error(ts.flattenDiagnosticMessageText(configFile.error.messageText, "\n"));
    const parsed = ts.parseJsonConfigFileContent(configFile.config, { ...ts.sys, readFile: read }, path.dirname(config));
    if (parsed.errors.length) throw new Error(ts.formatDiagnosticsWithColorAndContext(parsed.errors, { getCanonicalFileName: (n) => n, getCurrentDirectory: () => root, getNewLine: () => "\n" }));
    const allowed = new Set(parsed.fileNames.map((f) => path.resolve(f)));
    const names = sourceFiles.filter((f) => within(sourceRoot, f) && !declarationExtension.test(f) && allowed.has(f));
    if (!names.length) throw new Error(`no source files in ${project.sourceRoot}`);
    for (const name of names) {
      if (included.has(name)) throw new Error(`overlapping project input: ${name}`);
      included.add(name); hashInput(name, "included");
    }
    const host = ts.createCompilerHost(parsed.options);
    // Ignore unrelated ambient packages in parent directories and on the host.
    // The snapshot contains only explicitly linked source workspaces.
    const originalRead = host.readFile;
    const compilerLib = path.dirname(ts.getDefaultLibFilePath(parsed.options));
    host.readFile = (name) => within(root, name) || within(compilerLib, name) ? originalRead(name) : undefined;
    host.fileExists = (name) => (within(root, name) || within(compilerLib, name)) && ts.sys.fileExists(name);
    const program = ts.createProgram({ rootNames: [...names, ...parsed.fileNames.filter((f) => declarationExtension.test(f))], options: { ...parsed.options, noEmit: true }, host });
    if (program.getSyntacticDiagnostics().length) throw new Error(`TypeScript syntax diagnostics in ${project.config}: ${program.getSyntacticDiagnostics().map((d) => ts.flattenDiagnosticMessageText(d.messageText, "\n")).join("; ")}`);
    programs.push({ program, names, checker: program.getTypeChecker(), config: project.config });
  }
  const byteTables = new Map();
  const site = (sf, start, end) => {
    let offsets = byteTables.get(sf.fileName);
    if (!offsets) {
      offsets = new Uint32Array(sf.text.length + 1);
      let bytes = 0;
      for (let i = 0; i < sf.text.length;) {
        const point = sf.text.codePointAt(i), chars = point > 0xffff ? 2 : 1;
        offsets[i] = bytes;
        if (chars === 2) offsets[i + 1] = bytes;
        bytes += Buffer.byteLength(String.fromCodePoint(point));
        i += chars; offsets[i] = bytes;
      }
      byteTables.set(sf.fileName, offsets);
    }
    return { path: slash(path.relative(root, sf.fileName)), start: offsets[start], end: offsets[end] };
  };
  const declarationNames = new Set();
  for (const { program, names } of programs) for (const name of names) {
    const sf = program.getSourceFile(name);
    if (!sf) throw new Error(`compiler omitted ${name}`);
    const relative = slash(path.relative(root, name));
    oracle.organizations[`unit:${relative}`] = {kind:"Module", name:path.basename(name, path.extname(name)), qualifiedName:relative.slice(0,-path.extname(name).length), contributions:{[relative]:site(sf,0,sf.text.length)}};
    const visit = (node) => {
      const shape = declarationShape(node);
      const declarationName = ts.isConstructorDeclaration(node) ? node.getChildren(sf).find((child) => child.kind === ts.SyntaxKind.ConstructorKeyword) : node.name;
      if (shape && declarationName && !ts.isObjectBindingPattern(declarationName) && !ts.isArrayBindingPattern(declarationName)) {
        const nameSite = site(sf, declarationName.getStart(sf), declarationName.end);
        let [kind, supported, reason] = shape;
        if (ts.isComputedPropertyName(declarationName)) { supported = false; reason = "computed_name"; }
        const name = declarationName.text ?? declarationName.getText(sf);
        oracle.declarations[key(nameSite)] = { name, kind, nameSite, span: site(sf, declarationStart(node, sf), node.end), supported, ...(supported ? {} : { reason }) };
        if (!ts.isShorthandPropertyAssignment(node)) declarationNames.add(key(nameSite));
      }
      if (ts.isImportDeclaration(node) || ts.isImportEqualsDeclaration(node) || ts.isExportDeclaration(node)) {
        const bindingNames = (n) => { if (ts.isIdentifier(n)) declarationNames.add(key(site(sf, n.getStart(sf), n.end))); ts.forEachChild(n, bindingNames); };
        bindingNames(node);
      }
      ts.forEachChild(node, visit);
    };
    visit(sf);
  }
  for (const { program, checker, names, config } of programs) {
    for (const d of [...program.getOptionsDiagnostics(), ...program.getGlobalDiagnostics(), ...program.getSemanticDiagnostics()]) {
      diagnostics.push({ project: config, code: d.code, message: ts.flattenDiagnosticMessageText(d.messageText, "\n").replaceAll(root, "<snapshot>"), ...(d.file ? { site: site(d.file, d.start ?? 0, (d.start ?? 0) + (d.length ?? 0)) } : {}) });
    }
    const targetOf = (symbol) => {
      if (!symbol) return { class: "unknown", reason: "unresolved_symbol" };
      if (symbol.flags & ts.SymbolFlags.Alias) symbol = checker.getAliasedSymbol(symbol);
      const declarations = symbol?.declarations ?? [];
      if (declarations.some(ts.isSourceFile)) return { class: "module" };
      const implementations = declarations.filter((d) => ts.isFunctionDeclaration(d) && d.body);
      const candidates = implementations.length === 1 ? implementations : declarations;
      if (candidates.length !== 1) return { class: "unknown", reason: candidates.length ? "multiple_declarations" : "unresolved_alias" };
      const declaration = candidates[0], sf = declaration.getSourceFile();
      if (!included.has(path.resolve(sf.fileName))) {
        // A compiler-resolved library/support declaration is known outside the
        // measured graph. Unresolved imports never enter this branch.
        return { class: "external" };
      }
      if (!declaration.name || !ts.isIdentifier(declaration.name) && !ts.isStringLiteral(declaration.name) && !ts.isNumericLiteral(declaration.name)) return { class: "unknown", reason: "unnamed_declaration" };
      const target = key(site(sf, declaration.name.getStart(sf), declaration.name.end));
      const truth = oracle.declarations[target];
      if (!truth) return { class: "unknown", reason: "outside_inventory" };
      return { class: truth.supported ? "internal" : "outside_contract", target };
    };
    for (const name of names) {
      const sf = program.getSourceFile(name);
      const visit = (node) => {
        if (ts.isIdentifier(node) || ts.isPrivateIdentifier(node)) {
          const at = site(sf, node.getStart(sf), node.end), id = key(at);
          const isLabel = ts.isLabeledStatement(node.parent) || ts.isBreakStatement(node.parent) || ts.isContinueStatement(node.parent);
          if (!declarationNames.has(id) && !isLabel) {
            const symbol = ts.isShorthandPropertyAssignment(node.parent) ? checker.getShorthandAssignmentValueSymbol(node.parent) : checker.getSymbolAtLocation(node);
            const occurrence = { site: at, name: node.text, ...targetOf(symbol) };
            if (occurrence.class === "module") oracle.excludedReferences[id] = occurrence;
            else oracle.references[id] = occurrence;
          }
        }
        if (ts.isCallExpression(node) || ts.isNewExpression(node)) {
          const at = site(sf, node.getStart(sf), node.end);
          const expression = node.expression;
          const location = ts.isPropertyAccessExpression(expression) ? expression.name : expression;
          let symbol = checker.getSymbolAtLocation(location);
          if (symbol?.flags & ts.SymbolFlags.Alias) symbol = checker.getAliasedSymbol(symbol);
          const declarations = symbol?.declarations ?? [];
          const direct = declarations.some((d) => ts.isFunctionDeclaration(d) && !!d.body || ts.isNewExpression(node) && ts.isClassDeclaration(d));
          const target = targetOf(symbol);
          // A resolved method/callback signature does not prove a runtime target.
          const binding = direct || target.class === "unknown" || target.class === "external" ? target : { class: "runtime_dispatch" };
          oracle.calls[key(at)] = { site: at, name: location.getText(sf), ...binding };
        }
        if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier)) {
          const m = node.moduleSpecifier, at = site(sf, m.getStart(sf), m.end);
          oracle.imports[key(at)] = { site: at, name: m.text, class: "source" };
          const moduleSymbol = checker.getSymbolAtLocation(m);
          const files = (moduleSymbol?.declarations ?? []).filter(ts.isSourceFile).filter(f => included.has(path.resolve(f.fileName)));
          if (files.length === 1) Object.assign(oracle.imports[key(at)], {class:"internal", target:`unit:${slash(path.relative(root,files[0].fileName))}`});
        }
        ts.forEachChild(node, visit);
      };
      visit(sf);
    }
  }
  return { oracle, inputs: [...inputs.values()].sort((a, b) => a.path.localeCompare(b.path)), toolchain: `Node ${process.version}; TypeScript ${ts.version}`, diagnostics };
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const [, , root, profileFile] = process.argv;
    process.stdout.write(JSON.stringify(analyze(root, JSON.parse(fs.readFileSync(profileFile, "utf8")))));
  } catch (error) {
    process.stderr.write(`${error.stack ?? error}\n`);
    process.exitCode = 1;
  }
}
