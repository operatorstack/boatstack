package boatstack

import "path/filepath"

// The state-ownership map: one declared registry of every filesystem tree
// Boatstack manages — its class, partition, owning verbs, and whether the
// guard protects it from raw mutation. Before this map the same knowledge was
// scattered across path resolvers, guard regexes, and prose, and every
// state-partitioning defect (a clone-shared ledger, a worktree-local delivery
// state blocking a sibling, binary-vs-pin drift) was discovered by failing.
// The conformance suite in statemap_conformance_test.go holds the map, the
// WorkspaceContext resolvers, and the guard's path classifiers to each other,
// so the next divergence fails a test instead of a user.
// control-law: every-managed-path-has-a-declared-owner

// PathClass names what kind of state a managed tree holds.
type PathClass string

const (
	// ClassCommittedGenerated is the exported, hash-manifested bundle Boatstack
	// owns wholesale and regenerates (project.json, hooks, references).
	ClassCommittedGenerated PathClass = "committed-generated"
	// ClassCommittedPlanning is the durable feature evidence authored through
	// owned verbs and committed with the product (plans, approvals, locks, PRs).
	ClassCommittedPlanning PathClass = "committed-planning"
	// ClassCommittedInsight is the repository inbox for business and product
	// insights. Captures and append-only events are reviewable Git artifacts;
	// they are never runtime or detached controller state.
	ClassCommittedInsight PathClass = "committed-insight"
	// ClassCheckoutRuntime is reinstallable machine state living inside the
	// checkout but gitignored (the pinned helper binary, managed worktrees).
	ClassCheckoutRuntime PathClass = "checkout-runtime"
	// ClassRuntimeWorktree is mutable control state partitioned per worktree
	// under the worktree's own Git directory.
	ClassRuntimeWorktree PathClass = "runtime-worktree"
	// ClassRuntimeShared is control state shared by every worktree of a clone,
	// under the Git common directory.
	ClassRuntimeShared PathClass = "runtime-shared"
	// ClassHostActivation is host-owned configuration Boatstack merges into
	// (never owns wholesale): hook fragments in .claude/.cursor/.codex/.gemini.
	ClassHostActivation PathClass = "host-activation"
	// ClassDetached is the external per-user control root used by Detached
	// Supervision (repositories/, registry.json, shared runtimes).
	ClassDetached PathClass = "detached"
)

// StateEntry declares one managed tree.
type StateEntry struct {
	Name  string
	Class PathClass
	// Partition names the isolation domain: "checkout" (inside the worktree's
	// working tree), "per-worktree" (the worktree's Git dir), "git-common"
	// (shared by all worktrees), or "external" (outside the repository).
	Partition string
	// OwnerVerbs are the helper verbs/transitions that may create or mutate the
	// tree. Empty means host-owned (Boatstack only merges fragments in).
	OwnerVerbs []string
	Gitignored bool
	// GuardProtected marks trees only Boatstack transitions may name in a raw
	// mutation — exactly the set deliveryStatePathPattern denies.
	GuardProtected bool
	// Sample resolves one concrete representative path for conformance checks.
	Sample func(w WorkspaceContext) (string, error)
}

func staticSample(path string) func(WorkspaceContext) (string, error) {
	return func(WorkspaceContext) (string, error) { return path, nil }
}

func generatedSample(parts ...string) func(WorkspaceContext) (string, error) {
	return func(w WorkspaceContext) (string, error) {
		return filepath.Join(append([]string{w.GeneratedRoot()}, parts...)...), nil
	}
}

