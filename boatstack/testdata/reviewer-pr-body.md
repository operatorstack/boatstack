## Why this change

Repository hooks could not reliably find the project toolchain when an IDE pushed without an activated environment, while the privacy detector could attempt an unexpected large model download during tests.

## What changed

| Area | Before | After | Reviewer focus |
|---|---|---|---|
| Hook tooling | Depended on the caller's active shell | Resolves the repository's configured environment first | Resolution order and cross-platform fallback |
| Privacy detection | Missing optional models could trigger a runtime download | Fails fast, warns, and uses the installed compact model | Detection remains active without hidden network work |
| Typing | Third-party registry shape was implicit | Registry access has an explicit checked type | No runtime behavior change |

## Review order

1. Review the hook's environment resolution and failure behavior.
2. Review the privacy detector's missing-model boundary and warning.
3. Confirm the typing-only change does not alter runtime registration.

## Evidence

| Claim | Evidence | Result | Source |
|---|---|---|---|
| Hook runs without an activated environment | Repository hook smoke procedure | `NOT_VERIFIED` | Current branch test notes |
| Privacy detection remains active with the compact model | Targeted detector scenario | `NOT_VERIFIED` | Current branch test notes |
| Static typing remains clean | Project type-check command | `NOT_VERIFIED` | Current branch test notes |

## Operational safety

Repository safety scan passed. Destructive recovery remains operator-only.

## Security and privacy

The fallback keeps privacy detection enabled and removes an unexpected network/download side effect from the test path.

## Known gaps and risks

Native Windows environment layout remains unverified. This brief does not claim approval or completed Boatstack gates.

## Rollout and rollback

No data migration is required. Roll back the hook resolution and detector fallback commits independently if either environment path regresses.

<details>
<summary>Boatstack provenance</summary>

- Mode: evidence-limited ad-hoc
- Approval and gate evidence: unavailable
- Coding-host attribution: record here when known

</details>
