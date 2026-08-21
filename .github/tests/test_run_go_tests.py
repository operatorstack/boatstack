from __future__ import annotations

import io
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPTS = Path(__file__).resolve().parents[1] / "scripts"
sys.path.insert(0, str(SCRIPTS))

import ci_shard  # noqa: E402
import run_go_tests  # noqa: E402


class Completed:
    def __init__(self, returncode: int, stdout: str = "", stderr: str = ""):
        self.returncode = returncode
        self.stdout = stdout
        self.stderr = stderr


class ImmediateProcess:
    def __init__(self, command, *, stdout, returncode=0, **_kwargs):
        self.command = command
        self.returncode = returncode
        stdout.write("worker output\n")
        stdout.flush()

    def poll(self):
        return self.returncode

    def wait(self, timeout=None):
        return self.returncode

    def terminate(self):
        self.returncode = -15

    def kill(self):
        self.returncode = -9


class ActiveProcess(ImmediateProcess):
    def __init__(self):
        self.returncode = None
        self.terminated = False

    def terminate(self):
        self.terminated = True
        self.returncode = -15


class LocalGoTestBoundary(unittest.TestCase):
    def test_help_is_renderable(self):
        with self.assertRaises(SystemExit) as stopped:
            run_go_tests.main(["--help"])
        self.assertEqual(stopped.exception.code, 0)

    def test_default_uses_ten_of_this_macs_fourteen_cores(self):
        with mock.patch.object(run_go_tests.os, "cpu_count", return_value=14):
            self.assertEqual(run_go_tests.default_jobs(), 10)

    # control-law: complete-local-test-partition.
    def test_partition_is_exact_and_has_no_overlap(self):
        names = [f"Test{index:03d}" for index in range(31)]
        shards = run_go_tests.verified_partition(names, 6)
        assigned = [name for shard in shards for name in shard]
        self.assertEqual(sorted(assigned), names)
        self.assertEqual(len(assigned), len(set(assigned)))

    # control-law: complete-local-test-partition.
    def test_empty_enumeration_fails_closed(self):
        with self.assertRaisesRegex(run_go_tests.RunnerError, "empty test set"):
            run_go_tests.verified_partition([], 6)

    # control-law: complete-local-test-partition.
    def test_enumeration_failure_cannot_start_workers(self):
        def failed_run(*_args, **_kwargs):
            return Completed(1, stderr="compile failed")

        with self.assertRaisesRegex(run_go_tests.RunnerError, "compile failed"):
            run_go_tests.list_top_level_tests(run=failed_run)

    # control-law: complete-local-test-partition.
    def test_enumeration_maps_every_test_to_its_owning_package(self):
        listing = (
            "TestAlpha\n"
            "TestBeta\n"
            "ok  \texample.com/mod/first\t0.01s\n"
            "?   \texample.com/mod/none\t[no test files]\n"
            "TestGamma\n"
            "ok  \texample.com/mod/second\t0.02s\n"
        )

        def listed_run(*_args, **_kwargs):
            return Completed(0, stdout=listing)

        names, packages = run_go_tests.list_top_level_tests(run=listed_run)
        self.assertEqual(names, ["TestAlpha", "TestBeta", "TestGamma"])
        self.assertEqual(
            packages,
            {
                "TestAlpha": ("example.com/mod/first",),
                "TestBeta": ("example.com/mod/first",),
                "TestGamma": ("example.com/mod/second",),
            },
        )

    # control-law: complete-local-test-partition.
    def test_scoped_shards_refuse_tests_without_an_owning_package(self):
        shards = [["TestAlpha", "TestOrphan"]]
        packages = {"TestAlpha": ("example.com/mod/first",)}
        with self.assertRaisesRegex(run_go_tests.RunnerError, "TestOrphan"):
            run_go_tests.verified_package_scope(shards, packages)

    # control-law: complete-local-test-partition.
    def test_shard_scope_is_the_union_of_owning_packages(self):
        shards = [["TestAlpha", "TestGamma"], ["TestBeta"]]
        packages = {
            "TestAlpha": ("example.com/mod/first",),
            "TestBeta": ("example.com/mod/first", "example.com/mod/second"),
            "TestGamma": ("example.com/mod/second",),
        }
        scopes = run_go_tests.verified_package_scope(shards, packages)
        self.assertEqual(
            scopes,
            [
                ["example.com/mod/first", "example.com/mod/second"],
                ["example.com/mod/first", "example.com/mod/second"],
            ],
        )

    # control-law: complete-local-test-partition.
    def test_every_partition_reaches_one_isolated_worker(self):
        commands = []

        def launch(command, **kwargs):
            commands.append(command)
            return ImmediateProcess(command, **kwargs)

        names = ["TestAlpha", "TestBeta", "TestGamma", "TestDelta"]
        packages = {name: ("example.com/mod/only",) for name in names}
        shards = run_go_tests.verified_partition(names, 3)
        scopes = run_go_tests.verified_package_scope(shards, packages)
        results = run_go_tests.run_shards(shards, popen=launch, package_scopes=scopes)
        self.assertEqual(len(results), len(shards))
        self.assertTrue(all(result.returncode == 0 for result in results))
        selected = []
        for command in commands:
            self.assertEqual(command[:5], ["go", "test", "-count=1", "-v", "-run"])
            regex = command[5]
            self.assertEqual(command[6:], ["example.com/mod/only"])
            for name in names:
                if name in regex:
                    selected.append(name)
        self.assertEqual(sorted(selected), sorted(names))

    # control-law: complete-local-test-partition.
    def test_unscoped_workers_still_sweep_every_package(self):
        commands = []

        def launch(command, **kwargs):
            commands.append(command)
            return ImmediateProcess(command, **kwargs)

        shards = run_go_tests.verified_partition(["TestAlpha", "TestBeta"], 1)
        run_go_tests.run_shards(shards, popen=launch)
        self.assertEqual(commands[0][6:], ["./..."])

    # control-law: complete-local-test-partition.
    def test_interrupt_stops_active_workers(self):
        first, second = ActiveProcess(), ActiveProcess()
        run_go_tests.stop_processes([first, second])
        self.assertTrue(first.terminated)
        self.assertTrue(second.terminated)

    # control-law: complete-local-test-partition.
    def test_any_worker_failure_fails_the_aggregate_gate(self):
        names = ["TestAlpha", "TestBeta"]
        packages = {name: ("example.com/mod/only",) for name in names}
        results = [
            run_go_tests.ShardResult(0, 1, 0, 0.1, "ok"),
            run_go_tests.ShardResult(1, 1, 1, 0.1, "failed assertion"),
        ]
        with (
            tempfile.TemporaryDirectory() as scratch,
            mock.patch.object(
                run_go_tests, "list_top_level_tests", return_value=(names, packages)
            ),
            mock.patch.object(run_go_tests, "run_shards", return_value=results),
            mock.patch("sys.stdout", new_callable=io.StringIO),
            mock.patch("sys.stderr", new_callable=io.StringIO) as stderr,
        ):
            timings_file = str(Path(scratch) / "timings.json")
            self.assertEqual(
                run_go_tests.main(["--jobs", "2", "--timings-file", timings_file]), 1
            )
        self.assertIn("failed assertion", stderr.getvalue())


