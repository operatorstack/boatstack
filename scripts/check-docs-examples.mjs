import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";

const source = "docs/typescript/examples/product-delivery.flow.ts";
const raw = execFileSync(
  process.execPath,
  ["packages/boatstack/bin/boatstack-flow-frontend.mjs", source],
  { encoding: "utf8" },
);
const program = JSON.parse(raw);

assert.equal(program.program.id, "example-product");
assert.equal(program.program.human_identity, "developer");
assert.deepEqual(program.entries[0].requires.authorities, ["human"]);
assert.equal(program.entries[0].delegation.reference, "software-delivery/delegation/autonomy");
assert.equal(program.targets[0].id, "active-plan");

console.log("documentation Flow example compiled through the restricted frontend");
