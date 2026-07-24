#!/usr/bin/env python3
"""Unit tests for the balanced test-shard selector (ci_shard.py).

Run: python .github/scripts/test_ci_shard.py
The CI-speed-policy workflow runs these so the sharding controller is itself
gated — a broken partition would silently drop tests from every shard.
"""
from __future__ import annotations

import io
import unittest

import ci_shard


def names(n: int) -> list[str]:
    return [f"Test{i:03d}" for i in range(n)]


class ReadTestNames(unittest.TestCase):
    def test_filters_non_test_lines(self):
        raw = "TestAlpha\nTestBeta\nok  \texample/pkg\t1.2s\n\nhelperFunc\n"
        got = ci_shard.read_test_names(io.StringIO(raw))
        self.assertEqual(got, ["TestAlpha", "TestBeta"])

    def test_dedupes_and_sorts(self):
        raw = "TestB\nTestA\nTestB\n"
        self.assertEqual(ci_shard.read_test_names(io.StringIO(raw)), ["TestA", "TestB"])


class Partition(unittest.TestCase):
    def test_union_is_input_no_overlap(self):
        for total in (1, 2, 3, 6, 7):
            for count in (0, 1, 5, 50, 325):
                ns = names(count)
                shards = ci_shard.assign_shards(ns, total, {})
                self.assertEqual(len(shards), total)
                flat = [n for s in shards for n in s]
                self.assertEqual(sorted(flat), ns, (total, count))
                self.assertEqual(len(flat), len(set(flat)), (total, count))

    def test_deterministic(self):
        ns = names(97)
        a = ci_shard.assign_shards(ns, 6, {})
        b = ci_shard.assign_shards(list(reversed(ns)), 6, {})
        self.assertEqual(a, b)

    def test_count_balanced_without_timings(self):
        shards = ci_shard.assign_shards(names(300), 6, {})
        sizes = [len(s) for s in shards]
        self.assertLessEqual(max(sizes) - min(sizes), 1)

    def test_lpt_balances_weighted_load(self):
        # One very heavy test plus many light ones: LPT must isolate the heavy
        # one and spread the rest so the makespan stays near optimal.
        ns = names(20)
        timings = {ns[0]: 100.0}
        for n in ns[1:]:
            timings[n] = 1.0
        shards = ci_shard.assign_shards(ns, 4, timings)
        loads = [sum(timings[n] for n in s) for s in shards]
        # Optimal makespan is dominated by the 100s test; LPT must not exceed it
        # by more than one light unit of slack per the 4/3 bound on this input.
        self.assertLessEqual(max(loads), 106.0)

    def test_more_shards_than_tests_leaves_empties(self):
        shards = ci_shard.assign_shards(names(2), 6, {})
        non_empty = [s for s in shards if s]
        self.assertEqual(sum(len(s) for s in shards), 2)
        self.assertEqual(len(non_empty), 2)


class Regex(unittest.TestCase):
    def test_anchored_alternation(self):
        self.assertEqual(ci_shard.shard_regex(["TestA", "TestB"]), "^(TestA|TestB)$")

    def test_empty_shard_is_empty_string(self):
        self.assertEqual(ci_shard.shard_regex([]), "")

    def test_escapes_metacharacters(self):
        # Defensive: a name with regex metacharacters must be escaped, not
        # interpreted, so it can never widen the selection.
        self.assertEqual(ci_shard.shard_regex(["Test.A+"]), r"^(Test\.A\+)$")


class Cli(unittest.TestCase):
    def _run(self, argv, stdin_text):
        import contextlib

        out, err = io.StringIO(), io.StringIO()
        stdin = io.StringIO(stdin_text)
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            import sys

            saved = sys.stdin
            sys.stdin = stdin
            try:
                code = ci_shard.main(argv)
            finally:
                sys.stdin = saved
        return code, out.getvalue(), err.getvalue()

    def test_index_prints_regex(self):
        code, out, _ = self._run(["--total", "2", "--index", "0"], "TestA\nTestB\n")
        self.assertEqual(code, 0)
        self.assertTrue(out.strip().startswith("^("))

    def test_verify_ok(self):
        stdin = "".join(f"{n}\n" for n in names(50))
        code, out, _ = self._run(["--total", "6", "--verify"], stdin)
        self.assertEqual(code, 0)
        self.assertIn("partition OK", out)

    def test_index_out_of_range(self):
        code, _, err = self._run(["--total", "2", "--index", "5"], "TestA\n")
        self.assertEqual(code, 2)
        self.assertIn("--index", err)

    def test_missing_index_without_verify(self):
        code, _, err = self._run(["--total", "2"], "TestA\n")
        self.assertEqual(code, 2)
        self.assertIn("--index is required", err)

    def test_empty_shard_prints_nothing(self):
        # 2 tests, 6 shards: shards 2..5 are empty -> empty stdout, exit 0.
        code, out, _ = self._run(["--total", "6", "--index", "5"], "TestA\nTestB\n")
        self.assertEqual(code, 0)
        self.assertEqual(out.strip(), "")


if __name__ == "__main__":
    unittest.main()
