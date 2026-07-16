# Structured plan: diagram-json-v1

This is the canonical human-readable and machine-checkable plan used by the worked example.

<!-- boatstack-plan:v1 -->
```json
{
  "schema_version": 1,
  "feature_id": "diagram-json-v1",
  "source_plan_path": "source-plan.md",
  "spec_path": "spec.md",
  "blocking_questions": [],
  "acceptance_criteria": [
    {
      "id": "AC-1",
      "text": "The public serializer returns parseable schema-versioned graph JSON."
    },
    {
      "id": "AC-2",
      "text": "Serialization is deterministic and preserves ordered graph data."
    },
    {
      "id": "AC-3",
      "text": "Run serialization exposes only the compact signal and bottleneck overlay."
    },
    {
      "id": "AC-4",
      "text": "Existing ASCII output remains byte-compatible."
    },
    {
      "id": "AC-5",
      "text": "The API and schema types are publicly exported and documented."
    }
  ],
  "tasks": [
    {
      "id": "T-1",
      "title": "Define the v1 schema and pure serializer at the diagram boundary",
      "depends_on": [],
      "acceptance_criteria": ["AC-1", "AC-2", "AC-3"],
      "validation": [
        {
          "criteria": ["AC-1", "AC-2", "AC-3"],
          "run": "pnpm typecheck",
          "origin": "Public schema types required by AC-1, AC-2, and AC-3",
          "oracle": "The repository's existing TypeScript compiler configuration",
          "independence": "pre-existing"
        },
        {
          "criteria": ["AC-1", "AC-2", "AC-3"],
          "run": "pnpm exec tsx examples/05-diagram-printer/json-check.ts",
          "origin": "The approved JSON contract in AC-1, AC-2, and AC-3",
          "oracle": "Parser, schema-version, ordering, and compact-overlay assertions derived from the approved contract",
          "independence": "contract-derived"
        }
      ],
      "rollback_boundary": "Revert the serializer and schema types in src/diagram.ts without touching the ASCII renderer."
    },
    {
      "id": "T-2",
      "title": "Expose and document the additive public contract",
      "depends_on": ["T-1"],
      "acceptance_criteria": ["AC-5"],
      "validation": [
        {
          "criteria": ["AC-5"],
          "run": "pnpm typecheck",
          "origin": "The public type-export requirement in AC-5",
          "oracle": "The repository's existing TypeScript compiler configuration",
          "independence": "pre-existing"
        },
        {
          "criteria": ["AC-5"],
          "run": "pnpm build",
          "origin": "The package export and documentation contract in AC-5",
          "oracle": "The repository's existing production build",
          "independence": "pre-existing"
        }
      ],
      "rollback_boundary": "Remove the new src/index.ts exports and JSON documentation together."
    },
    {
      "id": "T-3",
      "title": "Add contract fixtures and prove text-renderer compatibility",
      "depends_on": ["T-1", "T-2"],
      "acceptance_criteria": ["AC-1", "AC-2", "AC-3", "AC-4", "AC-5"],
      "validation": [
        {
          "criteria": ["AC-1", "AC-2", "AC-3"],
          "run": "pnpm exec tsx examples/05-diagram-printer/json-check.ts",
          "origin": "The approved JSON behaviors in AC-1, AC-2, and AC-3",
          "oracle": "Contract-derived parser and fixture assertions",
          "independence": "contract-derived"
        },
        {
          "criteria": ["AC-4"],
          "run": "pnpm example:diagram",
          "origin": "The existing diagram behavior protected by AC-4",
          "oracle": "The repository's pre-feature executable example",
          "independence": "pre-existing"
        },
        {
          "criteria": ["AC-4"],
          "run": "diff -u examples/05-diagram-printer/expected-output.txt <(pnpm --silent example:diagram)",
          "origin": "The byte-compatibility decision in AC-4",
          "oracle": "The pre-feature expected ASCII fixture",
          "independence": "pre-existing"
        },
        {
          "criteria": ["AC-1", "AC-2", "AC-3", "AC-5"],
          "run": "pnpm typecheck",
          "origin": "The public type contracts in AC-1 through AC-5",
          "oracle": "The repository's existing TypeScript compiler configuration",
          "independence": "pre-existing"
        },
        {
          "criteria": ["AC-5"],
          "run": "pnpm build",
          "origin": "The distributable package contract in AC-5",
          "oracle": "The repository's existing production build",
          "independence": "pre-existing"
        }
      ],
      "rollback_boundary": "Revert the JSON fixture/check and documentation; retain the pre-feature expected ASCII fixture."
    }
  ]
}
```
<!-- /boatstack-plan -->
