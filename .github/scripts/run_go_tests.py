#!/usr/bin/env python3
"""Run the complete Boatstack Go suite in isolated, balanced local shards."""

from __future__ import annotations

import argparse
import io
import os
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Sequence

import ci_shard


REPO = Path(__file__).resolve().parents[2]
RUNTIME = REPO / "boatstack"
MAX_DEFAULT_JOBS = 10


class RunnerError(RuntimeError):
    """A deterministic local-test precondition or worker failed."""


@dataclass(frozen=True)
class ShardResult:
    index: int
    test_count: int
    returncode: int
    elapsed_seconds: float
    output: str


def default_jobs() -> int:
    cores = max(1, os.cpu_count() or 1)
    # Keep headroom for the Git subprocesses created by almost every test.
    cpu_aware_jobs = max(1, (cores * 5 + 3) // 7)
    return min(MAX_DEFAULT_JOBS, cpu_aware_jobs)


def list_top_level_tests(
    runtime: Path = RUNTIME,
    *,
    run: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
) -> list[str]:
    completed = run(
        ["go", "test", "-list", "^Test", "./..."],
        cwd=runtime,
        text=True,
        capture_output=True,
    )
    if completed.returncode != 0:
        detail = (completed.stdout + completed.stderr).strip()
        raise RunnerError(f"test enumeration failed\n{detail}")
    names = ci_shard.read_test_names(io.StringIO(completed.stdout))
    if not names:
        raise RunnerError("test enumeration returned no top-level tests")
    return names


def verified_partition(names: list[str], jobs: int) -> list[list[str]]:
    if jobs < 1:
        raise RunnerError("--jobs must be at least 1")
    if not names:
        raise RunnerError("cannot partition an empty test set")
    shards = ci_shard.assign_shards(names, min(jobs, len(names)), {})
    assigned = [name for shard in shards for name in shard]
    if sorted(assigned) != sorted(names) or len(assigned) != len(set(assigned)):
        raise RunnerError("shards do not partition the enumerated tests exactly")
    return shards


def stop_processes(processes: Sequence[subprocess.Popen], timeout: float = 2.0) -> None:
    active = [process for process in processes if process.poll() is None]
    for process in active:
        process.terminate()
    deadline = time.monotonic() + timeout
    while active and time.monotonic() < deadline:
        active = [process for process in active if process.poll() is None]
        if active:
            time.sleep(0.02)
    for process in active:
        process.kill()
    for process in processes:
        try:
            process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()


def run_shards(
    shards: list[list[str]],
    runtime: Path = RUNTIME,
    *,
    popen: Callable[..., subprocess.Popen] = subprocess.Popen,
) -> list[ShardResult]:
    workers: list[tuple[int, list[str], subprocess.Popen, object, float]] = []
    try:
        for index, shard in enumerate(shards):
            regex = ci_shard.shard_regex(shard)
            if not regex:
                raise RunnerError(f"refusing to run empty shard {index}")
            output = tempfile.TemporaryFile(mode="w+t", encoding="utf-8")
            started = time.monotonic()
            try:
                process = popen(
                    ["go", "test", "-count=1", "-run", regex, "./..."],
                    cwd=runtime,
                    stdout=output,
                    stderr=subprocess.STDOUT,
                    text=True,
                )
            except Exception:
                output.close()
                raise
            workers.append((index, shard, process, output, started))

        while any(process.poll() is None for _, _, process, _, _ in workers):
            time.sleep(0.05)
    except BaseException:
        stop_processes([process for _, _, process, _, _ in workers])
        for _, _, _, output, _ in workers:
            output.close()
        raise

    results: list[ShardResult] = []
    for index, shard, process, output, started in workers:
        process.wait()
        output.seek(0)
        value = output.read()
        output.close()
        results.append(
            ShardResult(
                index=index,
                test_count=len(shard),
                returncode=process.returncode,
                elapsed_seconds=time.monotonic() - started,
                output=value,
            )
        )
    return results


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed < 1:
        raise argparse.ArgumentTypeError("must be at least 1")
    return parsed


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--jobs",
        type=positive_int,
        default=default_jobs(),
        help=f"isolated test processes (default: about seven of ten CPUs, up to {MAX_DEFAULT_JOBS})",
    )
    args = parser.parse_args(argv)

    started = time.monotonic()
    try:
        names = list_top_level_tests()
        shards = verified_partition(names, args.jobs)
        print(
            f"Running {len(names)} tests across {len(shards)} isolated shards.",
            flush=True,
        )
        results = run_shards(shards)
    except KeyboardInterrupt:
        print("Interrupted; all local test shards were stopped.", file=sys.stderr)
        return 130
    except (OSError, RunnerError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 2

    failed = False
    for result in sorted(results, key=lambda item: item.index):
        label = "PASS" if result.returncode == 0 else "FAIL"
        print(
            f"{label} shard {result.index + 1}/{len(results)} "
            f"({result.test_count} tests, {result.elapsed_seconds:.1f}s)"
        )
        if result.returncode != 0:
            failed = True
            if result.output:
                print(result.output.rstrip(), file=sys.stderr)

    elapsed = time.monotonic() - started
    if failed:
        print(f"FAIL: one or more shards failed after {elapsed:.1f}s.", file=sys.stderr)
        return 1
    print(f"PASS: all {len(names)} tests passed in {elapsed:.1f}s.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
