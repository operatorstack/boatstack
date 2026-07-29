# Plan approval: <feature>

This receipt may be created only after the named human explicitly approves the exact fingerprint shown by `boatstack-helper check-plan`.

<!-- boatstack-approval:v1 -->
```json
{
  "schema_version": 3,
  "status": "APPROVED",
  "approved_by": "<human identity>",
  "approved_at": "<ISO-8601 timestamp>",
  "approval_fingerprint": "<PLAN_FINGERPRINT>",
  "baseline_diff_sha256": "<empty only when the product baseline is clean>",
  "baseline_changed_paths": [],
  "readiness_fingerprint": "<READINESS_FINGERPRINT>",
  "base_branch": "<base branch>",
  "head_branch": "<feature branch>",
  "base_commit": "<base commit>",
  "head_commit": "<head commit>",
  "upstream": "<upstream or empty>",
  "upstream_relation": "<CURRENT, AHEAD, or UNPUBLISHED>",
  "journey_manifest_sha256": "<sha256>"
}
```
<!-- /boatstack-approval -->
