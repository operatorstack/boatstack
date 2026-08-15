import { readFile } from "node:fs/promises";

const model = JSON.parse(await readFile("build/docs/api.json", "utf8"));
const required = new Map([
  ["@operatorstack/boatstack", ["defineFlow", "entry", "marked"]],
  [
    "@operatorstack/boatstack-software-delivery",
    ["inbox", "trustedDelegation", "trustedTransition"],
  ],
]);

function reflectionNames(node, names = new Set()) {
  if (Array.isArray(node)) {
    for (const value of node) reflectionNames(value, names);
  } else if (node && typeof node === "object") {
    if (typeof node.name === "string") names.add(node.name);
    for (const value of Object.values(node)) reflectionNames(value, names);
  }
  return names;
}

const packages = new Map(
  (model.children ?? []).map((child) => [child.name, reflectionNames(child)]),
);
const failures = [];

for (const [packageName, exports] of required) {
  const names = packages.get(packageName);
  if (!names) {
    failures.push(`missing documented package ${packageName}`);
    continue;
  }
  for (const exportName of exports) {
    if (!names.has(exportName)) {
      failures.push(`missing documented export ${packageName}.${exportName}`);
    }
  }
}

if (failures.length > 0) {
  console.error(failures.join("\n"));
  process.exit(1);
}

console.log("required public TypeScript SDK exports are documented");
