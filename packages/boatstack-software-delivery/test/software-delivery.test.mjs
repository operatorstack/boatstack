import assert from "node:assert/strict";
import test from "node:test";

import {
  planningPackageAdmit,
  softwareDelivery,
  softwareDeliveryEvidence,
  softwareDeliveryFacets,
} from "../dist/index.js";

const target = { id: "done", predicate: { true: true } };
const entry = { id: "run", target: "done" };
const work = (id) => ({
  id,
  instructions: { path: `${id}.md` },
  inputs: [],
  outputs: [],
});

function definition(overrides = {}) {
  return {
    id: "example",
    version: "1",
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
      lifecycle: [planningPackageAdmit, { id: "plan.activate", priority: 50 }],
      planningPackageWork: planning,
      work: additional,
    }),
  );

  assert.deepEqual(result.work.map(({ id }) => id), [
    "planning-package",
    "implementation",
    "review",
  ]);
  assert.equal(result.transitions[0].work, "planning-package");
  assert.equal(result.transitions[1].work, undefined);
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
      lifecycle: [planningPackageAdmit],
      planningPackageWork: work("planning-package"),
    }),
  );
  assert.equal(result.declarations, undefined);
});

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
          lifecycle: [planningPackageAdmit],
          planningPackageWork: planning,
          work: [planning],
        }),
      ),
    /SOFTWARE_DELIVERY_WORK_DUPLICATE/,
  );
});

test("rejects planning work without its admit step", () => {
  assert.throws(
    () =>
      softwareDelivery(
        definition({ planningPackageWork: work("planning-package") }),
      ),
    /SOFTWARE_DELIVERY_PLANNING_WORK_UNUSED/,
  );
});

test("does not mutate inputs or expose mutable canonical arrays", () => {
  const lifecycle = Object.freeze([{ id: "plan.activate", priority: 50 }]);
  const targets = Object.freeze([target]);
  const entries = Object.freeze([entry]);
  const input = Object.freeze({
    id: "example",
    version: "1",
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
