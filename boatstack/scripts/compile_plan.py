#!/usr/bin/env python3
"""Validate an approved structured plan and compile executable gate artifacts."""

from __future__ import annotations

import argparse
import json
from pathlib import Path


def fail(message: str) -> None:
    raise ValueError(message)


def validate(plan: dict) -> None:
    if plan.get("schema_version") != 1:
        fail("schema_version must be 1")
    if not plan.get("feature_id"):
        fail("feature_id is required")
    criteria = plan.get("acceptance_criteria")
    tasks = plan.get("tasks")
    if not isinstance(criteria, list) or not criteria:
        fail("at least one acceptance criterion is required")
    if not isinstance(tasks, list) or not tasks:
        fail("at least one task is required")

    criterion_ids = [item.get("id") for item in criteria if isinstance(item, dict)]
    task_ids = [item.get("id") for item in tasks if isinstance(item, dict)]
    if len(criterion_ids) != len(criteria) or None in criterion_ids or len(set(criterion_ids)) != len(criterion_ids):
        fail("acceptance criterion ids must be present and unique")
    if len(task_ids) != len(tasks) or None in task_ids or len(set(task_ids)) != len(task_ids):
        fail("task ids must be present and unique")

    known_criteria = set(criterion_ids)
    known_tasks = set(task_ids)
    covered: set[str] = set()
    graph: dict[str, list[str]] = {}
    for task in tasks:
        task_id = task["id"]
        dependencies = task.get("depends_on") or []
        mapped = task.get("acceptance_criteria") or []
        validations = task.get("validation") or []
        if task_id in dependencies:
            fail(f"task {task_id} cannot depend on itself")
        unknown_dependencies = set(dependencies) - known_tasks
        if unknown_dependencies:
            fail(f"task {task_id} has unknown dependencies: {sorted(unknown_dependencies)}")
        unknown_criteria = set(mapped) - known_criteria
        if unknown_criteria:
            fail(f"task {task_id} maps unknown criteria: {sorted(unknown_criteria)}")
        if not mapped and not task.get("enabling_reason"):
            fail(f"task {task_id} must map acceptance criteria or state an enabling_reason")
        if not isinstance(validations, list) or not validations:
            fail(f"task {task_id} requires at least one validation command or procedure")
        covered.update(mapped)
        graph[task_id] = list(dependencies)

    uncovered = known_criteria - covered
    if uncovered:
        fail(f"uncovered acceptance criteria: {sorted(uncovered)}")

    visiting: set[str] = set()
    visited: set[str] = set()

    def visit(task_id: str) -> None:
        if task_id in visiting:
            fail(f"task dependency cycle includes {task_id}")
        if task_id in visited:
            return
        visiting.add(task_id)
        for dependency in graph[task_id]:
            visit(dependency)
        visiting.remove(task_id)
        visited.add(task_id)

    for task_id in task_ids:
        visit(task_id)


def compile_artifacts(plan: dict) -> tuple[dict, dict, str]:
    criteria = {item["id"]: item for item in plan["acceptance_criteria"]}
    task_graph = {
        "schema_version": 1,
        "feature_id": plan["feature_id"],
        "source_plan_status": "HUMAN_APPROVED",
        "tasks": plan["tasks"],
    }
    rows = []
    for criterion_id, criterion in criteria.items():
        serving = [task for task in plan["tasks"] if criterion_id in (task.get("acceptance_criteria") or [])]
        validations = []
        for task in serving:
            for check in task.get("validation") or []:
                validations.append({"task_id": task["id"], "check": check})
        rows.append({
            "criterion_id": criterion_id,
            "criterion": criterion.get("text", ""),
            "tasks": [task["id"] for task in serving],
            "validations": validations,
            "result": "BLOCKED",
            "evidence": None,
        })
    test_matrix = {
        "schema_version": 1,
        "feature_id": plan["feature_id"],
        "requirements": rows,
    }
    evidence_lines = [
        f"# Evidence ledger: {plan['feature_id']}",
        "",
        "- Approved plan lock: pending",
        "- Test gate: `BLOCKED`",
        "- Review gate: `BLOCKED`",
        "- Ship gate: `BLOCKED`",
        "",
        "## Acceptance evidence",
        "",
        "| Criterion | Tasks | Result | Evidence |",
        "|---|---|---|---|",
    ]
    for row in rows:
        evidence_lines.append(
            f"| {row['criterion_id']}: {row['criterion']} | {', '.join(row['tasks'])} | `BLOCKED` | |"
        )
    evidence_lines.extend(["", "## Commands and checks", "", "## Review findings", "", "## Known gaps", "", "## Rollout and rollback", ""])
    return task_graph, test_matrix, "\n".join(evidence_lines)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--plan", type=Path, required=True)
    parser.add_argument("--out-dir", type=Path, required=True)
    args = parser.parse_args()
    try:
        plan = json.loads(args.plan.read_text())
        validate(plan)
        task_graph, test_matrix, evidence = compile_artifacts(plan)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"BLOCKED: invalid approved plan: {exc}")
        return 1
    args.out_dir.mkdir(parents=True, exist_ok=True)
    (args.out_dir / "tasks.json").write_text(json.dumps(task_graph, indent=2, sort_keys=True) + "\n")
    (args.out_dir / "test-matrix.json").write_text(json.dumps(test_matrix, indent=2, sort_keys=True) + "\n")
    (args.out_dir / "evidence.md").write_text(evidence)
    print(f"PASS: compiled approved plan into {args.out_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
