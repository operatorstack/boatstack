package boatstack

import (
	"fmt"
	"os"
	"path/filepath"
)

// Attach, detach, and status operations for Detached Supervision. Attaching a
// repository writes Boatstack's controller state to an external control root and a
// binding that identifies the repository; it never writes into the target working
// tree or its Git directory. Detaching removes the attachment (and, unless asked
// to preserve it, the external state). Status reports the binding and verifies it.

// AttachOptions requests a detached attachment. StateRoot, when set, overrides the
// external control-state root for this process (the CLI wires --state-root to it).
type AttachOptions struct {
	Repo  string
	Force bool
}

// AttachResult is the deterministic outcome of an attach request.
type AttachResult struct {
	SchemaVersion      int    `json:"schema_version"`
	VerificationStatus string `json:"verification_status"` // VERIFIED | BLOCKED
	Mode               string `json:"mode,omitempty"`
	RepoID             string `json:"repo_id,omitempty"`
	RepoRoot           string `json:"repo_root,omitempty"`
	ControlRoot        string `json:"control_root,omitempty"`
	WorktreeID         string `json:"worktree_id,omitempty"`
	Reason             string `json:"reason"`
}

func blockedAttach(reason string) AttachResult {
	return AttachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: reason}
}

// AttachDetached attaches repo in detached mode. It leaves the repository working
// tree and Git directory byte-for-byte unchanged; all controller state is written
// under the external control root.
func AttachDetached(opts AttachOptions) (AttachResult, error) {
	root, err := ResolveRepository(opts.Repo)
	if err != nil {
		return blockedAttach(err.Error()), nil
	}
	stateRoot, err := detachedStateRoot()
	if err != nil {
		return blockedAttach(err.Error()), nil
	}
	identity, err := repoIdentity(root)
	if err != nil {
		return blockedAttach("Boatstack could not compute a repository identity: " + err.Error()), nil
	}

	registry, err := loadRegistry(stateRoot)
	if err != nil {
		return blockedAttach("Boatstack could not read the attachment registry: " + err.Error()), nil
	}
	if existing, ok := registry.Repositories[root]; ok && !opts.Force {
		return blockedAttach(fmt.Sprintf("This repository is already attached (repo_id %s). Detach first, or re-run with --force.", existing)), nil
	}

	ctx := detachedContextFromIdentity(stateRoot, identity)

	// Synthesize configuration from the repository (test command, default branch,
	// context) exactly as embedded init does.
	config := defaultConfig(root, detectTestCommand(root))
	rawConfig, err := MarshalJSON(config)
	if err != nil {
		return blockedAttach(err.Error()), nil
	}

	// Generate the controller bundle and write it under the external control root.
	// The bundle layout mirrors embedded (.product-loop/** plus host adapter dirs),
	// only relocated outside the repository.
	bundle, err := BuildExportBundle(ctx.SourceConfigPath(), config, rawConfig, "boatstack")
	if err != nil {
		return blockedAttach("Boatstack could not build the controller bundle: " + err.Error()), nil
	}
	if err := os.MkdirAll(ctx.controlRoot, 0o755); err != nil {
		return blockedAttach(err.Error()), nil
	}
	if err := writeExport(ctx.controlRoot, bundle.Files, nil); err != nil {
		return blockedAttach("Boatstack could not write the controller bundle: " + err.Error()), nil
	}
	if err := os.WriteFile(ctx.SourceConfigPath(), rawConfig, 0o644); err != nil {
		return blockedAttach(err.Error()), nil
	}

	// Write the binding and index it in the registry.
	binding := DetachedBinding{
		SchemaVersion:     detachedSchemaVersion,
		Mode:              string(SupervisionDetached),
		RepoID:            identity.RepoID,
		CanonicalRepoPath: identity.CanonicalRepoPath,
		GitCommonIdentity: identity.GitCommonIdentity,
		InitialCommit:     identity.InitialCommit,
		NormalizedOrigin:  identity.NormalizedOrigin,
		CreatedByVersion:  Version,
		CreatedAt:         nowRFC3339(),
	}
	bindingRaw, err := MarshalJSON(binding)
	if err != nil {
		return blockedAttach(err.Error()), nil
	}
	if err := os.MkdirAll(filepath.Dir(bindingPath(stateRoot, identity.RepoID)), 0o755); err != nil {
		return blockedAttach(err.Error()), nil
	}
	if err := os.WriteFile(bindingPath(stateRoot, identity.RepoID), bindingRaw, 0o644); err != nil {
		return blockedAttach(err.Error()), nil
	}
	registry.Repositories[root] = identity.RepoID
	if err := saveRegistry(stateRoot, registry); err != nil {
		return blockedAttach(err.Error()), nil
	}
	invalidateWorkspaceCache()

	// Populate the external shared-runtime slot from the running helper so the
	// developer-level ambient guard has a stable helper to invoke. The binding is
	// written above, so WorkspaceFor now resolves detached and the slot is external.
	if source, execErr := os.Executable(); execErr == nil {
		if _, runtimeErr := installDetachedRuntime(root, source); runtimeErr != nil {
			return blockedAttach("Boatstack could not install the external runtime: " + runtimeErr.Error()), nil
		}
	}

	return AttachResult{
		SchemaVersion:      detachedSchemaVersion,
		VerificationStatus: "VERIFIED",
		Mode:               string(SupervisionDetached),
		RepoID:             identity.RepoID,
		RepoRoot:           root,
		ControlRoot:        ctx.controlRoot,
		WorktreeID:         identity.WorktreeID,
		Reason:             "Attached Boatstack in detached mode. The repository was not modified; all controller state lives under the external control root.",
	}, nil
}