// StateRegistry returns the declared ownership map. It is a function (not a
// package variable) so entries can never be mutated by a caller.
func StateRegistry() []StateEntry {
	return []StateEntry{
		{
			Name: "project-config", Class: ClassCommittedGenerated, Partition: "checkout",
			OwnerVerbs: []string{"init", "update", "export"},
			Sample:     func(w WorkspaceContext) (string, error) { return w.ProjectConfigPath(), nil },
		},
		{
			Name: "source-config", Class: ClassCommittedGenerated, Partition: "checkout",
			OwnerVerbs: []string{"init", "migrate-config", "update"},
			Sample:     func(w WorkspaceContext) (string, error) { return w.SourceConfigPath(), nil },
		},
		{
			Name: "generated-references", Class: ClassCommittedGenerated, Partition: "checkout",
			OwnerVerbs: []string{"init", "update", "export"},
			Sample:     generatedSample("workflow.md"),
		},
		{
			Name: "guard-hooks", Class: ClassCommittedGenerated, Partition: "checkout",
			OwnerVerbs: []string{"init", "update", "export"},
			Sample:     generatedSample("hooks", "guard.sh"),
		},
		{
			Name: "runtime-launchers", Class: ClassCommittedGenerated, Partition: "checkout",
			OwnerVerbs: []string{"init", "update", "export"},
			Sample:     generatedSample("boatstack"),
		},
		{
			Name: "generated-lock", Class: ClassCommittedGenerated, Partition: "checkout",
			OwnerVerbs: []string{"init", "update", "export"},
			Sample:     generatedSample("generated.lock.json"),
		},
		{
			Name: "planning-artifacts", Class: ClassCommittedPlanning, Partition: "checkout",
			OwnerVerbs: []string{"planning-write"},
			Sample:     generatedSample("features", "sample-feature", "plan.md"),
		},
		{
			Name: "approval-receipt", Class: ClassCommittedPlanning, Partition: "checkout",
			OwnerVerbs: []string{"record-approval"},
			Sample:     generatedSample("features", "sample-feature", "approval.md"),
		},
		{
			Name: "autonomy-receipt", Class: ClassCommittedPlanning, Partition: "checkout",
			OwnerVerbs: []string{"record-autonomy"},
			Sample:     generatedSample("features", "sample-feature", "autonomy.md"),
		},
		{
			Name: "plan-lock", Class: ClassCommittedPlanning, Partition: "checkout",
			OwnerVerbs: []string{"activate-plan"},
			Sample:     generatedSample("features", "sample-feature", "plan.lock.json"),
		},
		{
			Name: "compiled-artifacts", Class: ClassCommittedPlanning, Partition: "checkout",
			OwnerVerbs: []string{"activate-plan"},
			Sample:     generatedSample("features", "sample-feature", "compiled", "tasks.json"),
		},
		{
			Name: "pr-preview", Class: ClassCommittedPlanning, Partition: "checkout",
			OwnerVerbs: []string{"ship-gate", "publish-pr"},
			Sample:     generatedSample("features", "sample-feature", "pr.md"),
		},
		{
			Name: "change-ledger", Class: ClassCommittedPlanning, Partition: "checkout",
			OwnerVerbs: []string{"record-change"},
			Sample:     generatedSample("features", "sample-feature", "changes.md"),
		},
		{
			Name: "discard-archive", Class: ClassCommittedPlanning, Partition: "checkout",
			OwnerVerbs: []string{"discard-delivery"},
			Sample:     generatedSample("features", ".discarded", "sample-feature", "plan.md"),
		},
		{
			Name: "pr-briefs", Class: ClassCommittedPlanning, Partition: "checkout",
			OwnerVerbs: []string{"pr-context"},
			Sample:     generatedSample("pr-briefs", "sample-branch", "pr.md"),
		},
		{
			Name: "verified-boundaries", Class: ClassCommittedPlanning, Partition: "checkout",
			OwnerVerbs: []string{"record-delivery-gate"},
			Sample:     generatedSample("verified-boundaries.md"),
		},
		{
			Name: "insight-artifacts", Class: ClassCommittedInsight, Partition: "checkout", GuardProtected: true,
			OwnerVerbs: []string{"insight"},
			Sample: func(w WorkspaceContext) (string, error) {
				base, err := w.InsightDir()
				if err != nil {
					return "", err
				}
				return filepath.Join(base, "ins-sample", "capture.json"), nil
			},
		},
		{
			Name: "worktree-helper", Class: ClassCheckoutRuntime, Partition: "checkout", Gitignored: true,
			OwnerVerbs: []string{"init", "update", "hydrate-runtime", "activate-worktree-runtime"},
			Sample:     generatedSample("bin", "install.lock.json"),
		},
		{
			Name: "managed-worktrees", Class: ClassCheckoutRuntime, Partition: "checkout", Gitignored: true,
			OwnerVerbs: []string{"workspace-cut", "workspace-cleanup", "workspace-reap"},
			Sample:     generatedSample("worktrees", "sample-branch"),
		},
		{
			Name: "delivery-state", Class: ClassRuntimeWorktree, Partition: "per-worktree", Gitignored: true, GuardProtected: true,
			OwnerVerbs: []string{"activate-plan", "record-delivery-gate", "record-change", "publish-pr", "repair-state", "discard-delivery"},
			Sample: func(w WorkspaceContext) (string, error) {
				base, err := w.DeliveryDir()
				if err != nil {
					return "", err
				}
				return filepath.Join(base, "sample-feature", "state.json"), nil
			},
		},
		{
			Name: "operation-ledger", Class: ClassRuntimeWorktree, Partition: "per-worktree", Gitignored: true, GuardProtected: true,
			OwnerVerbs: []string{"run-preflight", "publish-pr", "publish-update-pr"},
			Sample: func(w WorkspaceContext) (string, error) {
				base, err := w.OperationDir()
				if err != nil {
					return "", err
				}
				return filepath.Join(base, "sample-operation.json"), nil
			},
		},
		{
			Name: "flow-logs", Class: ClassRuntimeWorktree, Partition: "per-worktree", Gitignored: true, GuardProtected: true,
			OwnerVerbs: []string{"flow"},
			Sample: func(w WorkspaceContext) (string, error) {
				base, err := w.FlowDir()
				if err != nil {
					return "", err
				}
				return filepath.Join(base, "trajectory.jsonl"), nil
			},
		},
		{
			// The denial ledger backing repeated-denials-escalate-to-solutions.
			// Written only by the hook's own deny path; per-worktree so one
			// worktree's denial history never escalates a sibling's denials.
			Name: "guard-denial-ledger", Class: ClassRuntimeWorktree, Partition: "per-worktree", Gitignored: true, GuardProtected: true,
			OwnerVerbs: []string{"safety-hook", "ambient-safety-hook"},
			Sample: func(w WorkspaceContext) (string, error) {
				base, err := w.GuardDir()
				if err != nil {
					return "", err
				}
				return filepath.Join(base, "denials.json"), nil
			},
		},
		{
			Name: "runtime-slots", Class: ClassRuntimeShared, Partition: "git-common", Gitignored: true, GuardProtected: true,
			OwnerVerbs: []string{"init", "update", "hydrate-runtime"},
			Sample: func(w WorkspaceContext) (string, error) {
				base, err := w.RuntimeDir("v0.0.0", "0000000")
				if err != nil {
					return "", err
				}
				return filepath.Join(base, "runtime.lock.json"), nil
			},
		},
		{
			Name: "mutation-receipts", Class: ClassRuntimeShared, Partition: "git-common", Gitignored: true, GuardProtected: true,
			OwnerVerbs: []string{"activate-plan", "undo"},
			Sample:     staticSample(filepath.FromSlash(".git/boatstack/mutations/v1/sample.json")),
		},
		{
			Name: "update-previews", Class: ClassRuntimeShared, Partition: "git-common", Gitignored: true, GuardProtected: true,
			OwnerVerbs: []string{"prepare-update-pr", "publish-update-pr"},
			Sample:     staticSample(filepath.FromSlash(".git/boatstack/updates/v0.0.0/pr-preview.json")),
		},
		{
			Name: "repair-receipts", Class: ClassRuntimeShared, Partition: "git-common", Gitignored: true, GuardProtected: true,
			OwnerVerbs: []string{"update"},
			Sample:     staticSample(filepath.FromSlash(".git/boatstack/updates/v0.0.0/repair.json")),
		},
		{
			Name: "visual-evidence", Class: ClassRuntimeShared, Partition: "git-common", Gitignored: true, GuardProtected: true,
			OwnerVerbs: []string{"record-pr-visual-evidence", "review-pr-visual-evidence", "capture-evidence", "record-pr-visual-publication", "attach-evidence"},
			Sample:     staticSample(filepath.FromSlash(".git/boatstack/visual-evidence/sample/manifest.json")),
		},
		{
			Name: "quarantine", Class: ClassRuntimeShared, Partition: "git-common", Gitignored: true, GuardProtected: true,
			OwnerVerbs: []string{"repair-state"},
			Sample:     staticSample(filepath.FromSlash(".git/boatstack/quarantine/sample-feature/receipt.json")),
		},
		{
			Name: "host-hook-config", Class: ClassHostActivation, Partition: "checkout",
			// Host-owned files Boatstack merges hook fragments into; never owned
			// wholesale, so no owner verbs beyond the activation merge.
			OwnerVerbs: []string{"init", "update", "activate"},
			Sample:     staticSample(filepath.FromSlash(".claude/settings.json")),
		},
		{
			Name: "detached-registry", Class: ClassDetached, Partition: "external", GuardProtected: true,
			OwnerVerbs: []string{"attach", "detach"},
			Sample:     staticSample(filepath.FromSlash("state-root/boatstack/registry.json")),
		},
		{
			Name: "detached-repositories", Class: ClassDetached, Partition: "external", GuardProtected: true,
			OwnerVerbs: []string{"attach", "detach", "activate"},
			Sample:     staticSample(filepath.FromSlash("state-root/boatstack/repositories/sample/binding.json")),
		},
	}
}
