#!/usr/bin/env python3
"""Balanced test-shard selector — a makespan-minimization controller for CI.

Why this exists
---------------
The Boatstack Go suite (~325 tests in one package) runs strictly serially, and
almost every test spawns several `git` subprocesses. On Windows each process
spawn is ~10x costlier than on Linux, so the serial suite takes ~15 min there
(vs ~1 min on Linux) — process-spawn *latency*, not CPU, is the bottleneck.
In-process `t.Parallel()` is unsafe here because the package swaps ~14 mutable
global function-seams; parallel tests would race. The safe lever is job-level
sharding: run N shards as N separate processes (globals are per-process), each
running a disjoint subset of tests serially, so wall-clock drops ~Nx.

Control-theory framing
----------------------
Assigning tests to shards to minimize the slowest shard is the classic
multiprocessor-scheduling / makespan-minimization problem (P || Cmax), which is
NP-hard. We use the standard **LPT (Longest-Processing-Time-first) greedy**
approximation: sort tests by descending estimated cost, then place each on the
currently-lightest shard. LPT is a 4/3-approximation of optimal makespan.

Cost is a per-test weight. With no data every test weighs 1 (count-balanced,
which is already good because the heavy tests are spread across many names).
Passing `--timings <json>` (a map of test-name -> measured seconds) closes the
loop with the measured runtime envelope, matching the calibration pattern used
elsewhere in the repo.

Usage
-----
    go test -list '^Test' ./... | \
        python .github/scripts/ci_shard.py --total 6 --index 0
        # -> prints a `go test -run` regex: ^(TestA|TestB|...)$

    ... | python .github/scripts/ci_shard.py --total 6 --verify
        # -> exits non-zero if the 6 shards don't partition the input exactly

The workflow captures the printed regex and runs `go test -run "<regex>" ./...`.
An empty selection prints nothing (exit 0); the caller must treat empty as
"skip this shard", never as `go test -run ''` (which would run everything).
"""
from __future__ import annotations

import argparse
import json
import re
import sys

# `go test -list` prints one test name per line plus a trailing "ok  <pkg>  <t>"
# summary line (and possibly blank lines). Real test names are Go identifiers
# beginning with "Test"; keep only those.
TEST_NAME = re.compile(r"^Test[A-Za-z0-9_]*$")

# The per-package summary line that follows that package's test names.
# Packages without test files print "?   <pkg>   [no test files]" instead.
PACKAGE_SUMMARY = re.compile(r"^ok\s+(\S+)")


def read_test_names(stream) -> list[str]:
    """Parse `go test -list` output from a stream into a sorted, de-duped list."""
    names = set()
    for line in stream:
        name = line.strip()
        if TEST_NAME.match(name):
            names.add(name)
    return sorted(names)


def read_test_packages(stream) -> dict[str, tuple[str, ...]]:
    """Parse `go test -list` output into a test-name -> owning-packages mapping.

    `go test -list ./...` interleaves each package's test names with that
    package's trailing "ok  <pkg>  <t>" summary line, so names are attributed
    to the next summary line seen. A name can legitimately exist in more than
    one package; every owner is kept (sorted, de-duped). Names never followed
    by a package summary are dropped — callers that need completeness must
    verify the mapping covers their enumeration and fail closed on a gap.
    """
    owners: dict[str, set[str]] = {}
    pending: list[str] = []
    for line in stream:
        stripped = line.strip()
        if TEST_NAME.match(stripped):
            pending.append(stripped)
            continue
        summary = PACKAGE_SUMMARY.match(stripped)
        if summary:
            package = summary.group(1)
            for name in pending:
                owners.setdefault(name, set()).add(package)
            pending = []
    return {name: tuple(sorted(packages)) for name, packages in owners.items()}


def assign_shards(names: list[str], total: int, timings: dict[str, float]) -> list[list[str]]:
    """Partition `names` into `total` shards via LPT greedy on estimated cost.

    Returns a list of `total` shard lists. Deterministic: tests are ordered by
    (descending weight, name) before placement, and ties in shard load are
    broken by lowest shard index, so the same input always yields the same
    partition regardless of platform or run.
    """
    if total < 1:
        raise ValueError("--total must be >= 1")

    # Descending weight, then name, for a stable ordering.
    ordered = sorted(names, key=lambda n: (-float(timings.get(n, 1.0)), n))

    shards: list[list[str]] = [[] for _ in range(total)]
    loads = [0.0] * total
    for name in ordered:
        # Lightest shard wins; ties -> lowest index (min is stable on first).
        target = min(range(total), key=lambda i: (loads[i], i))
        shards[target].append(name)
        loads[target] += float(timings.get(name, 1.0))

    # Emit each shard sorted by name for readable, stable regexes.
    return [sorted(shard) for shard in shards]


def shard_regex(shard: list[str]) -> str:
    """Build an anchored `go test -run` alternation for one shard.

    Go test names are identifiers, but escape defensively so a stray character
    can never turn into an unintended regex. Empty shard -> empty string.
    """
    if not shard:
        return ""
    alternation = "|".join(re.escape(name) for name in shard)
    return f"^({alternation})$"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--total", type=int, required=True, help="Number of shards.")
    parser.add_argument("--index", type=int, help="Shard index to emit (0-based).")
    parser.add_argument(
        "--timings",
        help="Optional JSON file mapping test name -> measured seconds (LPT weights).",
    )
    parser.add_argument(
        "--verify",
        action="store_true",
        help="Assert the shards partition the input exactly; print a summary; no regex.",
    )
    args = parser.parse_args(argv)

    if args.total < 1:
        print("error: --total must be >= 1", file=sys.stderr)
        return 2
    if not args.verify and args.index is None:
        print("error: --index is required unless --verify is set", file=sys.stderr)
        return 2
    if args.index is not None and not (0 <= args.index < args.total):
        print(f"error: --index must be in [0, {args.total})", file=sys.stderr)
        return 2

    timings: dict[str, float] = {}
    if args.timings:
        with open(args.timings, "r", encoding="utf-8") as fh:
            timings = {str(k): float(v) for k, v in json.load(fh).items()}

    names = read_test_names(sys.stdin)
    shards = assign_shards(names, args.total, timings)

    if args.verify:
        assigned = [n for shard in shards for n in shard]
        if sorted(assigned) != names or len(assigned) != len(names):
            print(
                "error: shards do not partition the input exactly "
                f"(input={len(names)}, assigned={len(assigned)})",
                file=sys.stderr,
            )
            return 1
        sizes = ", ".join(f"#{i}={len(s)}" for i, s in enumerate(shards))
        print(f"partition OK: {len(names)} tests across {args.total} shards ({sizes})")
        return 0

    print(shard_regex(shards[args.index]))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