// DetachOptions requests removal of a detached attachment.
type DetachOptions struct {
	Repo          string
	PreserveState bool
}

// DetachResult is the deterministic outcome of a detach request.
type DetachResult struct {
	SchemaVersion      int    `json:"schema_version"`
	VerificationStatus string `json:"verification_status"` // VERIFIED | BLOCKED
	RepoID             string `json:"repo_id,omitempty"`
	StateRemoved       bool   `json:"state_removed"`
	Reason             string `json:"reason"`
}

// DetachDetached removes a repository's detached attachment. It always removes the
// registry entry; it removes the external controller state only when PreserveState
// is false. It never touches the repository itself.
func DetachDetached(opts DetachOptions) (DetachResult, error) {
	root, err := ResolveRepository(opts.Repo)
	if err != nil {
		return DetachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: err.Error()}, nil
	}
	stateRoot, err := detachedStateRoot()
	if err != nil {
		return DetachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: err.Error()}, nil
	}
	registry, err := loadRegistry(stateRoot)
	if err != nil {
		return DetachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", Reason: err.Error()}, nil
	}
	repoID, ok := registry.Repositories[root]
	if !ok {
		return DetachResult{
			SchemaVersion: detachedSchemaVersion, VerificationStatus: "VERIFIED",
			Reason: "This repository is not attached in detached mode; nothing to detach.",
		}, nil
	}
	delete(registry.Repositories, root)
	if err := saveRegistry(stateRoot, registry); err != nil {
		return DetachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", RepoID: repoID, Reason: err.Error()}, nil
	}
	stateRemoved := false
	if !opts.PreserveState {
		if err := os.RemoveAll(repositoryControlRoot(stateRoot, repoID)); err != nil {
			return DetachResult{SchemaVersion: detachedSchemaVersion, VerificationStatus: "BLOCKED", RepoID: repoID, Reason: err.Error()}, nil
		}
		stateRemoved = true
	}
	invalidateWorkspaceCache()
	reason := "Detached Boatstack. The external controller state was removed."
	if opts.PreserveState {
		reason = "Detached Boatstack. The external controller state was preserved."
	}
	return DetachResult{
		SchemaVersion: detachedSchemaVersion, VerificationStatus: "VERIFIED",
		RepoID: repoID, StateRemoved: stateRemoved, Reason: reason,
	}, nil
}

// DetachedStatusResult reports whether a repository is attached in detached mode
// and whether its binding verifies.
type DetachedStatusResult struct {
	SchemaVersion int    `json:"schema_version"`
	Attached      bool   `json:"attached"`
	Verified      bool   `json:"verified"`
	Mode          string `json:"mode"`
	RepoID        string `json:"repo_id,omitempty"`
	RepoRoot      string `json:"repo_root,omitempty"`
	ControlRoot   string `json:"control_root,omitempty"`
	WorktreeID    string `json:"worktree_id,omitempty"`
	Reason        string `json:"reason"`
}

// DetachedStatus reports the detached attachment state for a repository. It is
// read-only.
func DetachedStatus(repoPath string) (DetachedStatusResult, error) {
	root, err := ResolveRepository(repoPath)
	if err != nil {
		return DetachedStatusResult{SchemaVersion: detachedSchemaVersion, Reason: err.Error()}, nil
	}
	ctx, ok, verifyErr := detachedContextFor(root)
	if !ok {
		return DetachedStatusResult{
			SchemaVersion: detachedSchemaVersion, Attached: false, Mode: string(SupervisionEmbedded),
			RepoRoot: root, Reason: "This repository is not attached in detached mode.",
		}, nil
	}
	if verifyErr != nil {
		return DetachedStatusResult{
			SchemaVersion: detachedSchemaVersion, Attached: true, Verified: false, Mode: string(SupervisionDetached),
			RepoRoot: root, Reason: verifyErr.Error(),
		}, nil
	}
	return DetachedStatusResult{
		SchemaVersion: detachedSchemaVersion, Attached: true, Verified: true, Mode: string(SupervisionDetached),
		RepoID: ctx.RepoID, RepoRoot: ctx.RepoRoot, ControlRoot: ctx.controlRoot, WorktreeID: ctx.WorktreeID,
		Reason: "This repository is attached in detached mode and its binding verifies.",
	}, nil
}
