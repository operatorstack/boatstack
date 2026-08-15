import assert from "node:assert/strict";
import test from "node:test";

import { documentationFailures } from "./check-docs-api.mjs";

const requiredFunctions = {
  "@operatorstack/boatstack": ["defineFlow", "entry", "marked"],
  "@operatorstack/boatstack-software-delivery": [
    "inbox",
    "trustedDelegation",
    "trustedTransition",
  ],
};

function modelWithRequiredFunctions() {
  return {
    children: Object.entries(requiredFunctions).map(([name, functions]) => ({
      name,
      kind: 2,
      children: functions.map((functionName) => ({
        name: functionName,
        kind: 64,
      })),
    })),
  };
}

test("accepts required top-level function exports", () => {
  assert.deepEqual(documentationFailures(modelWithRequiredFunctions()), []);
});

test("rejects a required name that exists only on a nested member", () => {
  const model = modelWithRequiredFunctions();
  const core = model.children.find(
    (reflection) => reflection.name === "@operatorstack/boatstack",
  );
  core.children = core.children.filter(
    (reflection) => reflection.name !== "defineFlow",
  );
  core.children.push({
    name: "MisleadingInterface",
    kind: 256,
    children: [{ name: "defineFlow", kind: 1024 }],
  });

  assert.deepEqual(documentationFailures(model), [
    "missing documented export @operatorstack/boatstack.defineFlow",
  ]);
});