class MeasuredShardBalance(unittest.TestCase):
    def test_per_test_durations_parse_top_level_results_only(self):
        output = (
            "=== RUN   TestAlpha\n"
            "--- PASS: TestAlpha (2.50s)\n"
            "=== RUN   TestBeta\n"
            "    --- PASS: TestBeta/subtest (1.00s)\n"
            "--- FAIL: TestBeta (4.25s)\n"
            "ok  \texample.com/mod/first\t6.75s\n"
        )
        self.assertEqual(
            run_go_tests.parse_test_durations(output),
            {"TestAlpha": 2.5, "TestBeta": 4.25},
        )

    def test_timings_roundtrip_and_foreign_entries_are_dropped(self):
        with tempfile.TemporaryDirectory() as scratch:
            path = Path(scratch) / "cache" / "timings.json"
            run_go_tests.save_timings(
                path, {"TestAlpha": 2.5, "TestRemoved": 9.0, "TestBad": -1.0}
            )
            loaded = run_go_tests.load_timings(path, ["TestAlpha", "TestBeta"])
        self.assertEqual(loaded, {"TestAlpha": 2.5})

    def test_missing_or_malformed_timings_degrade_to_count_balance(self):
        with tempfile.TemporaryDirectory() as scratch:
            missing = Path(scratch) / "absent.json"
            self.assertEqual(run_go_tests.load_timings(missing, ["TestAlpha"]), {})
            malformed = Path(scratch) / "broken.json"
            malformed.write_text("not json", encoding="utf-8")
            self.assertEqual(run_go_tests.load_timings(malformed, ["TestAlpha"]), {})

    def test_measured_weights_change_the_partition(self):
        names = ["TestHeavy", "TestA", "TestB", "TestC"]
        counted = run_go_tests.verified_partition(names, 2)
        weighted = run_go_tests.verified_partition(
            names, 2, {"TestHeavy": 100.0, "TestA": 1.0, "TestB": 1.0, "TestC": 1.0}
        )
        self.assertEqual(weighted, ci_shard.assign_shards(names, 2, {"TestHeavy": 100.0}))
        self.assertIn(["TestHeavy"], weighted)
        self.assertNotEqual(counted, weighted)

    def test_failure_display_strips_pass_narration(self):
        output = (
            "=== RUN   TestAlpha\n"
            "--- PASS: TestAlpha (0.10s)\n"
            "=== RUN   TestBeta\n"
            "    boundary_test.go:12: broken invariant\n"
            "--- FAIL: TestBeta (0.20s)\n"
            "FAIL\n"
        )
        display = run_go_tests.failure_display(output)
        self.assertIn("broken invariant", display)
        self.assertIn("--- FAIL: TestBeta", display)
        self.assertNotIn("--- PASS", display)
        self.assertNotIn("=== RUN", display)


class ExistingShardControllerContract(unittest.TestCase):
    def test_runner_reuses_the_reviewed_controller(self):
        names = [f"Test{index:03d}" for index in range(12)]
        self.assertEqual(
            run_go_tests.verified_partition(names, 4),
            ci_shard.assign_shards(names, 4, {}),
        )


if __name__ == "__main__":
    unittest.main()
