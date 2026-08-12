#!/usr/bin/env node
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";
import { tsImport } from "tsx/esm/api";

const source = process.argv[2];
if (!source || process.argv.length !== 3) {
  console.error("usage: boatstack-flow-frontend <flow.ts>");
  process.exit(2);
}

const absolute = resolve(source);
const loaded = await tsImport(pathToFileURL(absolute).href, import.meta.url);
const exported =
  loaded.default?.__esModule && loaded.default.default
    ? loaded.default.default
    : loaded.default;
if (!exported || typeof exported !== "object") {
  throw new Error("Flow module must default-export a Control Program IR object");
}
process.stdout.write(`${JSON.stringify(exported)}\n`);
