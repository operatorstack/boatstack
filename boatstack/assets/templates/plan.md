# Structured plan: <feature>

- This Markdown file is the canonical plan.
- Prose and structured data are both covered by the approval fingerprint.
- Approval state is recorded separately in `approval.md`; never edit this file merely to mark it approved.

## Human-readable summary

<Describe the accepted outcome, boundaries, task order, and verification approach.>

## Structured plan

<!-- boatstack-plan:v1 -->
```json
{
  "schema_version": 1,
  "feature_id": "<stable-feature-id>",
  "source_plan_path": "source-plan.md",
  "spec_path": "feature-spec.md",
  "blocking_questions": [],
  "acceptance_criteria": [
    {
      "id": "AC-1",
      "text": "<observable accepted behavior>"
    }
  ],
  "tasks": [
    {
      "id": "T-1",
      "title": "<bounded implementation operation>",
      "depends_on": [],
      "acceptance_criteria": ["AC-1"],
      "affected_paths": ["<repository path or glob>"],
      "side_effects": [],
      "validation": [
        {
          "criteria": ["AC-1"],
          "run": "<real command or verification procedure>",
          "origin": "<criterion, repository invariant, human decision, or external contract>",
          "oracle": "<independent fact, fixture, threshold, rubric, or named human judgment>",
          "independence": "<pre-existing, contract-derived, external, human, or implementation-authored>"
        }
      ],
      "rollback_boundary": "<how to revert this task>"
    }
  ]
}
```
<!-- /boatstack-plan -->

For an external write, replace the empty `side_effects` list with entries such as:

```json
{
  "kind": "database-write",
  "target": "<immutable project/database identifier>",
  "reversibility": "transactional",
  "failure_policy": "rollback-transaction",
  "destructive": false
}
```

Boatstack rejects ambiguous targets, automated resets, and destructive rollback. Use
`stop-and-fix-forward` when a transaction cannot contain the full operation.
