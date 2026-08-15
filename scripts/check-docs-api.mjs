import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

const required = new Map([
  ["@operatorstack/boatstack", ["defineFlow", "entry", "marked"]],
  [
    "@operatorstack/boatstack-software-delivery",
    ["inbox", "trustedDelegation", "trustedTransition"],
  ],
]);

const FUNCTION_REFLECTION = 64;

export function documentationFailures(model) {
  const packages = new Map(
    (model.children ?? []).map((child) => [
      child.name,
      new Set(
        (child.children ?? [])
          .filter((reflection) => reflection.kind === FUNCTION_REFLECTION)
          .map((reflection) => reflection.name),
      ),
    ]),
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
  return failures;
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  const model = JSON.parse(await readFile("build/docs/api.json", "utf8"));
  const failures = documentationFailures(model);
  if (failures.length > 0) {
    console.error(failures.join("\n"));
    process.exit(1);
  }

  console.log("required public TypeScript SDK exports are documented");
}
