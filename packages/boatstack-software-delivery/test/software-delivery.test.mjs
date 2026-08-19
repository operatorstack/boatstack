import assert from "node:assert/strict";
import test from "node:test";

import {
  planningPackage,
  planningPackagePromote,
  softwareDelivery,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
  workPackage,
  workPackageAdmit,
  workPackageApprove,
} from "../dist/index.js";

const target = { id: "done", predicate: { true: true } };
const entry = {
  id: "run",
  target: "done",
  requires: { authorities: ["human"] },
};
const work = (id) => ({
  id,
  instructions: { path: `${id}.md` },
  inputs: [],
  outputs: [{ id: "implementation-plan", path: `${id}.out.md`, media_type: "text/markdown", required: true }],
});

function definition(overrides = {}) {
  return {
    id: "example",
    version: "1",
    humanIdentity: "developer",
    lifecycle: [{ id: "plan.activate", priority: 50 }],
    targets: [target],
    entries: [entry],
    ...overrides,
  };
}

test("composes canonical domain wiring without hidden policy", () => {
  const result = softwareDelivery(
    definition({
      description: "Example",
      lifecycle: [
        { id: "plan.activate", priority: 50 },
        { id: "plan.abandon", priority: 50 },
      ],
    }),
  );

  assert.equal(result.id, "example");
  assert.equal(result.version, "1");
  assert.equal(result.description, "Example");
  assert.equal(result.human_identity, "developer");
  assert.deepEqual(result.facets, softwareDeliveryFacets);
  assert.deepEqual(result.evidence, softwareDeliveryEvidence);
  assert.deepEqual(
    result.operators.map(({ id }) => id),
    ["plan.activate", "plan.abandon"],
  );
  assert.deepEqual(
    result.transitions.map(({ id, priority }) => ({ id, priority })),
    [
      { id: "plan.activate", priority: 50 },
      { id: "plan.abandon", priority: 50 },
    ],
  );
  assert.deepEqual(result.targets, [target]);
  assert.deepEqual(result.entries, [entry]);
  assert.deepEqual(result.work, []);
  assert.equal(result.declarations, undefined);
});

test("registers planning work exactly once before additional work", () => {
  const planning = work("planning-package");
  const additional = [work("implementation"), work("review")];
  const result = softwareDelivery(
    definition({
      lifecycle: [
        workPackageAdmit,
        workPackageApprove,
        planningPackagePromote,
        { id: "plan.activate", priority: 50, work: "implementation" },
        { id: "plan.abandon", priority: 31, work: "review" },
      ],
      planningPackage: planningPackage({ work: planning, planOutput: "implementation-plan" }),
      work: additional,
    }),
  );

  assert.deepEqual(result.work.map(({ id }) => id), [
    "planning-package",
    "implementation",
    "review",
  ]);
  assert.equal(result.transitions[0].work, "planning-package");
  assert.equal(result.transitions[3].work, "implementation");
  assert.equal(result.transitions[4].work, "review");
});

test("closes resolver declarations over exact first use", () => {
  const result = softwareDelivery(
    definition({
      entries: [
        {
          id: "run",
          target: "done",
          inputs: [
            { id: "a", type: "text", required: true, resolver: "resolver.b" },
            { id: "b", type: "text", required: true, resolver: "resolver.a" },
          ],
        },
        {
          id: "resume",
          target: "done",
          inputs: [
            { id: "c", type: "text", required: true, resolver: "resolver.b" },
            { id: "d", type: "text", required: true },
          ],
        },
      ],
    }),
  );

  assert.deepEqual(result.declarations, {
    input_resolvers: ["resolver.b", "resolver.a"],
  });
});

test("planning work alone does not infer an input resolver", () => {
  const result = softwareDelivery(
    definition({
      lifecycle: [workPackageAdmit, workPackageApprove, planningPackagePromote],
      planningPackage: planningPackage({ work: work("planning-package"), planOutput: "implementation-plan" }),
    }),
  );
  assert.equal(result.declarations, undefined);
});

