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


REPO = Path(__file__).resolve().parents[2]
RUNTIME = REPO / "boatstack"
CONFIG = REPO / "project.example.json"


class RepositoryContract(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.build = tempfile.TemporaryDirectory()
        cls.helper = Path(cls.build.name) / (
            "boatstack-helper.exe" if os.name == "nt" else "boatstack-helper"
        )
        result = subprocess.run(
            ["go", "build", "-o", str(cls.helper), "./cmd/boatstack-helper"],
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
        self.assertFalse((REPO / "UPSTREAM.json").exists())

    def test_release_builds_six_checksum_bound_v2_runtimes(self) -> None:
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
        for symbol in ("boatstack.Version", "boatstack.SourceCommit", "boatstack.ChecksumsSHA256"):
            self.assertIn(symbol, release)
        self.assertIn('source_commit="$(git rev-parse HEAD)"', release)
        self.assertIn("sha256sum", release)
        self.assertIn('workflows: ["Verify Boatstack distribution"]', automatic)

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

    def test_operation_skills_are_three_distinct_authority_preserving_surfaces(self) -> None:
        # control-law: operation-skill-discovery-preserves-authority-and-exact-cardinality
        skills = {
            path.parent.name: path.read_text()
            for path in sorted((REPO / ".agents" / "skills").glob("boatstack-*/SKILL.md"))
        }
        prompts = {
            path.parents[1].name: path.read_text()
            for path in sorted((REPO / ".agents" / "skills").glob("boatstack-*/agents/openai.yaml"))
        }
        readme = (REPO / "README.md").read_text()

        self.assertEqual(set(skills), {"boatstack-autoplan", "boatstack-run", "boatstack-update"})
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
        mappings = {
            "boatstack-autoplan": "`approved-plan` terminal",
            "boatstack-run": "`open-or-updated-pr` terminal",
            "boatstack-update": "`installation.update` transition",
        }
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
            "immediately preceding prescription",
            "complete apply response and stderr",
            "authority-bearing `FRONTIER`",
            "every requested authority source is materialized\nor conclusively rejected against the post-receipt state",
        ):
            for name, skill in skills.items():
                self.assertIn(contract, skill, name)
        self.assertIn("never grants merge authority", skills["boatstack-run"])
        for name in ("boatstack-autoplan", "boatstack-run"):
            for contract in (
                "request human and repository-policy authority sources",
                "repository-policy source remains requested",
                "do not pass `--repository-authority`\nuntil the current configuration has exact verified fingerprint evidence",
                "one bounded attempt for that receipt",
                "The kernel must derive the\nreceipt from that exact verified fingerprint",
                "do not retry it\nagain for the same receipt",
            ):
                self.assertIn(contract, skills[name], name)
        update = skills["boatstack-update"]
        self.assertIn("request only checksum-verified installation authority", update)
        self.assertIn(
            "Do not\nrequest or materialize repository, provider, publication, product-delivery, or\nmerge authority",
            update,
        )
        self.assertNotIn("repository-policy source remains requested", update)
        self.assertIn("Untargeted resolution selects\nonly a transition that advances the configured goal", readme)
        self.assertIn("exactly three operation skills", readme)

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
            "goal-configure", "plan-create", "plan-validate", "plan-approve",
            "plan-activate", "plan-amend", "workspace-cut", "workspace-sync",
            "workspace-cleanup", "workspace-reap", "record-build", "record-test",
            "record-review", "record-change", "record-journey",
            "publication-preview", "publish-pr", "observe-pr", "correct-pr",
            "abandon",
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
            "docs/architecture/boatstack-v2-*.md text eol=lf",
            "docs/architecture/boatstack-v2-*.mmd text eol=lf",
            "docs/architecture/boatstack-v2-*.json text eol=lf",
            "docs/architecture/boatstack-standard-flow.mmd text eol=lf",
        ):
            self.assertIn(pattern, attributes)

        response = json.loads(self.run_helper("catalog").stdout)
        transitions = response["catalog"]
        self.assertEqual(len(transitions), 62)
        self.assertEqual(len({item["id"] for item in transitions}), 62)
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
            (REPO / "docs" / "architecture" / "boatstack-v2-transition-catalog.md").read_text(),
        )
        self.assertEqual(
            mermaid,
            (REPO / "docs" / "architecture" / "boatstack-v2-transition-catalog.mmd").read_text(),
        )
        self.assertEqual(
            standard_flow,
            (REPO / "docs" / "architecture" / "boatstack-standard-flow.mmd").read_text(),
        )
        self.assertEqual(standard_flow.count("<br/>"), 30)
        for name, rendered in (
            ("boatstack-v2-locus-safety.json", locus_safety),
            ("boatstack-v2-locus-liveness.json", locus_liveness),
        ):
            checked = (REPO / "docs" / "architecture" / name).read_text()
            self.assertEqual(rendered, checked)
            model = json.loads(checked)
            self.assertEqual(len(model["events"]), 62)
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
            digest = hashlib.sha256(self.helper.read_bytes()).hexdigest()
            env = dict(os.environ)
            env.update(
                {
                    "BOATSTACK_REPO": str(repository),
                    "BOATSTACK_BINARY": str(self.helper),
                    "BOATSTACK_BINARY_SHA256": digest,
                    "BOATSTACK_INSTALL_DIR": str(install_dir),
                    "BOATSTACK_CONFIG": str(CONFIG),
                    "BOATSTACK_STATE_ROOT": str(root / "state"),
                    "BOATSTACK_ACTOR": "contract",
                    "BOATSTACK_VERSION": "contract-v2",
                }
            )
            self.run_command("bash", REPO / "install.sh", cwd=repository, env=env)
            launcher = install_dir / "boatstack"
            self.assertTrue(launcher.is_symlink())
            self.assertTrue((repository / ".boatstack" / "project.json").is_file())

            doctor = json.loads(
                self.run_command(launcher, "doctor", "--repo", repository, env=env).stdout
            )
            self.assertTrue(doctor["doctor"]["healthy"])
            self.assertEqual(doctor["doctor"]["transition_count"], 62)
            self.assertEqual(doctor["snapshot"]["runtime"]["value"], "verified")

            goal = (
                "--goal-id", "bootstrap", "--goal-kind", "approved-plan",
                "--delivery", "bootstrap",
            )
            self.run_command(
                launcher, "apply", "--repo", repository,
                "--transition", "engagement.begin", *goal,
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
            env["BOATSTACK_VERSION"] = "contract-v2-next"
            self.run_command("bash", REPO / "install.sh", cwd=repository, env=env)
            updated = json.loads(
                self.run_command(launcher, "doctor", "--repo", repository, env=env).stdout
            )
            self.assertTrue(updated["doctor"]["healthy"])
            self.assertIn("contract-v2-next", updated["snapshot"]["invocation"]["runtime_path"])
            events = self.run_command(
                launcher, "events", "--repo", repository, "--format", "jsonl", env=env
            ).stdout.splitlines()
            transitions = {json.loads(line)["transition_id"] for line in events}
            self.assertTrue({"installation.initialize", "engagement.begin", "installation.update"}.issubset(transitions))

    def test_installers_are_checksum_first_and_kernel_owned(self) -> None:
        shell = (REPO / "install.sh").read_text()
        powershell = (REPO / "install.ps1").read_text()
        for expected in (
            "BOATSTACK_BINARY_SHA256", "sha256sum", "shasum -a 256",
            '"$runtime" init', '"$runtime" update',
        ):
            self.assertIn(expected, shell)
        for expected in (
            "BOATSTACK_BINARY_SHA256", "Get-FileHash", "$Runtime init", "$Runtime update",
        ):
            self.assertIn(expected, powershell)
        self.assertNotIn("--repair", shell)
        self.assertNotIn("--repair", powershell)


if __name__ == "__main__":
    unittest.main()
