import { access, readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";

const required = new Map([
  [
    "@operatorstack/boatstack",
    [
      "AuthorityRequirements",
      "EntryDefinition",
      "defineFlow",
      "entry",
      "facet",
      "fact",
      "foregroundWork",
      "fromEntryInput",
      "fromReceipt",
      "fromState",
      "fromStateOrReceipt",
      "fromWorkOutput",
      "hostParameter",
      "marked",
      "operator",
      "transition",
      "trustedParameterResolver",
    ],
  ],
  [
    "@operatorstack/boatstack-software-delivery",
    [
      "SoftwareDeliveryFlowDefinition",
      "inbox",
      "planningPackageAdmit",
      "planningPackageApprove",
      "planningPackagePromote",
      "softwareDelivery",
      "trustedDelegation",
      "trustedSoftwareDeliveryTransitions",
      "trustedTransition",
    ],
  ],
]);

const requiredCategories = new Map([
  [
    "@operatorstack/boatstack",
    [
      "Authority",
      "Foreground work",
      "Invocation and parameters",
      "Operators and transitions",
      "Predicates and state",
      "Program structure",
      "Targets and entries",
    ],
  ],
  [
    "@operatorstack/boatstack-software-delivery",
    [
      "Authority and delegation",
      "Flow composition",
      "Parameter producers",
      "Planning",
      "Repository inputs",
      "Trusted transitions",
    ],
  ],
]);

function partsText(parts = []) {
  return parts.map((part) => part.text ?? "").join("");
}

function commentText(reflection) {
  return partsText(reflection?.comment?.summary);
}

function child(reflection, name) {
  return (reflection?.children ?? []).find((candidate) => candidate.name === name);
}

export function documentationFailures(
  model,
  { strict = false, requiredDocuments = [] } = {},
) {
  const packages = new Map((model.children ?? []).map((item) => [item.name, item]));
  const failures = [];

  for (const [packageName, exports] of required) {
    const packageReflection = packages.get(packageName);
    if (!packageReflection) {
      failures.push(`missing documented package ${packageName}`);
      continue;
    }
    const names = new Set(
      (packageReflection.children ?? []).map((reflection) => reflection.name),
    );
    for (const exportName of exports) {
      if (!names.has(exportName)) {
        failures.push(`missing documented export ${packageName}.${exportName}`);
      }
    }
  }

  if (!strict) return failures;

  if (partsText(model.readme).length < 500) {
    failures.push("TypeDoc global landing document is missing or too small");
  }

  const documents = new Set((model.documents ?? []).map((document) => document.name));
  for (const document of requiredDocuments) {
    if (!documents.has(document)) failures.push(`missing TypeDoc project document ${document}`);
  }

  for (const [packageName, categories] of requiredCategories) {
    const packageReflection = packages.get(packageName);
    if (!packageReflection) continue;
    if (commentText(packageReflection).length < 250) {
      failures.push(`package overview is not meaningful: ${packageName}`);
    }
    const actual = new Set(
      (packageReflection.categories ?? []).map((category) => category.title),
    );
    for (const category of categories) {
      if (!actual.has(category)) {
        failures.push(`missing TypeDoc category ${packageName}.${category}`);
      }
    }
  }

  const base = packages.get("@operatorstack/boatstack");
  const entryDefinition = child(base, "EntryDefinition");
  if (!commentText(entryDefinition).includes("activation authority") || !child(entryDefinition, "requires")) {
    failures.push("EntryDefinition.requires is not documented as entry activation authority");
  }

  const software = packages.get("@operatorstack/boatstack-software-delivery");
  const softwareDefinition = child(software, "SoftwareDeliveryFlowDefinition");
  if (!commentText(child(softwareDefinition, "humanIdentity")).includes("identity role")) {
    failures.push("SoftwareDeliveryFlowDefinition.humanIdentity is not documented as an identity role");
  }

  return failures;
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  const config = JSON.parse(await readFile("typedoc.json", "utf8"));
  for (const document of config.projectDocuments ?? []) await access(document);

  const model = JSON.parse(await readFile("build/docs/api.json", "utf8"));
  const requiredDocuments = (config.projectDocuments ?? []).map((path) =>
    path.replace(/^docs\//, "").replace(/\.md$/, ""),
  );
  const failures = documentationFailures(model, { strict: true, requiredDocuments });

  const htmlChecks = [
    ["build/docs/html/index.html", ["Boatstack TypeScript SDK", "Flow anatomy"]],
    [
      "build/docs/html/modules/_operatorstack_boatstack.html",
      ["Program structure", "Invocation and parameters", "Targets and entries"],
    ],
    [
      "build/docs/html/modules/_operatorstack_boatstack-software-delivery.html",
      ["Flow composition", "Planning", "Authority and delegation"],
    ],
    ["build/docs/html/documents/typescript_flow-anatomy.html", ["Flow anatomy"]],
    [
      "build/docs/html/documents/product-delivery_planning-and-foreground-work.html",
      ["Planning and foreground work"],
    ],
  ];
  for (const [path, fragments] of htmlChecks) {
    const html = await readFile(path, "utf8");
    for (const fragment of fragments) {
      if (!html.includes(fragment)) failures.push(`${path} does not contain ${fragment}`);
    }
  }

  if (failures.length > 0) {
    console.error(failures.join("\n"));
    process.exit(1);
  }

  console.log("TypeDoc packages, concepts, guides, categories, and public exports are documented");
}