test("binds the explicit plan output through trusted compiled semantics", () => {
  const planning = work("planning-package");
  const result = softwareDelivery(definition({ lifecycle: [workPackageAdmit, workPackageApprove, planningPackagePromote], planningPackage: planningPackage({ work: planning, planOutput: "implementation-plan" }) }));
  assert.equal(result.transitions[0].parameters, undefined);
  assert.deepEqual(result.transitions[2].parameters, [{ parameter: "plan_output", producer: { kind: "trusted-resolver", binding: { reference: "software-delivery/planning-package-plan-output/implementation-plan", version: "1" } } }]);
});

test("generic work package binds no plan semantics", () => {
  const accepted = work("accepted-work");
  accepted.outputs.push(
    { id: "architecture-plan", path: "architecture-plan.md", media_type: "text/markdown", required: true },
    { id: "tasks", path: "tasks.json", media_type: "application/json", required: true },
    { id: "verification", path: "verification.json", media_type: "application/json", required: true },
    { id: "journey", path: "journey.md", media_type: "text/markdown", required: true },
  );
  const result = softwareDelivery(definition({
    lifecycle: [workPackageAdmit, workPackageApprove],
    workPackage: workPackage({ work: accepted }),
  }));
  assert.equal(result.transitions[0].work, accepted.id);
  assert.equal(result.transitions[0].parameters, undefined);
  assert.equal(result.transitions.some(({ id }) => id === "planning.package.promote"), false);
});

test("rejects simultaneous generic and planning package declarations", () => {
  const accepted = work("accepted-work");
  assert.throws(
    () => softwareDelivery(definition({
      lifecycle: [workPackageAdmit, workPackageApprove, planningPackagePromote],
      workPackage: workPackage({ work: accepted }),
      planningPackage: planningPackage({ work: accepted, planOutput: "implementation-plan" }),
    })),
    /SOFTWARE_DELIVERY_PACKAGE_EXCLUSIVE/,
  );
});

for (const [name, planning, error] of [
  ["unknown output", { work: work("planning"), planOutput: "missing" }, /SOFTWARE_DELIVERY_PLAN_OUTPUT_REQUIRED/],
  ["invalid output ID", { work: work("planning"), planOutput: "Implementation Plan" }, /SOFTWARE_DELIVERY_PLAN_OUTPUT_INVALID/],
  ["reserved path", { work: { ...work("planning"), outputs: [{ id: "implementation-plan", path: "Manifest.JSON", media_type: "text/markdown", required: true }] }, planOutput: "implementation-plan" }, /SOFTWARE_DELIVERY_OUTPUT_PATH_RESERVED/],
  ["Windows device path", { work: { ...work("planning"), outputs: [{ id: "implementation-plan", path: "NUL.md", media_type: "text/markdown", required: true }] }, planOutput: "implementation-plan" }, /SOFTWARE_DELIVERY_OUTPUT_PATH_INVALID/],
  ["Windows trailing-dot path", { work: { ...work("planning"), outputs: [{ id: "implementation-plan", path: "plan.", media_type: "text/markdown", required: true }] }, planOutput: "implementation-plan" }, /SOFTWARE_DELIVERY_OUTPUT_PATH_INVALID/],
  ["ancestor collision", { work: { ...work("planning"), outputs: [{ id: "implementation-plan", path: "planning", media_type: "text/markdown", required: true }, { id: "details", path: "planning/details.md", media_type: "text/markdown", required: true }] }, planOutput: "implementation-plan" }, /SOFTWARE_DELIVERY_OUTPUT_PATH_CONFLICT/],
]) {
  test(`rejects ${name}`, () => assert.throws(() => softwareDelivery(definition({ lifecycle: [workPackageAdmit, workPackageApprove, planningPackagePromote], planningPackage: planning })), error));
}

for (const [name, lifecycle, prefix] of [
  ["blank lifecycle ID", [{ id: "  ", priority: 1 }], "SOFTWARE_DELIVERY_LIFECYCLE_EMPTY"],
  [
    "duplicate lifecycle ID",
    [
      { id: "plan.activate", priority: 1 },
      { id: "plan.activate", priority: 2 },
    ],
    "SOFTWARE_DELIVERY_LIFECYCLE_DUPLICATE",
  ],
  ["fractional priority", [{ id: "plan.activate", priority: 1.5 }], "SOFTWARE_DELIVERY_PRIORITY_INVALID"],
  ["NaN priority", [{ id: "plan.activate", priority: Number.NaN }], "SOFTWARE_DELIVERY_PRIORITY_INVALID"],
  ["infinite priority", [{ id: "plan.activate", priority: Number.POSITIVE_INFINITY }], "SOFTWARE_DELIVERY_PRIORITY_INVALID"],
]) {
  test(`rejects ${name}`, () => {
    assert.throws(
      () => softwareDelivery(definition({ lifecycle })),
      (error) => error instanceof Error && error.message.startsWith(prefix),
    );
  });
}

