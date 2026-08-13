from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path

from boatstack_test_support import prescription_cli_arguments


REPO = Path(__file__).resolve().parents[2]
RUNTIME = REPO / "boatstack"
CONFIG = REPO / "project.example.json"
DOMAIN_NEUTRAL_ROOTS = (
    ("pullrequest", "pull request"),
    ("codingagent", "coding agent"),
    ("github", "github"),
    ("repository", "repository"),
    ("worktree", "worktree"),
    ("publication", "publication"),
    ("branch", "branch"),
    ("git", "git"),
)
DOMAIN_NEUTRAL_PHRASE = re.compile(r"\b(?:pull\s+request|coding\s+agent)\b", re.IGNORECASE)
GO_IDENTIFIER = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")
GO_IDENTIFIER_PART = re.compile(r"[A-Z]?[a-z]+|[A-Z]+(?![a-z])|[0-9]+")


def go_source_metadata(paths: list[Path]) -> list[dict[str, object]]:
    result = subprocess.run(
        [
            "go",
            "run",
            str(REPO / ".github" / "tests" / "go_source_metadata.go"),
            "--",
            *map(str, paths),
        ],
        cwd=REPO,
        text=True,
        capture_output=True,
    )
    if result.returncode != 0:
        raise RuntimeError(result.stdout + result.stderr)
    return json.loads(result.stdout)


def identifier_domain_token(identifier: str) -> str | None:
    for component in identifier.split("_"):
        for part in GO_IDENTIFIER_PART.findall(component):
            normalized = part.lower()
            for root, token in DOMAIN_NEUTRAL_ROOTS:
                if normalized == root or normalized.startswith(root):
                    return token
    return None


def import_domain_token(import_path: str) -> str | None:
    components = import_path.split("/")
    if components and components[0] == "github.com":
        components = components[1:]
    for component in components:
        for identifier in re.split(r"[.-]", component):
            token = identifier_domain_token(identifier)
            if token is not None:
                return token
    return None


def domain_vocabulary_hits(paths: list[Path]) -> list[tuple[Path, str]]:
    hits: list[tuple[Path, str]] = []
    for path, metadata in zip(paths, go_source_metadata(paths), strict=True):
        for import_path in metadata["imports"]:
            token = import_domain_token(import_path)
            if token is not None:
                hits.append((path, token))
        for source in metadata["vocabulary"]:
            phrase = DOMAIN_NEUTRAL_PHRASE.search(source)
            if phrase is not None:
                hits.append((path, phrase.group(0).lower()))
                continue
            for identifier in GO_IDENTIFIER.findall(source):
                token = identifier_domain_token(identifier)
                if token is not None:
                    hits.append((path, token))
                    break
    return hits


