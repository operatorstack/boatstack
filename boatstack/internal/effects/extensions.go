package effects

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/control"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
)

// NewExtensionLocalPrepared turns a declarative extension write plan into the
// same reversible prepared-effect contract used by first-party effects.
func NewExtensionLocalPrepared(repositoryRoot, extensionID string, writes []control.ResourceWrite, admission protocol.Admission, transition catalog.Transition) (ports.PreparedEffect, error) {
	return newNamespacedLocalPrepared(repositoryRoot, filepath.Join("extensions", extensionID), extensionID, "extension", writes, admission, transition)
}

// NewFlowLocalPrepared constrains a protocol-backed program runtime to its own
// repository-local namespace while retaining the normal reversible effect
// contract.
func NewFlowLocalPrepared(repositoryRoot, flowID string, writes []control.ResourceWrite, admission protocol.Admission, transition catalog.Transition) (ports.PreparedEffect, error) {
	return newNamespacedLocalPrepared(repositoryRoot, filepath.Join("flows", flowID), flowID, "program runtime", writes, admission, transition)
}

func newNamespacedLocalPrepared(repositoryRoot, namespace, owner, kind string, writes []control.ResourceWrite, admission protocol.Admission, transition catalog.Transition) (ports.PreparedEffect, error) {
	if len(writes) == 0 {
		return nil, fmt.Errorf("%s %q planned no local writes", kind, owner)
	}
	canonicalRepository, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve %s repository root: %w", kind, err)
	}
	lexicalRoot := filepath.Join(filepath.Clean(repositoryRoot), ".boatstack", namespace)
	root := filepath.Join(canonicalRepository, ".boatstack", namespace)
	mutations := make([]ports.ResourceMutation, 0, len(writes))
	seen := map[string]bool{}
	totalBytes := 0
	for _, write := range writes {
		if !strings.HasPrefix(write.Resource, owner+".") {
			return nil, fmt.Errorf("%s %q planned undeclared resource %q", kind, owner, write.Resource)
		}
		if !filepath.IsAbs(write.Path) {
			return nil, fmt.Errorf("%s %q planned a non-absolute path", kind, owner)
		}
		clean := filepath.Clean(write.Path)
		relative, err := filepath.Rel(lexicalRoot, clean)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return nil, fmt.Errorf("%s %q write escapes its namespaced resource root", kind, owner)
		}
		clean = filepath.Join(root, relative)
		if seen[clean] {
			return nil, fmt.Errorf("%s %q planned duplicate path %q", kind, owner, clean)
		}
		seen[clean] = true
		if write.Delete && len(write.Content) != 0 {
			return nil, fmt.Errorf("%s %q delete contains target bytes", kind, owner)
		}
		if !write.Delete {
			totalBytes += len(write.Content)
			if totalBytes > 4<<20 {
				return nil, fmt.Errorf("%s %q write plan exceeds 4 MiB", kind, owner)
			}
			digest := sha256.Sum256(write.Content)
			if write.SHA256 == "" || hex.EncodeToString(digest[:]) != write.SHA256 {
				return nil, fmt.Errorf("%s %q write digest mismatch", kind, owner)
			}
		}
		mode := os.FileMode(write.Mode)
		if mode == 0 {
			mode = 0o600
		}
		if err := rejectSymlinkComponents(canonicalRepository, clean); err != nil {
			return nil, fmt.Errorf("%s %q write path is unsafe: %w", kind, owner, err)
		}
		mutation, err := mutationFor(clean, write.Content, mode, false, write.Delete)
		if err != nil {
			return nil, err
		}
		mutation.Resource, mutation.Owner = write.Resource, owner
		mutations = append(mutations, mutation)
	}
	prepared := &preparedEffect{mutations: mutations}
	if err := bindPreparedCapabilities(prepared, admission, transition); err != nil {
		return nil, err
	}
	return prepared, nil
}

func rejectSymlinkComponents(repositoryRoot, target string) error {
	relative, err := filepath.Rel(repositoryRoot, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("target escapes the canonical repository")
	}
	current := repositoryRoot
	for _, segment := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, segment)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink component %q", current)
		}
	}
	return nil
}

func NewExtensionExternalPrepared(execute func(context.Context) (ports.EffectResult, error), admission protocol.Admission, transition catalog.Transition) (ports.PreparedEffect, error) {
	if execute == nil {
		return nil, fmt.Errorf("external extension effect requires an executor")
	}
	prepared := &preparedEffect{boundary: execute}
	if err := bindPreparedCapabilities(prepared, admission, transition); err != nil {
		return nil, err
	}
	return prepared, nil
}
