#!/usr/bin/env python3
"""Run the complete Boatstack Go suite in isolated, balanced local shards."""

from __future__ import annotations

import argparse
import io
import json
import os
import re
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

# Top-level test verdicts in `go test -v` output; subtests are indented and
# deliberately excluded — shards are balanced on top-level names only.
TOP_LEVEL_RESULT = re.compile(r"^--- (?:PASS|FAIL): (Test[A-Za-z0-9_]*) \((\d+(?:\.\d+)?)s\)")

# `go test -v` narration that carries no failure signal.
VERBOSE_NOISE = re.compile(r"^(=== (?:RUN|PAUSE|CONT|NAME)\s|--- PASS: |PASS$)")


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


def default_timings_path() -> Path:
    return Path.home() / ".cache" / "boatstack" / "test-timings.json"


def list_top_level_tests(
    runtime: Path = RUNTIME,
    *,
    run: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
) -> tuple[list[str], dict[str, tuple[str, ...]]]:
    """Enumerate top-level tests and the packages that own each of them."""
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
    packages_by_test = ci_shard.read_test_packages(io.StringIO(completed.stdout))
    return names, packages_by_test


def verified_partition(
    names: list[str], jobs: int, timings: dict[str, float] | None = None
) -> list[list[str]]:
    if jobs < 1:
        raise RunnerError("--jobs must be at least 1")
    if not names:
        raise RunnerError("cannot partition an empty test set")
    shards = ci_shard.assign_shards(names, min(jobs, len(names)), timings or {})
    assigned = [name for shard in shards for name in shard]
    if sorted(assigned) != sorted(names) or len(assigned) != len(set(assigned)):
        raise RunnerError("shards do not partition the enumerated tests exactly")
    return shards


def verified_package_scope(
    shards: list[list[str]], packages_by_test: dict[str, tuple[str, ...]]
) -> list[list[str]]:
    """Compute each shard's owning-package list, refusing on any coverage gap.

    Every enumerated test must map to at least one package, and every owning
    package of every test must be in that shard's scope — otherwise a scoped
    `go test` invocation could silently skip an enumerated test.
    """
    scopes: list[list[str]] = []
    for index, shard in enumerate(shards):
        packages: set[str] = set()
        for name in shard:
            owners = packages_by_test.get(name, ())
            if not owners:
                raise RunnerError(
                    f"test {name!r} has no owning package in the enumeration; "
                    "refusing to run a scoped shard that could skip it"
                )
            packages.update(owners)
        scope = sorted(packages)
        missing = [
            name
            for name in shard
            if not set(packages_by_test[name]).issubset(packages)
        ]
        if missing:
            raise RunnerError(
                f"shard {index} scope does not cover tests: {', '.join(missing)}"
            )
        scopes.append(scope)
    return scopes


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
    package_scopes: list[list[str]] | None = None,
) -> list[ShardResult]:
    workers: list[tuple[int, list[str], subprocess.Popen, object, float]] = []
    finished_at: dict[int, float] = {}
    try:
        for index, shard in enumerate(shards):
            regex = ci_shard.shard_regex(shard)
            if not regex:
                raise RunnerError(f"refusing to run empty shard {index}")
            scope = package_scopes[index] if package_scopes else ["./..."]
            if not scope:
                raise RunnerError(f"refusing to run shard {index} with an empty package scope")
            output = tempfile.TemporaryFile(mode="w+t", encoding="utf-8")
            started = time.monotonic()
            try:
                process = popen(
                    ["go", "test", "-count=1", "-v", "-run", regex, *scope],
                    cwd=runtime,
                    stdout=output,
                    stderr=subprocess.STDOUT,
                    text=True,
                )
            except Exception:
                output.close()
                raise
            workers.append((index, shard, process, output, started))

        while True:
            now = time.monotonic()
            for index, _, process, _, _ in workers:
                if index not in finished_at and process.poll() is not None:
                    finished_at[index] = now
            if len(finished_at) == len(workers):
                break
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
                elapsed_seconds=finished_at.get(index, time.monotonic()) - started,
                output=value,
            )
        )
    return results


def parse_test_durations(output: str) -> dict[str, float]:
    """Extract top-level per-test seconds from one shard's `go test -v` output."""
    durations: dict[str, float] = {}
    for line in output.splitlines():
        match = TOP_LEVEL_RESULT.match(line)
        if match:
            durations[match.group(1)] = float(match.group(2))
    return durations


def load_timings(path: Path, names: list[str]) -> dict[str, float]:
    """Read persisted per-test seconds, keeping only currently enumerated tests.

    Missing, malformed, or foreign entries degrade to nothing: LPT then weighs
    those tests at 1.0, exactly the pre-timings behavior.
    """
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return {}
    if not isinstance(raw, dict):
        return {}
    current = set(names)
    timings: dict[str, float] = {}
    for key, value in raw.items():
        if key in current and isinstance(value, (int, float)) and value > 0:
            timings[str(key)] = float(value)
    return timings


def save_timings(path: Path, timings: dict[str, float]) -> None:
    """Persist per-test seconds atomically; failure to persist never fails the run."""
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile(
            mode="w", encoding="utf-8", dir=path.parent, delete=False
        ) as handle:
            json.dump(dict(sorted(timings.items())), handle, indent=1)
            handle.write("\n")
            staging = Path(handle.name)
        os.replace(staging, path)
    except OSError as error:
        print(f"warning: could not persist test timings: {error}", file=sys.stderr)


def failure_display(output: str) -> str:
    """Strip `go test -v` pass narration so a failed shard shows its signal."""
    kept = [line for line in output.splitlines() if not VERBOSE_NOISE.match(line)]
    return "\n".join(kept)


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
    parser.add_argument(
        "--timings-file",
        type=Path,
        default=default_timings_path(),
        help="per-test seconds persisted between runs to balance shards (never in the repo)",
    )
    args = parser.parse_args(argv)

    started = time.monotonic()
    try:
        names, packages_by_test = list_top_level_tests()
        timings = load_timings(args.timings_file, names)
        shards = verified_partition(names, args.jobs, timings)
        package_scopes = verified_package_scope(shards, packages_by_test)
        balanced = f"{len(timings)} of {len(names)} tests have measured weights"
        print(
            f"Running {len(names)} tests across {len(shards)} isolated shards ({balanced}).",
            flush=True,
        )
        results = run_shards(shards, package_scopes=package_scopes)
    except KeyboardInterrupt:
        print("Interrupted; all local test shards were stopped.", file=sys.stderr)
        return 130
    except (OSError, RunnerError) as error:
        print(f"ERROR: {error}", file=sys.stderr)
        return 2

    measured: dict[str, float] = dict(timings)
    for result in results:
        measured.update(parse_test_durations(result.output))
    save_timings(args.timings_file, {k: v for k, v in measured.items() if k in set(names)})

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
                print(failure_display(result.output).rstrip(), file=sys.stderr)

    elapsed = time.monotonic() - started
    if failed:
        print(f"FAIL: one or more shards failed after {elapsed:.1f}s.", file=sys.stderr)
        return 1
    print(f"PASS: all {len(names)} tests passed in {elapsed:.1f}s.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