class RepositoryContract(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.build = tempfile.TemporaryDirectory()
        cls.helper = Path(cls.build.name) / (
            "boatstack-helper.exe" if os.name == "nt" else "boatstack-helper"
        )
        cls.old_helper = Path(cls.build.name) / (
            "boatstack-helper-old.exe" if os.name == "nt" else "boatstack-helper-old"
        )
        for output, version, source in (
            (cls.helper, "v0.7.contract-new", "b" * 40),
            (cls.old_helper, "v0.7.contract-old", "a" * 40),
        ):
            ldflags = " ".join(
                (
                    f"-X github.com/operatorstack/boatstack/boatstack/internal/buildinfo.Version={version}",
                    f"-X github.com/operatorstack/boatstack/boatstack/internal/buildinfo.SourceCommit={source}",
                )
            )
            result = subprocess.run(
                ["go", "build", "-ldflags", ldflags, "-o", str(output), "./cmd/boatstack-helper"],
                cwd=RUNTIME,
                text=True,
                capture_output=True,
            )
            if result.returncode != 0:
                raise RuntimeError(result.stdout + result.stderr)

    @classmethod
    def tearDownClass(cls) -> None:
        cls.build.cleanup()

    def run_command(
        self,
        *args: object,
        cwd: Path | None = None,
        env: dict[str, str] | None = None,
        stdin: str | None = None,
        expected: int = 0,
    ) -> subprocess.CompletedProcess[str]:
        result = subprocess.run(
            [*map(str, args)],
            cwd=cwd,
            env=env,
            input=stdin,
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        return result

    def run_helper(
        self,
        *args: object,
        env: dict[str, str] | None = None,
        stdin: str | None = None,
        expected: int = 0,
    ) -> subprocess.CompletedProcess[str]:
        return self.run_command(
            self.helper, *args, env=env, stdin=stdin, expected=expected
        )

    def apply_prescribed(
        self,
        binary: Path,
        transition: str,
        *args: object,
        cwd: Path | None = None,
        env: dict[str, str] | None = None,
    ) -> dict:
        resolved = json.loads(
            self.run_command(
                binary, "next", "--transition", transition, *args,
                cwd=cwd, env=env,
            ).stdout
        )
        prescription = resolved["prescription"]
        correlation = resolved["snapshot"]["invocation"]["correlation_id"]
        applied = self.run_command(
            binary, "apply", "--transition", transition, *args,
            *prescription_cli_arguments(prescription, correlation),
            cwd=cwd, env=env,
        )
        return json.loads(applied.stdout)

    def init_repository(self, root: Path) -> None:
        self.run_command("git", "init", "-b", "main", cwd=root)
        self.run_command("git", "config", "user.name", "Boatstack Test", cwd=root)
        self.run_command(
            "git", "config", "user.email", "boatstack@example.invalid", cwd=root
        )
        (root / "README.md").write_text("# Fixture\n")
        self.run_command("git", "add", "README.md", cwd=root)
        self.run_command("git", "commit", "-m", "fixture", cwd=root)

    def test_active_workflows_have_no_private_upstream_authority(self) -> None:
        workflows = REPO / ".github" / "workflows"
        self.assertFalse((workflows / "sync-upstream.yml").exists())
        for workflow in workflows.glob("*.yml"):
            value = workflow.read_text()
            self.assertNotIn("operatorstack/intelligence-flow", value, workflow)
            self.assertNotIn("sync/intelligence-flow-", value, workflow)
            self.assertNotIn("UPSTREAM.json", value, workflow)

    def test_codex_review_is_secret_scoped_read_only_and_structured(self) -> None:
        workflow = (REPO / ".github" / "workflows" / "codex-review.yml").read_text()
        prompt = (REPO / ".github" / "codex" / "review-prompt.md").read_text()
        schema = json.loads((REPO / ".github" / "codex" / "review-output-schema.json").read_text())

        self.assertIn("pull_request:", workflow)
        self.assertNotIn("pull_request_target", workflow)
        self.assertIn("CODEX_REVIEWER_API", workflow)
        self.assertIn("head.repo.full_name", workflow)
        self.assertIn("not configured", workflow)
        self.assertIn("persist-credentials: false", workflow)
        self.assertIn('git merge-base "$BASE_SHA" "$HEAD_SHA"', workflow)
        self.assertIn('git show "$BASE_SHA:.github/codex/review-prompt.md"', workflow)
        self.assertIn('git show "$BASE_SHA:.github/codex/review-output-schema.json"', workflow)
        self.assertIn("output-schema-file: ${{ steps.policy.outputs.schema }}", workflow)
        self.assertNotIn("cp .github/codex/review-prompt.md", workflow)
        self.assertIn("base revision has no admitted review policy", workflow)
        self.assertIn("first 200 shown", workflow)
        self.assertNotIn('diff --unified=5 "$BASE_SHA" "$HEAD_SHA"', workflow)
        self.assertIn("permission-profile: \":read-only\"", workflow)
        self.assertIn("safety-strategy: drop-sudo", workflow)
        self.assertRegex(workflow, r"openai/codex-action@[0-9a-f]{40}")
        self.assertIn("pull-requests: write", workflow)
        self.assertIn("contents: read", workflow)
        self.assertIn("gpt-5.6-sol", workflow)
        self.assertIn("CODEX_REVIEW_EFFORT || 'high'", workflow)
        self.assertIn("untrusted data", prompt)
        self.assertIn("`LEFT` for deleted lines", prompt)
        self.assertIn("Resolver / apply agreement", prompt)
        self.assertIn("Receipts as facts", prompt)
        self.assertIn("Questions for model-level verification", prompt)
        self.assertIn("Review to closure before reporting findings", prompt)
        self.assertIn("second pass over the relevant dimensions", prompt)
        self.assertIn("soundness: construct invalid implementations", prompt)
        self.assertIn("completeness: construct valid implementations", prompt)
        self.assertIn("CLOSURE OBLIGATIONS", prompt)
        self.assertIn("Return only the object required by the supplied output schema", prompt)
        self.assertEqual(
            set(schema["required"]),
            {"findings", "overall_correctness", "overall_explanation", "overall_confidence_score"},
        )
        finding = schema["properties"]["findings"]["items"]
        self.assertEqual(
            set(finding["required"]),
            {"title", "body", "confidence_score", "priority", "code_location"},
        )
        location = finding["properties"]["code_location"]
        self.assertIn("side", location["required"])
        self.assertEqual(location["properties"]["side"]["enum"], ["LEFT", "RIGHT"])
        self.assertIn("build_codex_github_review.py", workflow)
        publisher = (REPO / ".github" / "scripts" / "build_codex_github_review.py").read_text()
        self.assertIn('"side": side', publisher)
        self.assertIn("Findings without inline diff anchors", publisher)
        self.assertFalse((REPO / "UPSTREAM.json").exists())

    def test_release_builds_six_checksum_bound_v2_runtimes(self) -> None:
        ci = (REPO / ".github" / "workflows" / "ci.yml").read_text()
        release = (REPO / ".github" / "workflows" / "release.yml").read_text()
        automatic = (REPO / ".github" / "workflows" / "auto-release.yml").read_text()
        for asset in (
            "boatstack-helper_linux_amd64",
            "boatstack-helper_linux_arm64",
            "boatstack-helper_darwin_amd64",
            "boatstack-helper_darwin_arm64",
            "boatstack-helper_windows_amd64.exe",
            "boatstack-helper_windows_arm64.exe",
        ):
            self.assertIn(asset, release)
        for symbol in ("internal/buildinfo.Version", "internal/buildinfo.SourceCommit", "internal/buildinfo.ChecksumsSHA256"):
            self.assertIn(symbol, release)
        self.assertIn('source_commit="$(git rev-parse HEAD)"', release)
        self.assertIn("sha256sum", release)
        ci_name = re.search(r"(?m)^name:\s*(.+?)\s*$", ci)
        self.assertIsNotNone(ci_name)
        self.assertEqual(ci_name.group(1), "CI")
        self.assertIn(f'workflows: ["{ci_name.group(1)}"]', automatic)

    def test_manual_release_is_prerelease_only_and_exact_source_bound(self) -> None:
        # control-law: branch-prerelease-publishes-only-an-exact-new-rc-source
        release = (REPO / ".github" / "workflows" / "release.yml").read_text()
        for contract in (
            "prerelease_tag:",
            "manual prereleases must be dispatched from a branch",
            "manual release tags must use vMAJOR.MINOR.PATCH-rc.NUMBER",
            "release tag $RELEASE_TAG already exists",
            "selected branch no longer resolves to exact source $RELEASE_SOURCE",
            '--target "$RELEASE_SOURCE"',
            "--prerelease",
        ):
            self.assertIn(contract, release)
        self.assertIn("VERSION: ${{ inputs.prerelease_tag || github.ref_name }}", release)
        self.assertIn('RELEASE_SOURCE: ${{ github.sha }}', release)

    def test_kernel_skill_is_maintenance_only_and_delivery_entries_are_repository_owned(self) -> None:
        # control-law: kernel-skill-discovery-cannot-invent-domain-entry-authority
        skills = {
            path.parent.name: path.read_text()
            for path in sorted((REPO / ".agents" / "skills").glob("boatstack-*/SKILL.md"))
        }
        prompts = {
            path.parents[1].name: path.read_text()
            for path in sorted((REPO / ".agents" / "skills").glob("boatstack-*/agents/openai.yaml"))
        }
        readme = (REPO / "README.md").read_text()

        self.assertEqual(set(skills), {"boatstack-update"})
        self.assertEqual(set(prompts), set(skills))
        for host_root in (REPO / ".claude" / "skills", REPO / ".gemini" / "skills"):
            projected = {
                path.parent.name: path.read_text()
                for path in sorted(host_root.glob("boatstack-*/SKILL.md"))
            }
            self.assertEqual(projected, skills)
        cursor = {
            path.stem: path.read_text()
            for path in sorted((REPO / ".cursor" / "commands").glob("boatstack-*.md"))
        }
        self.assertEqual(cursor, skills)
        self.assertFalse((REPO / "boatstack" / "SKILL.md").exists())
        mappings = {"boatstack-update": "`installation.reconcile-update`"}
        for name, mapping in mappings.items():
            self.assertIn(f"name: {name}", skills[name])
            self.assertIn(mapping, skills[name])
            self.assertIn(f"${name}", prompts[name])
        for contract in (
            "authority-free\n`FRONTIER`",
            "command-scoped context",
            "every `next`, `apply`, `recover`, and re-resolution",
            "requested authority sources separately from currently\nmaterialized authority receipts",
            "untargeted authority-bearing `next`",
            "immediately preceding `PRESCRIBED`",
            "complete apply response and stderr",
            "authority-bearing `FRONTIER`",
            "every requested authority source is materialized\nor conclusively rejected against the post-receipt state",
        ):
            for name, skill in skills.items():
                self.assertIn(contract, skill, name)
        update = skills["boatstack-update"]
        self.assertIn("request only checksum-verified installation authority", update)
        self.assertIn(
            "Do not\nrequest or materialize repository, provider, publication, product-delivery, or\nmerge authority",
            update,
        )
        self.assertNotIn("repository-policy source remains requested", update)
        for contract in (
            "preserve the healthy admitted\nruntime",
            "program-delta fingerprint",
            "Do not accept the delta implicitly",
            "`--accept-program-change`",
            "single atomic\n`installation.reconcile-update` boundary",
            "carry the same human authority\nthrough that rollback",
        ):
            self.assertIn(contract, update)
        self.assertIn("Untargeted resolution selects\nonly a transition that advances the configured objective", readme)
        self.assertIn("installer generates the maintenance skill", readme)
        self.assertIn("A repository Flow declares its own entries", readme)
        self.assertIn("does\nnot interpret the word `run`", readme)

        _, kernel_and_later = readme.split("### Kernel", 1)
        kernel_section, delivery_and_later = kernel_and_later.split("### Software delivery", 1)
        delivery_section, _ = delivery_and_later.split("### Developer surfaces", 1)
        self.assertNotIn("idempotent replay", kernel_section.lower())
        self.assertIn("idempotent replay", delivery_section.lower())
        for delivery_only_authority in (
            "human",
            "autonomy",
            "repository-policy",
            "external-provider",
            "maximum capability surface",
        ):
            self.assertNotIn(delivery_only_authority, kernel_section.lower())
            self.assertIn(delivery_only_authority, delivery_section.lower())

    def test_document_links_claims_and_assets_are_valid(self) -> None:
        def anchors(document: Path) -> set[str]:
            result: set[str] = set()
            for heading in re.findall(
                r"^#{1,6}\s+(.+?)\s*$", document.read_text(), re.MULTILINE
            ):
                plain = re.sub(r"<[^>]+>", "", heading).strip().lower()
                plain = re.sub(r"[^\w\s-]", "", plain)
                result.add(re.sub(r"\s+", "-", plain))
            return result

        documents = [REPO / "README.md", *sorted((REPO / "docs").rglob("*.md"))]
        for document in documents:
            for target in re.findall(r"\[[^\]]+\]\(([^)]+)\)", document.read_text()):
                if target.startswith(("http://", "https://", "#", "mailto:")):
                    continue
                relative, _, anchor = target.partition("#")
                resolved = (document.parent / relative).resolve()
                self.assertTrue(resolved.exists(), f"broken link {target} in {document}")
                if anchor and resolved.suffix == ".md":
                    self.assertIn(anchor, anchors(resolved), f"broken anchor {target}")

        configuration = (REPO / "docs" / "configuration.md").read_text()
        for example in re.findall(r"```json\n(.*?)\n```", configuration, re.DOTALL):
            json.loads(example)

        claims = json.loads((REPO / "docs" / "public-claims.json").read_text())
        self.assertEqual(claims["schema_version"], 2)
        allowed = set(claims["statuses"])
        for claim in claims["claims"]:
            self.assertIn(claim["status"], allowed)
            self.assertRegex(claim["last_verified_version"], r"^v\d+\.\d+\.\d+$")
            readable, _, anchor = claim["readable_evidence"].partition("#")
            readable_path = REPO / "docs" / readable
            self.assertTrue(readable_path.is_file(), claim["id"])
            self.assertIn(anchor, anchors(readable_path), claim["id"])
            for evidence in claim["implementation"] + claim["verification"]:
                self.assertTrue((REPO / "docs" / evidence).resolve().is_file(), evidence)

        for name in (
            "boatstack-mark.svg",
            "boatstack-journey.svg",
            "boatstack-portability.svg",
        ):
            path = REPO / "assets" / name
            root = ET.parse(path).getroot()
            self.assertEqual(root.attrib.get("role"), "img", name)
            value = path.read_text()
            self.assertIn("<title", value, name)
            self.assertIn("<desc", value, name)

    def test_public_tree_excludes_private_context_and_v1_operating_guidance(self) -> None:
        private_values = (
            "Tax" + "Weave",
            "/" + "Users/apple/Documents/GitHub/" + "tax" + "weave",
            "big" + "boateng",
            "cursor_password_" + "reset_button_addition",
        )
        text_suffixes = {".go", ".json", ".md", ".ps1", ".py", ".sh", ".yaml", ".yml"}
        for path in REPO.rglob("*"):
            if not path.is_file() or ".git" in path.parts or path.suffix not in text_suffixes:
                continue
            value = path.read_text(errors="replace")
            for private in private_values:
                self.assertNotIn(private, value, path)

        current_guidance = [
            REPO / "README.md",
            *sorted((REPO / ".agents" / "skills").glob("boatstack-*/SKILL.md")),
            *sorted((REPO / "docs").glob("*.md")),
            *sorted((REPO / "boatstack" / "references").glob("*.md")),
        ]
        deprecated = (
            "plan-gate",
            "product-loop",
            "planning-write",
            "ship-gate",
            "insight-capture",
            "deliverycontrol",
        )
        for document in current_guidance:
            value = document.read_text().lower()
            for token in deprecated:
                self.assertNotIn(token, value, document)

    def test_general_kernel_is_domain_neutral_and_owns_shared_control_laws(self) -> None:
        kernel = REPO / "boatstack" / "kernel"
        kernel_files = sorted(kernel.glob("*.go"))
        self.assertEqual([], domain_vocabulary_hits(kernel_files))
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary) / "domain_leak_test.go"
            for source, token in (
                ('package kernel\nconst fixtureDomain = "pull request"\n', "pull request"),
                ("package kernel\ntype gitClient struct{}\n", "git"),
                ("package kernel\nvar testRepository string\n", "repository"),
                ("package kernel\ntype worktreeManager struct{}\n", "worktree"),
                ('package kernel\nconst provider = "github"\n', "github"),
                ("package kernel\ntype githubClient struct{}\n", "github"),
                ("package kernel\ntype gitclient struct{}\n", "git"),
                ("package kernel\ntype repositoryclient struct{}\n", "repository"),
                ("package kernel\ntype worktreemanager struct{}\n", "worktree"),
                ("package kernel\ntype branchmanager struct{}\n", "branch"),
                ("package kernel\ntype publicationqueue struct{}\n", "publication"),
                ("package kernel\ntype pullrequesthandler struct{}\n", "pull request"),
                ("package kernel\ntype codingagentpolicy struct{}\n", "coding agent"),
            ):
                with self.subTest(token=token):
                    fixture.write_text(source)
                    self.assertEqual([(fixture, token)], domain_vocabulary_hits([fixture]))
            fixture.write_text('package kernel\nimport "github.com/example/provider"\n')
            self.assertEqual([], domain_vocabulary_hits([fixture]))
            fixture.write_text(
                'package kernel\nimport vcs "github.com/go-git/go-git/v5"\nvar _ = vcs.PlainClone\n'
            )
            self.assertEqual([(fixture, "git")], domain_vocabulary_hits([fixture]))
            fixture.write_text("package kernel\nfunc TestDigitalSignature() {}\n")
            self.assertEqual([], domain_vocabulary_hits([fixture]))

        boatstack_packages = "github.com/operatorstack/boatstack/boatstack/"
        kernel_package = boatstack_packages + "kernel"
        conformance_package = kernel_package + "/conformance"
        kernel_metadata = go_source_metadata(kernel_files)
        for path, metadata in zip(kernel_files, kernel_metadata, strict=True):
            allowed = {kernel_package}
            if path.name.endswith("_test.go"):
                allowed.add(conformance_package)
            invalid = [
                import_path
                for import_path in metadata["imports"]
                if import_path.startswith(boatstack_packages)
                and import_path not in allowed
            ]
            self.assertEqual([], invalid, f"kernel dependency direction: {path}")

        kernel_tests = sorted(kernel.glob("*_test.go"))
        test_source = "\n".join(path.read_text() for path in kernel_tests)
        self.assertNotRegex(test_source, r'\bexec\.Command\(\s*"git"')
        self.assertNotRegex(
            test_source,
            r'\bexec\.CommandContext\([^,\n]+,\s*"git"',
        )
        self.assertNotIn("testRepository", test_source)
        self.assertNotIn("softwaredelivery", test_source.lower())
        test_metadata = go_source_metadata(kernel_tests)
        for path, metadata in zip(kernel_tests, test_metadata, strict=True):
            self.assertFalse(
                any(
                    "/softwaredelivery" in import_path
                    for import_path in metadata["imports"]
                ),
                f"kernel test fixture imports software delivery: {path}",
            )

        conformance_files = sorted((kernel / "conformance").glob("*.go"))
        self.assertTrue(conformance_files)
        conformance_source = "\n".join(path.read_text() for path in conformance_files)
        for token in (
            "git", "repository", "worktree", "pull request",
            "softwaredelivery", "subprocess",
        ):
            self.assertNotRegex(
                conformance_source.lower(),
                rf"\b{re.escape(token)}\b",
                token,
            )
        for path, metadata in zip(
            conformance_files,
            go_source_metadata(conformance_files),
            strict=True,
        ):
            invalid = [
                import_path
                for import_path in metadata["imports"]
                if import_path.startswith(boatstack_packages)
                and import_path != kernel_package
            ]
            invalid.extend(
                import_path
                for import_path in metadata["imports"]
                if import_path in {"os", "os/exec", "path/filepath"}
            )
            self.assertEqual([], invalid, f"kernel conformance dependency: {path}")
        self.assertIn("conformance.IntegerFixture().Run(t)", test_source)

        runtime = (kernel / "runtime.go").read_text()
        software_relation = (
            REPO / "boatstack" / "internal" / "softwaredelivery" /
            "supervisor" / "supervisor.go"
        ).read_text()
        software_prescription = (
            REPO / "boatstack" / "internal" / "softwaredelivery" /
            "protocol" / "prescription.go"
        ).read_text()
        self.assertIn("Relate(RelationInput", runtime)
        self.assertIn("general.Relate(general.RelationInput", software_relation)
        self.assertIn("general.Freshness", software_prescription)
        self.assertIn("general.NewFreshness", software_prescription)

        implementation = "\n".join(
            path.read_text() for path in sorted((REPO / "boatstack").rglob("*.go"))
        )
        for retired in (
            "boatstack/control\"", "internal/kernel", "internal/effects",
            "internal/plant", "internal/surfaces", "goal.configure",
            "GOAL_REQUIRED", 'json:"goal',
        ):
            self.assertNotIn(retired, implementation)

        component_ci = (REPO / ".github" / "workflows" / "ci.yml").read_text()
        for current in (
            "./kernel", "./controlprogram", "./delivery", "./internal/softwaredelivery/protocol",
            "./internal/softwaredelivery/surfaces",
            "./internal/softwaredelivery/plant",
            "./internal/softwaredelivery/effects",
        ):
            self.assertIn(current, component_ci)
        for retired in (
            "./internal/kernel", "./internal/plant",
            "./internal/effects", "./internal/surfaces",
        ):
            self.assertNotIn(retired, component_ci)
        self.assertNotIn("packages: ./control\n", component_ci)
        self.assertIn("run: go test -race ./...", component_ci)
        self.assertIn(
            "run: go test -race ./kernel ./internal/softwaredelivery/effects",
            component_ci,
        )

    def test_go_import_parser_accepts_legal_import_forms(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            commented = Path(directory) / "commented.go"
            commented.write_text(
                'package consumer\n\nimport /* migration T5 */ '
                '"github.com/operatorstack/boatstack/boatstack/kernel"\n'
            )
            line_broken = Path(directory) / "line_broken.go"
            line_broken.write_text(
                'package consumer\n\nimport\n b "\\u0067ithub.com/operatorstack/'
                'boatstack/boatstack/internal/buildinfo"\n'
            )
            self.assertEqual(
                [
                    "github.com/operatorstack/boatstack/boatstack/kernel",
                    "github.com/operatorstack/boatstack/boatstack/internal/buildinfo",
                ],
                [
                    metadata["imports"][0]
                    for metadata in go_source_metadata([commented, line_broken])
                ],
            )

    @unittest.expectedFailure
    def test_kernel_runtime_has_no_production_consumer_yet(self) -> None:
        # Migration task T5 must replace this marker with a permanent positive
        # production-reachability assertion when the generic runtime is adopted.
        production_files = [
            path
            for path in sorted((REPO / "boatstack").rglob("*.go"))
            if not path.name.endswith("_test.go")
            and not path.is_relative_to(REPO / "boatstack" / "kernel")
        ]
        consumers = [
            str(path.relative_to(REPO))
            for path, metadata in zip(
                production_files,
                go_source_metadata(production_files),
                strict=True,
            )
            if metadata["runtime_consumers"]
        ]
        self.assertTrue(
            consumers,
            "migration T5 has not connected the generic kernel runtime to production code",
        )

    def test_documented_cli_verbs_are_registered_v2_surfaces(self) -> None:
        documents = [
            REPO / "README.md",
            *sorted((REPO / ".agents" / "skills").glob("boatstack-*/SKILL.md")),
            *sorted((REPO / "docs").glob("*.md")),
            *sorted((REPO / "boatstack" / "references").glob("*.md")),
        ]
        registered = {
            "status", "next", "next-status", "apply", "recover", "doctor",
            "events", "catalog", "guard", "rpc", "retro", "version", "init",
            "update", "attach", "detach", "hydrate-runtime", "configure",
            "reconcile-update",
            "objective-bind", "plan-create", "plan-validate", "plan-approve",
            "plan-activate", "plan-amend", "workspace-cut", "workspace-sync",
            "workspace-cleanup", "workspace-reap", "record-build", "record-test",
            "record-review", "record-change", "record-journey",
            "publication-preview", "publish-pr", "observe-pr", "correct-pr",
            "abandon", "flow",
        }
        pattern = re.compile(
            r"(?m)^[ \t]*(?:\$[A-Za-z_][A-Za-z0-9_]*/)?"
            r"boatstack(?:-helper)?[ \t]+([a-z][a-z0-9-]*)"
        )
        seen: set[str] = set()
        for document in documents:
            for verb in pattern.findall(document.read_text().replace("\\\n", " ")):
                seen.add(verb)
                self.assertIn(verb, registered, f"unregistered command in {document}")
        self.assertTrue({"status", "apply", "catalog"}.issubset(seen))

    def test_catalog_and_generated_artifacts_match_the_executable_registry(self) -> None:
        attributes = (REPO / ".gitattributes").read_text().splitlines()
        for pattern in (
            "docs/architecture/boatstack-*.md text eol=lf",
            "docs/architecture/boatstack-*.mmd text eol=lf",
            "docs/architecture/boatstack-*.json text eol=lf",
            "docs/architecture/boatstack-standard-flow.mmd text eol=lf",
        ):
            self.assertIn(pattern, attributes)

        response = json.loads(self.run_helper("catalog").stdout)
        transitions = response["catalog"]
        self.assertEqual(len(transitions), 63)
        self.assertEqual(len({item["id"] for item in transitions}), 63)
        self.assertEqual(
            {item["class"] for item in transitions},
            {"authority", "owned-local", "owned-external", "recovery", "observed-external"},
        )
        markdown = self.run_helper("catalog", "--format", "markdown").stdout
        mermaid = self.run_helper("catalog", "--format", "mermaid").stdout
        standard_flow = self.run_helper(
            "catalog", "--format", "standard-flow-mermaid"
        ).stdout
        locus_safety = self.run_helper(
            "catalog", "--format", "locus-safety"
        ).stdout
        locus_liveness = self.run_helper(
            "catalog", "--format", "locus-liveness"
        ).stdout
        self.assertEqual(
            markdown,
            (REPO / "docs" / "architecture" / "boatstack-transition-catalog.md").read_text(),
        )
        self.assertEqual(
            mermaid,
            (REPO / "docs" / "architecture" / "boatstack-transition-catalog.mmd").read_text(),
        )
        self.assertEqual(
            standard_flow,
            (REPO / "docs" / "architecture" / "boatstack-standard-flow.mmd").read_text(),
        )
        self.assertEqual(standard_flow.count("<br/>"), 30)
        for name, rendered in (
            ("boatstack-locus-safety.json", locus_safety),
            ("boatstack-locus-liveness.json", locus_liveness),
        ):
            checked = (REPO / "docs" / "architecture" / name).read_text()
            self.assertEqual(rendered, checked)
            model = json.loads(checked)
            self.assertEqual(len(model["events"]), 63)
            self.assertEqual(
                {event["id"] for event in model["events"]},
                {item["id"] for item in transitions},
            )

    def test_rpc_and_configuration_decoders_fail_closed(self) -> None:
        malformed_rpc = json.dumps(
            {
                "schema_version": 2,
                "operation": "catalog",
                "repository": ".",
                "host": "cli",
                "correlation_id": "contract",
                "unexpected": True,
            }
        )
        rejected = self.run_helper("rpc", stdin=malformed_rpc, expected=1)
        self.assertIn("unknown field", rejected.stderr.lower())

        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            repository = root / "repo"
            repository.mkdir()
            self.init_repository(repository)
            invalid = json.loads(CONFIG.read_text())
            invalid["unexpected"] = True
            invalid_path = root / "invalid.json"
            invalid_path.write_text(json.dumps(invalid))
            env = dict(os.environ)
            env["BOATSTACK_STATE_ROOT"] = str(root / "state")
            rejected = self.run_helper(
                "init", "--repo", repository, "--human", "contract",
                "--param", f"config_path={invalid_path}", env=env, expected=1,
            )
            self.assertIn("unknown field", (rejected.stdout + rejected.stderr).lower())
            self.assertFalse((repository / ".boatstack" / "project.json").exists())

    def test_offline_installer_initializes_updates_and_guards_through_kernel(self) -> None:
        if os.name == "nt":
            self.skipTest("the repository contract job exercises the POSIX installer")
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            repository = root / "repo"
            install_dir = root / "bin"
            repository.mkdir()
            self.init_repository(repository)
            digest = hashlib.sha256(self.old_helper.read_bytes()).hexdigest()
            env = dict(os.environ)
            env.update(
                {
                    "BOATSTACK_REPO": str(repository),
                    "BOATSTACK_BINARY": str(self.old_helper),
                    "BOATSTACK_BINARY_SHA256": digest,
                    "BOATSTACK_INSTALL_DIR": str(install_dir),
                    "BOATSTACK_HOME": str(root / "home"),
                    "BOATSTACK_CONFIG": str(CONFIG),
                    "BOATSTACK_STATE_ROOT": str(root / "state"),
                    "BOATSTACK_ACTOR": "contract",
                    "BOATSTACK_VERSION": "contract-v2",
                }
            )
            installed = self.run_command("bash", REPO / "install.sh", cwd=repository, env=env)
            self.assertIn(".boatstack/runtime.json", installed.stdout)
            launcher = install_dir / "boatstack"
            self.assertTrue(launcher.is_file())
            self.assertFalse(launcher.is_symlink())
            self.assertTrue((repository / ".boatstack" / "project.json").is_file())

            self.run_command("git", "add", ".", cwd=repository)
            self.run_command("git", "commit", "-m", "install Boatstack", cwd=repository)
            clone = root / "clone"
            self.run_command("git", "clone", str(repository), str(clone))
            fresh = json.loads(
                self.run_command(
                    launcher, "init", "--repo", clone, "--human", "contract",
                    "--param", f"config_path={clone / '.boatstack' / 'project.json'}",
                    "--format", "json", env=env,
                ).stdout
            )
            self.assertEqual(fresh["receipt"]["transition_id"], "installation.initialize")
            fresh_doctor = json.loads(
                self.run_command(launcher, "doctor", "--repo", clone, env=env).stdout
            )
            self.assertTrue(fresh_doctor["doctor"]["healthy"])

            invalid_clone = root / "invalid-clone"
            self.run_command("git", "clone", str(repository), str(invalid_clone))
            invalid_pin_path = invalid_clone / ".boatstack" / "runtime.json"
            invalid_pin = json.loads(invalid_pin_path.read_text())
            invalid_pin["version"] = "v-conflicting"
            invalid_pin_path.write_text(json.dumps(invalid_pin, indent=2) + "\n")
            state_before = {
                path.relative_to(root / "state"): path.read_bytes()
                for path in (root / "state").rglob("*") if path.is_file()
            }
            refused = self.run_command(
                self.old_helper, "init", "--repo", invalid_clone, "--human", "contract",
                "--param", f"config_path={invalid_clone / '.boatstack' / 'project.json'}",
                "--format", "json", env=env, expected=1,
            )
            self.assertIn("not admissible", refused.stderr)
            state_after = {
                path.relative_to(root / "state"): path.read_bytes()
                for path in (root / "state").rglob("*") if path.is_file()
            }
            self.assertEqual(state_after, state_before)

            doctor = json.loads(
                self.run_command(launcher, "doctor", "--repo", repository, env=env).stdout
            )
            self.assertTrue(doctor["doctor"]["healthy"])
            self.assertEqual(doctor["doctor"]["transition_count"], 63)
            self.assertEqual(doctor["snapshot"]["runtime"]["value"], "verified")

            objective = (
                "--objective-id", "bootstrap", "--target-id", "approved-plan",
                "--delivery", "bootstrap",
            )
            self.apply_prescribed(
                launcher, "objective.bind", "--repo", repository, *objective,
                "--human", "contract", "--param", "target_id=approved-plan",
                "--param", "delivery_id=bootstrap", env=env,
            )
            self.apply_prescribed(
                launcher, "engagement.begin", "--repo", repository, *objective,
                "--repository-authority", env=env,
            )
            ordinary = json.loads(
                self.run_command(
                    launcher, "guard", "--repo", repository,
                    "--command", "go test ./...", env=env,
                ).stdout
            )
            self.assertTrue(ordinary["guard"]["allowed"])
            managed = json.loads(
                self.run_command(
                    launcher, "guard", "--repo", repository,
                    "--command", "git push origin HEAD", env=env,
                ).stdout
            )
            self.assertFalse(managed["guard"]["allowed"])
            self.assertEqual(managed["guard"]["required_transition"], "publication.execute")
            destructive = json.loads(
                self.run_command(
                    launcher, "guard", "--repo", repository,
                    "--command", "git reset --hard HEAD~1", env=env,
                ).stdout
            )
            self.assertFalse(destructive["guard"]["allowed"])

            env["BOATSTACK_MODE"] = "update"
            env["BOATSTACK_BINARY"] = str(self.helper)
            env["BOATSTACK_BINARY_SHA256"] = hashlib.sha256(self.helper.read_bytes()).hexdigest()
            env["BOATSTACK_ACCEPT_PROGRAM_CHANGE"] = "true"
            self.run_command("bash", REPO / "install.sh", cwd=repository, env=env)
            updated = json.loads(
                self.run_command(launcher, "doctor", "--repo", repository, env=env).stdout
            )
            self.assertTrue(updated["doctor"]["healthy"])
            pin = json.loads((repository / ".boatstack" / "runtime.json").read_text())
            self.assertEqual(pin["version"], "v0.7.contract-new")
            self.assertEqual(pin["sha256"], hashlib.sha256(self.helper.read_bytes()).hexdigest())
            self.assertNotIn("path", pin)
            events = [json.loads(line) for line in self.run_command(
                launcher, "events", "--repo", repository, "--format", "jsonl", env=env
            ).stdout.splitlines()]
            transitions = {event["transition_id"] for event in events}
            self.assertTrue({"installation.initialize", "engagement.begin", "installation.reconcile-update"}.issubset(transitions))
            for event in events:
                self.assertTrue(event["authority_fingerprint"])
                self.assertTrue(event["required_capabilities"])
                self.assertTrue(event["granted_capabilities"])
                self.assertNotIn("exercised_capabilities", event)
                self.assertTrue(event["committed_effects"])
                self.assertEqual(event["verification"]["result"], "satisfied")

    def test_program_changing_update_is_explicit_atomic_and_dormant_safe(self) -> None:
        # control-law: accepted-program-delta-atomically-pins-runtime-and-program
        if os.name == "nt":
            self.skipTest("the repository contract job exercises the POSIX installer")
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            repository = root / "repo"
            install_dir = root / "bin"
            state_root = root / "state"
            repository.mkdir()
            self.init_repository(repository)
            env = dict(os.environ)
            env.update(
                {
                    "BOATSTACK_REPO": str(repository),
                    "BOATSTACK_BINARY": str(self.old_helper),
                    "BOATSTACK_BINARY_SHA256": hashlib.sha256(self.old_helper.read_bytes()).hexdigest(),
                    "BOATSTACK_INSTALL_DIR": str(install_dir),
                    "BOATSTACK_CONFIG": str(CONFIG),
                    "BOATSTACK_STATE_ROOT": str(state_root),
                    "BOATSTACK_ACTOR": "contract",
                    "BOATSTACK_VERSION": "contract-old",
                }
            )
            self.run_command("bash", REPO / "install.sh", cwd=repository, env=env)
            launcher = install_dir / "boatstack"
            old_launcher = launcher.read_bytes()
            old_pin = (repository / ".boatstack" / "runtime.json").read_bytes()
            state_path = next((repository / ".git" / "boatstack").rglob("state.json"))
            state_before = state_path.read_bytes()

            env.update(
                {
                    "BOATSTACK_MODE": "update",
                    "BOATSTACK_BINARY": str(self.helper),
                    "BOATSTACK_BINARY_SHA256": hashlib.sha256(self.helper.read_bytes()).hexdigest(),
                    "BOATSTACK_VERSION": "contract-new",
                }
            )
            candidate_status = json.loads(
                self.run_command(
                    self.helper, "status", "--repo", repository, env=env
                ).stdout
            )
            prior_program = candidate_status["snapshot"]["recorded_program_fingerprint"]
            split_reconciliation = self.run_command(
                self.helper,
                "next",
                "--repo",
                repository,
                "--transition",
                "catalog.reconcile",
                "--human",
                "contract",
                "--objective-id",
                "bootstrap",
                "--target-id",
                "approved-plan",
                "--delivery",
                "bootstrap",
                "--param",
                f"prior_program_fingerprint={prior_program}",
                "--param",
                "accept_obligation_change=true",
                env=env,
            )
            self.assertIn(
                "catalog reconciliation cannot activate a different runtime",
                split_reconciliation.stdout + split_reconciliation.stderr,
            )
            self.assertEqual(launcher.read_bytes(), old_launcher)
            self.assertEqual((repository / ".boatstack" / "runtime.json").read_bytes(), old_pin)
            self.assertEqual(state_path.read_bytes(), state_before)
            rejected = self.run_command(
                "bash", REPO / "install.sh", cwd=repository, env=env, expected=1
            )
            self.assertIn(
                "compiled control program drift requires explicit reconciliation",
                rejected.stdout + rejected.stderr,
            )
            refusal = json.loads(rejected.stdout)
            change = refusal["program_change"]
            self.assertEqual(change["prior_program_fingerprint"], prior_program)
            self.assertEqual(
                change["candidate_program_fingerprint"],
                refusal["snapshot"]["program_fingerprint"],
            )
            self.assertRegex(change["program_delta_fingerprint"], r"^[0-9a-f]{64}$")
            self.assertEqual(change["required_transition"], "installation.reconcile-update")
            self.assertEqual(change["acceptance_flag"], "--accept-program-change")
            self.assertEqual(launcher.read_bytes(), old_launcher)
            self.assertEqual((repository / ".boatstack" / "runtime.json").read_bytes(), old_pin)
            self.assertEqual(state_path.read_bytes(), state_before)

            env["BOATSTACK_ACCEPT_PROGRAM_CHANGE"] = "true"
            self.run_command("bash", REPO / "install.sh", cwd=repository, env=env)
            pin = json.loads((repository / ".boatstack" / "runtime.json").read_text())
            self.assertEqual(pin["version"], "v0.7.contract-new")
            self.assertEqual(pin["sha256"], hashlib.sha256(self.helper.read_bytes()).hexdigest())
            self.assertNotIn("path", pin)
            doctor = json.loads(
                self.run_command(launcher, "doctor", "--repo", repository, env=env).stdout
            )
            self.assertTrue(doctor["doctor"]["healthy"])
            self.assertTrue(doctor["doctor"]["runtime_healthy"])
            self.assertTrue(doctor["doctor"]["update_ready"])
            self.assertFalse(doctor["doctor"]["recovery_required"])
            self.assertEqual(doctor["snapshot"]["engagement"]["value"], "dormant")

            receipt_path = next(state_root.rglob("receipts.jsonl"))
            receipts = [json.loads(line) for line in receipt_path.read_text().splitlines()]
            update = next(
                receipt
                for receipt in receipts
                if receipt["transition_id"] == "installation.reconcile-update"
            )
            self.assertTrue(update["program_change_accepted"])
            self.assertEqual(update["kind"], "transition-committed")
            self.assertRegex(update["prior_program_fingerprint"], r"^[0-9a-f]{64}$")
            self.assertTrue(update["program"]["id"])
            self.assertTrue(update["program"]["version"])
            self.assertRegex(update["program"]["fingerprint"], r"^[0-9a-f]{64}$")
            self.assertRegex(update["program_delta_fingerprint"], r"^[0-9a-f]{64}$")
            self.assertTrue(update["committed_effects"])
            self.assertEqual(update["verification"]["result"], "satisfied")
            self.assertEqual(
                update["runtime_fingerprint"], hashlib.sha256(self.helper.read_bytes()).hexdigest()
            )
            self.assertEqual(update["runtime_source_revision"], "b" * 40)

    def test_installers_are_checksum_first_and_kernel_owned(self) -> None:
        shell = (REPO / "install.sh").read_text()
        powershell = (REPO / "install.ps1").read_text()
        for expected in (
            "BOATSTACK_BINARY_SHA256", "sha256sum", "shasum -a 256",
            '"$runtime" init', "update_arguments=(", '"$runtime" "${update_arguments[@]}"',
            "BOATSTACK_ACCEPT_PROGRAM_CHANGE", "--accept-program-change",
            ".boatstack/runtime.json",
        ):
            self.assertIn(expected, shell)
        for expected in (
            "BOATSTACK_BINARY_SHA256", "Get-FileHash", "$Runtime init", "$Runtime update",
            "BOATSTACK_ACCEPT_PROGRAM_CHANGE", "--accept-program-change",
            ".boatstack\\runtime.json",
        ):
            self.assertIn(expected, powershell)
        self.assertNotIn("--repair", shell)
        self.assertNotIn("--repair", powershell)


if __name__ == "__main__":
    unittest.main()
