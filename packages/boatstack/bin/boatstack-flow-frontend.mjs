#!/usr/bin/env node
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import process from "node:process";
import ts from "typescript";

const trustedModules = new Set([
  "@operatorstack/boatstack",
  "@operatorstack/boatstack-software-delivery",
]);

async function readInput() {
  if (process.argv.length === 3) {
    const path = resolve(process.argv[2]);
    return { name: path, source: await readFile(path, "utf8") };
  }
  if (process.argv.length === 4 && process.argv[2] === "--stdin") {
    const chunks = [];
    for await (const chunk of process.stdin) chunks.push(chunk);
    return { name: resolve(process.argv[3]), source: Buffer.concat(chunks).toString("utf8") };
  }
  throw new Error("usage: boatstack-flow-frontend <flow.ts> | --stdin <source-name>");
}

function propertyName(node) {
  if (ts.isIdentifier(node) || ts.isStringLiteral(node) || ts.isNumericLiteral(node)) {
    return node.text;
  }
  throw new Error("Flow object keys must be static identifiers or literals");
}

async function compile(input) {
  const sourceFile = ts.createSourceFile(
    input.name,
    input.source,
    ts.ScriptTarget.ESNext,
    true,
    ts.ScriptKind.TS,
  );
  if (sourceFile.parseDiagnostics.length > 0) {
    throw new Error("Flow source contains invalid TypeScript syntax");
  }
  const imports = new Map();
  let exported;

  for (const statement of sourceFile.statements) {
    if (ts.isImportDeclaration(statement)) {
      const moduleName = statement.moduleSpecifier.text;
      if (!trustedModules.has(moduleName)) {
        throw new Error(`Flow imports may reference only trusted Boatstack SDKs: ${moduleName}`);
      }
      const clause = statement.importClause;
      if (!clause || clause.name || !clause.namedBindings || !ts.isNamedImports(clause.namedBindings)) {
        throw new Error("Flow imports must use named Boatstack SDK imports");
      }
      const loaded = await import(moduleName);
      for (const element of clause.namedBindings.elements) {
        if (element.isTypeOnly || clause.isTypeOnly) continue;
        const importedName = element.propertyName?.text ?? element.name.text;
        if (!(importedName in loaded)) {
          throw new Error(`Trusted SDK export is unavailable: ${moduleName}.${importedName}`);
        }
        imports.set(element.name.text, loaded[importedName]);
      }
      continue;
    }
    if (ts.isExportAssignment(statement) && !statement.isExportEquals && exported === undefined) {
      exported = statement.expression;
      continue;
    }
    if (ts.isEmptyStatement(statement)) continue;
    throw new Error("Flow source may contain only trusted imports and one default export");
  }
  if (!exported) throw new Error("Flow source must contain one default export");

  const evaluate = (node) => {
    if (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isSatisfiesExpression(node)) {
      return evaluate(node.expression);
    }
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
    if (ts.isNumericLiteral(node)) return Number(node.text);
    if (node.kind === ts.SyntaxKind.TrueKeyword) return true;
    if (node.kind === ts.SyntaxKind.FalseKeyword) return false;
    if (node.kind === ts.SyntaxKind.NullKeyword) return null;
    if (ts.isIdentifier(node)) {
      if (node.text === "undefined") return undefined;
      if (!imports.has(node.text)) throw new Error(`Flow identifier is not a trusted SDK import: ${node.text}`);
      return structuredClone(imports.get(node.text));
    }
    if (ts.isArrayLiteralExpression(node)) return node.elements.map(evaluate);
    if (ts.isObjectLiteralExpression(node)) {
      const value = {};
      for (const property of node.properties) {
        if (!ts.isPropertyAssignment(property)) {
          throw new Error("Flow objects may contain only explicit property assignments");
        }
        value[propertyName(property.name)] = evaluate(property.initializer);
      }
      return value;
    }
    if (ts.isCallExpression(node)) {
      if (!ts.isIdentifier(node.expression) || !imports.has(node.expression.text)) {
        throw new Error("Flow calls may invoke only named trusted SDK imports");
      }
      const callable = imports.get(node.expression.text);
      if (typeof callable !== "function") {
        throw new Error(`Trusted SDK import is not callable: ${node.expression.text}`);
      }
      return callable(...node.arguments.map(evaluate));
    }
    throw new Error(`Flow expression is not declarative: ${ts.SyntaxKind[node.kind]}`);
  };

  const value = evaluate(exported);
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("Flow default export must lower to a Control Program IR object");
  }
  return value;
}

const input = await readInput();
const output = await compile(input);
process.stdout.write(`${JSON.stringify(output)}\n`);
