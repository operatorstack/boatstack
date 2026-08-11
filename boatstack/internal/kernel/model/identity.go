package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
)

type Topology string

const (
	TopologyEmbedded Topology = "embedded"
	TopologyDetached Topology = "detached"
	TopologyHybrid   Topology = "hybrid"
)

func (t Topology) Valid() bool {
	return t == TopologyEmbedded || t == TopologyDetached || t == TopologyHybrid
}

// InvocationContext is carried unchanged from a public surface to every
// observation, admission, effect, verification, and receipt.
type InvocationContext struct {
	RepositoryID       string   `json:"repository_id"`
	GitCommonID        string   `json:"git_common_id"`
	WorktreeID         string   `json:"worktree_id"`
	Ref                string   `json:"ref"`
	ControllerID       string   `json:"controller_id"`
	InvokingPath       string   `json:"invoking_path"`
	RuntimeVersion     string   `json:"runtime_version"`
	RuntimePath        string   `json:"runtime_path"`
	RuntimeFingerprint string   `json:"runtime_fingerprint"`
	Topology           Topology `json:"topology"`
	Host               string   `json:"host"`
	Correlation        string   `json:"correlation_id"`
}

// DeriveWorktreeID binds a worktree to both its Git-common identity and its
// canonical repository root. The destination of workspace.cut can therefore
// be named before Git creates its administrative directory, while a path by
// itself can never become effect authority.
func DeriveWorktreeID(gitCommonID, repositoryRoot string) (string, error) {
	if gitCommonID == "" || repositoryRoot == "" || !filepath.IsAbs(repositoryRoot) {
		return "", fmt.Errorf("worktree identity requires git-common identity and an absolute repository root")
	}
	digest := sha256.Sum256([]byte(gitCommonID + "\x00" + filepath.Clean(repositoryRoot)))
	return hex.EncodeToString(digest[:])[:12], nil
}

func (c InvocationContext) Validate(effectful bool) error {
	if !c.Topology.Valid() {
		return fmt.Errorf("invocation: invalid topology %q", c.Topology)
	}
	if c.RepositoryID == "" || c.GitCommonID == "" || c.ControllerID == "" {
		return fmt.Errorf("invocation: repository, git-common, and controller identity are required")
	}
	if c.InvokingPath == "" || !filepath.IsAbs(c.InvokingPath) {
		return fmt.Errorf("invocation: invoking path must be explicit and absolute")
	}
	if c.Host == "" || c.Correlation == "" {
		return fmt.Errorf("invocation: host and correlation identity are required")
	}
	if effectful && (c.WorktreeID == "" || c.Ref == "" || c.RuntimeVersion == "" || c.RuntimePath == "" || c.RuntimeFingerprint == "" || !filepath.IsAbs(c.RuntimePath)) {
		return fmt.Errorf("invocation: effectful operation requires worktree, ref, and exact runtime version, location, and fingerprint")
	}
	return nil
}
