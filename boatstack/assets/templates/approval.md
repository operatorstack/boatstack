# Plan approval: <feature>

This receipt may be created only after the named human explicitly approves the exact fingerprint shown by `boatstack-helper check-plan`.

<!-- boatstack-approval:v1 -->
```json
{
  "schema_version": 2,
  "status": "APPROVED",
  "approved_by": "<human identity>",
  "approved_at": "<ISO-8601 timestamp>",
  "approval_fingerprint": "<PLAN_FINGERPRINT>",
  "baseline_diff_sha256": "<empty only when the product baseline is clean>",
  "baseline_changed_paths": []
}
```
<!-- /boatstack-approval -->
