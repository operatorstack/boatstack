# Windows CI test sharding

## The problem

The Boatstack Go suite (`boatstack/`) is ~330 tests in one package. Almost every
test builds a real on-disk git repo, spawning several `git` subprocesses — on the
order of 1,000–2,000 process spawns across the suite, run **serially**
(`t.Parallel()` is not used anywhere).

On Linux/macOS this is ~1–2 min. On Windows each `CreateProcess` (plus Microsoft
Defender scanning the freshly written compile/link output) costs ~10× the Linux
`fork+exec`, so the serial suite took **~15 min** — the process-spawn *latency*,
not CPU, is the entire gap. Inline Defender exclusions in `ci.yml` were already
applied and are not enough on their own.

In-process `t.Parallel()` is **not** a safe fix here: the package swaps ~14
mutable package-global function-seams (`runGitCommand`, `operationNow`,
`hookDiagnosticRunner`, `fetchLatestRelease`, …) and uses many `t.Setenv` sites.
Parallel tests within one process would race on that shared global state.

## The fix: job-level sharding

Run the Windows suite as **N separate runner processes**, each executing a
disjoint, balanced subset of tests serially. Globals are per-process, so each
shard keeps today's exact serial semantics; wall-clock drops ~N×. We use N=6,
targeting a slowest-shard time well under 5 min.

`.github/workflows/ci.yml` runs Unix (`test` job) as the full, unsharded
correctness reference and Windows (`test-windows` job) as a
`matrix: { shard: [0..5] }`. Each shard (working-directory `boatstack`):

```bash
regex=$(go test -list '^Test' ./... | python .github/scripts/ci_shard.py --total 6 --index <shard>)
go test -run "$regex" ./...
```

`go test -list` and `go test -run` share the warm GOCACHE within a job, so the
second compile is a cache hit.

## The controller: `ci_shard.py`

Assigning tests to shards to minimize the slowest shard is the classic
multiprocessor-scheduling / makespan-minimization problem (P || Cmax, NP-hard).
`ci_shard.py` uses the standard **LPT (Longest-Processing-Time-first) greedy**
approximation (a 4/3 bound on optimal makespan): sort tests by descending
estimated cost, place each on the currently-lightest shard.

- **Default weight** is 1 per test (count-balanced) — already good because heavy
  tests are spread across many names.
- **Calibration (optional):** pass `--timings <json>` (a `{test-name: seconds}`
  map from a prior `go test -json` run) to weight by measured runtime and close
  the loop against the real cost envelope. No profile is committed yet; the
  controller degrades gracefully to count-balancing without one.
- `--verify` asserts the shards partition the input exactly (no dropped or
  duplicated test). This invariant is unit-tested in `test_ci_shard.py`, which
  the `ci-policy` workflow runs — a broken partition can't merge.

## Canonical location

This repository owns `ci_shard.py` and the sharded runtime workflow. There is no
upstream mirror or external CI authority.

## Operational note

Sharding renames the Windows status check (`test (windows-latest)` →
`test-windows (shard 0..5)`). Update the branch-protection required-status-checks
after merging, or PRs will wait on a check that no longer runs.