test("rejects duplicate work IDs including repeated planning work", () => {
  const planning = work("planning-package");
  assert.throws(
    () =>
      softwareDelivery(
        definition({
          lifecycle: [workPackageAdmit, workPackageApprove, planningPackagePromote],
          planningPackage: planningPackage({ work: planning, planOutput: "implementation-plan" }),
          work: [planning],
        }),
      ),
    /SOFTWARE_DELIVERY_WORK_DUPLICATE/,
  );
});

for (const humanIdentity of [undefined, "", "Developer", "1developer", "developer role", "x".repeat(129)]) {
  test(`rejects invalid human identity ${String(humanIdentity)}`, () => {
    assert.throws(
      () => softwareDelivery(definition({ humanIdentity })),
      /SOFTWARE_DELIVERY_HUMAN_IDENTITY_INVALID/,
    );
  });
}

test("rejects planning work without its complete package lifecycle", () => {
  assert.throws(
    () =>
      softwareDelivery(
        definition({ planningPackage: planningPackage({ work: work("planning-package"), planOutput: "implementation-plan" }) }),
      ),
    /SOFTWARE_DELIVERY_WORK_PACKAGE_UNUSED/,
  );
});

test("rejects additional work that is not bound to a lifecycle step", () => {
  assert.throws(
    () => softwareDelivery(definition({ work: [work("implementation")] })),
    /SOFTWARE_DELIVERY_WORK_UNUSED/,
  );
});

test("rejects lifecycle references to undeclared work", () => {
  assert.throws(
    () =>
      softwareDelivery(
        definition({
          lifecycle: [
            { id: "plan.activate", priority: 50, work: "implementation" },
          ],
        }),
      ),
    /SOFTWARE_DELIVERY_WORK_UNKNOWN/,
  );
});

test("rejects empty lifecycle work references", () => {
  assert.throws(
    () =>
      softwareDelivery(
        definition({
          lifecycle: [{ id: "plan.activate", priority: 50, work: "  " }],
        }),
      ),
    /SOFTWARE_DELIVERY_WORK_REFERENCE_EMPTY/,
  );
});

test("rejects replacing or repeating the planning work binding", () => {
  const planning = work("planning-package");
  for (const lifecycle of [
    [{ ...workPackageAdmit, work: "implementation" }, workPackageApprove, planningPackagePromote],
    [workPackageAdmit, workPackageApprove, planningPackagePromote, { id: "plan.activate", priority: 50, work: planning.id }],
  ]) {
    assert.throws(
      () =>
        softwareDelivery(
          definition({
            lifecycle,
            planningPackage: planningPackage({ work: planning, planOutput: "implementation-plan" }),
            work: [work("implementation")],
          }),
        ),
      /SOFTWARE_DELIVERY_WORK_CONFLICT/,
    );
  }
});

test("does not mutate inputs or expose mutable canonical arrays", () => {
  const lifecycle = Object.freeze([{ id: "plan.activate", priority: 50 }]);
  const targets = Object.freeze([target]);
  const entries = Object.freeze([entry]);
  const input = Object.freeze({
    id: "example",
    version: "1",
    humanIdentity: "developer",
    lifecycle,
    targets,
    entries,
  });
  const canonicalFacetID = softwareDeliveryFacets[0].id;
  const canonicalEvidenceID = softwareDeliveryEvidence[0].id;

  const result = softwareDelivery(input);
  result.facets[0].id = "changed";
  result.evidence[0].id = "changed";
  result.targets.push({ id: "other", predicate: { true: true } });
  result.entries.push({ id: "other", target: "done" });

  assert.equal(softwareDeliveryFacets[0].id, canonicalFacetID);
  assert.equal(softwareDeliveryEvidence[0].id, canonicalEvidenceID);
  assert.deepEqual(targets, [target]);
  assert.deepEqual(entries, [entry]);
  assert.deepEqual(lifecycle, [{ id: "plan.activate", priority: 50 }]);
});
