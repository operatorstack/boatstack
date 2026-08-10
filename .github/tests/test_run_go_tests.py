from __future__ import annotations

import io
import sys
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
    def test_every_partition_reaches_one_isolated_worker(self):
        commands = []

        def launch(command, **kwargs):
            commands.append(command)
            return ImmediateProcess(command, **kwargs)

        names = ["TestAlpha", "TestBeta", "TestGamma", "TestDelta"]
        shards = run_go_tests.verified_partition(names, 3)
        results = run_go_tests.run_shards(shards, popen=launch)
        self.assertEqual(len(results), len(shards))
        self.assertTrue(all(result.returncode == 0 for result in results))
        selected = []
        for command in commands:
            self.assertEqual(command[:4], ["go", "test", "-count=1", "-run"])
            regex = command[4]
            for name in names:
                if name in regex:
                    selected.append(name)
        self.assertEqual(sorted(selected), sorted(names))

    # control-law: complete-local-test-partition.
    def test_interrupt_stops_active_workers(self):
        first, second = ActiveProcess(), ActiveProcess()
        run_go_tests.stop_processes([first, second])
        self.assertTrue(first.terminated)
        self.assertTrue(second.terminated)

    # control-law: complete-local-test-partition.
    def test_any_worker_failure_fails_the_aggregate_gate(self):
        names = ["TestAlpha", "TestBeta"]
        results = [
            run_go_tests.ShardResult(0, 1, 0, 0.1, "ok"),
            run_go_tests.ShardResult(1, 1, 1, 0.1, "failed assertion"),
        ]
        with (
            mock.patch.object(run_go_tests, "list_top_level_tests", return_value=names),
            mock.patch.object(run_go_tests, "run_shards", return_value=results),
            mock.patch("sys.stdout", new_callable=io.StringIO),
            mock.patch("sys.stderr", new_callable=io.StringIO) as stderr,
        ):
            self.assertEqual(run_go_tests.main(["--jobs", "2"]), 1)
        self.assertIn("failed assertion", stderr.getvalue())


class ExistingShardControllerContract(unittest.TestCase):
    def test_runner_reuses_the_reviewed_controller(self):
        names = [f"Test{index:03d}" for index in range(12)]
        self.assertEqual(
            run_go_tests.verified_partition(names, 4),
            ci_shard.assign_shards(names, 4, {}),
        )


if __name__ == "__main__":
    unittest.main()
