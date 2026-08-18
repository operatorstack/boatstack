package effects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/ports"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

type boundaryCall func(context.Context) (ports.EffectResult, error)

type preparedEffect struct {
	mutations             []ports.ResourceMutation
	boundary              boundaryCall
	postVerify            func(context.Context) error
	verifyInvocation      *model.InvocationContext
	applied               []ports.ResourceMutation
	appliedTreeRoots      []string
	removedTreeGroups     []atomicTreeRemoval
	boundarySettled       bool
	effectResult          ports.EffectResult
	transition            catalog.Transition
	requiredCapabilities  []catalog.Capability
	effectiveCapabilities []catalog.Capability
	changedStateFacets    []model.StateFacet
}

type atomicTreeRemoval struct {
	root      string
	mutations []ports.ResourceMutation
}

func (p *preparedEffect) ChangedStateFacets() []model.StateFacet {
	return append([]model.StateFacet(nil), p.changedStateFacets...)
}

func (p *preparedEffect) Manifest() []ports.ResourceMutation {
	result := make([]ports.ResourceMutation, len(p.mutations))
	copy(result, p.mutations)
	return result
}

func (p *preparedEffect) CommittedEffects() []protocol.EffectFact {
	facts := make([]protocol.EffectFact, 0, len(p.applied)+1)
	for _, mutation := range p.applied {
		resource, owner := mutation.Resource, mutation.Owner
		if resource == "" && len(p.transition.OwnedResources) > 0 {
			resource = p.transition.OwnedResources[0]
		}
		if owner == "" {
			owner = p.transition.Owner
		}
		operation := "update"
		switch {
		case mutation.Delete:
			operation = "delete"
		case mutation.TargetLink != "":
			operation = "symlink"
		case !mutation.PriorExists:
			operation = "create"
		}
		facts = append(facts, protocol.EffectFact{
			Kind: protocol.EffectResourceMutation, EffectID: p.transition.Effect, Owner: owner, Resource: resource,
			Target: mutation.Path, Operation: operation,
			PriorFingerprint:     mutationStateFingerprint(mutation.PriorExists, mutation.Prior, mutation.PriorLink, mutation.Mode),
			ResultingFingerprint: mutationStateFingerprint(!mutation.Delete, mutation.Target, mutation.TargetLink, mutation.Mode),
		})
	}
	if p.boundarySettled {
		raw, _ := json.Marshal(p.effectResult)
		facts = append(facts, protocol.EffectFact{
			Kind: protocol.EffectBoundarySettled, EffectID: p.transition.Effect, Owner: p.transition.Owner,
			Target: string(p.transition.ID), Operation: "settled",
			PriorFingerprint: sha256Bytes([]byte("not-applicable")), ResultingFingerprint: sha256Bytes(raw),
		})
	}
	return facts
}

func mutationStateFingerprint(exists bool, content []byte, link string, mode uint32) string {
	value := struct {
		Exists        bool   `json:"exists"`
		ContentSHA256 string `json:"content_sha256,omitempty"`
		Link          string `json:"link,omitempty"`
		Mode          uint32 `json:"mode,omitempty"`
	}{Exists: exists, Link: link}
	if exists && link == "" {
		value.ContentSHA256 = sha256Bytes(content)
		value.Mode = mode
	}
	raw, _ := json.Marshal(value)
	return sha256Bytes(raw)
}

func (p *preparedEffect) VerificationInvocation() (model.InvocationContext, bool) {
	if p.verifyInvocation == nil {
		return model.InvocationContext{}, false
	}
	return *p.verifyInvocation, true
}

func (p *preparedEffect) Execute(ctx context.Context) (ports.EffectResult, error) {
	result := ports.EffectResult{Settlement: ports.EffectSettled}
	if len(p.requiredCapabilities) == 0 {
		return result, fmt.Errorf("effect boundary has no kernel capability classification")
	}
	if missing := catalog.MissingCapability(p.requiredCapabilities, catalog.NewCapabilitySet(p.effectiveCapabilities...)); missing != "" {
		return result, fmt.Errorf("EFFECT_CAPABILITY_DENIED %q at prepared effect boundary", missing)
	}
	if p.boundary != nil {
		var err error
		result, err = p.boundary(ctx)
		if err != nil || result.Settlement == ports.EffectUnknown {
			return result, err
		}
		p.boundarySettled = true
		p.effectResult = result
	}
	ordered := append([]ports.ResourceMutation(nil), p.mutations...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].InstallLast != ordered[j].InstallLast {
			return !ordered[i].InstallLast
		}
		return ordered[i].Path < ordered[j].Path
	})
	treeGroups := map[string][]ports.ResourceMutation{}
	regular := ordered[:0]
	for _, mutation := range ordered {
		if mutation.AtomicTreeRoot != "" {
			treeGroups[mutation.AtomicTreeRoot] = append(treeGroups[mutation.AtomicTreeRoot], mutation)
		} else {
			regular = append(regular, mutation)
		}
	}
	treeRoots := make([]string, 0, len(treeGroups))
	for root := range treeGroups {
		treeRoots = append(treeRoots, root)
	}
	sort.Strings(treeRoots)
	for _, root := range treeRoots {
		group := treeGroups[root]
		allDelete := true
		for _, mutation := range group {
			allDelete = allDelete && mutation.Delete && mutation.TargetLink == "" && len(mutation.Target) == 0
		}
		if allDelete {
			for _, mutation := range group {
				relative, err := filepath.Rel(root, mutation.Path)
				if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
					return result, fmt.Errorf("atomic tree deletion contains member outside root: %s", mutation.Path)
				}
			}
			if err := os.RemoveAll(root); err != nil {
				return result, fmt.Errorf("remove immutable resource tree %s: %w", root, err)
			}
			if err := syncDirectory(filepath.Dir(root)); err != nil {
				return result, fmt.Errorf("sync immutable resource tree parent %s: %w", root, err)
			}
			p.applied = append(p.applied, group...)
			p.removedTreeGroups = append(p.removedTreeGroups, atomicTreeRemoval{root: root, mutations: group})
			continue
		}
		allExisting := true
		for _, mutation := range group {
			allExisting = allExisting && mutation.PriorExists
		}
		if allExisting {
			// Recovery reconstructs an already-installed atomic tree as an
			// exact no-op mutation group. It still committed every staged
			// resource, so retain the group for exact effect facts.
			p.applied = append(p.applied, group...)
			continue
		}
		if err := atomicInstallTree(root, group); err != nil {
			return result, fmt.Errorf("install immutable resource tree %s: %w", root, err)
		}
		p.applied = append(p.applied, group...)
		p.appliedTreeRoots = append(p.appliedTreeRoots, root)
	}
	for _, mutation := range regular {
		var err error
		if mutation.Delete {
			err = os.Remove(mutation.Path)
			if os.IsNotExist(err) {
				err = nil
			}
		} else if mutation.TargetLink != "" {
			err = atomicSymlink(mutation.Path, mutation.TargetLink)
		} else {
			err = atomicWrite(mutation.Path, mutation.Target, os.FileMode(mutation.Mode))
		}
		if err != nil {
			return result, fmt.Errorf("install managed resource %s: %w", mutation.Path, err)
		}
		p.applied = append(p.applied, mutation)
	}
	if p.postVerify != nil {
		if err := p.postVerify(ctx); err != nil {
			return result, err
		}
	}
	return result, nil
}

func bindPreparedCapabilities(effect *preparedEffect, admission protocol.Admission, transition catalog.Transition) error {
	if err := protocol.ValidateEffectCapabilities(admission, transition); err != nil {
		return err
	}
	effect.requiredCapabilities = catalog.RequiredCapabilities(transition)
	effect.effectiveCapabilities = append([]catalog.Capability(nil), admission.EffectiveCapabilities...)
	effect.transition = transition
	return nil
}

func (p *preparedEffect) Rollback(context.Context) error {
	var rollbackErrors []error
	for index := len(p.appliedTreeRoots) - 1; index >= 0; index-- {
		if err := os.RemoveAll(p.appliedTreeRoots[index]); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		} else if err := syncDirectory(filepath.Dir(p.appliedTreeRoots[index])); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	for index := len(p.applied) - 1; index >= 0; index-- {
		mutation := p.applied[index]
		if mutation.AtomicTreeRoot != "" {
			continue
		}
		if mutation.PriorLink != "" {
			if err := atomicSymlink(mutation.Path, mutation.PriorLink); err != nil {
				rollbackErrors = append(rollbackErrors, err)
			}
		} else if mutation.PriorExists {
			if err := atomicWrite(mutation.Path, mutation.Prior, os.FileMode(mutation.Mode)); err != nil {
				rollbackErrors = append(rollbackErrors, err)
			}
		} else if err := os.Remove(mutation.Path); err != nil && !os.IsNotExist(err) {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	for index := len(p.removedTreeGroups) - 1; index >= 0; index-- {
		removed := p.removedTreeGroups[index]
		restore := make([]ports.ResourceMutation, 0, len(removed.mutations))
		for _, mutation := range removed.mutations {
			if !mutation.PriorExists {
				continue
			}
			restore = append(restore, ports.ResourceMutation{Path: mutation.Path, Target: mutation.Prior, TargetLink: mutation.PriorLink, Mode: mutation.Mode, InstallLast: mutation.InstallLast, AtomicTreeRoot: removed.root})
		}
		if len(restore) > 0 {
			if err := atomicInstallTree(removed.root, restore); err != nil {
				rollbackErrors = append(rollbackErrors, err)
			}
		}
	}
	p.applied = nil
	p.appliedTreeRoots = nil
	p.removedTreeGroups = nil
	if p.boundarySettled {
		rollbackErrors = append(rollbackErrors, fmt.Errorf("external effect settled and requires reconciliation or compensation"))
	}
	return errors.Join(rollbackErrors...)
}

func atomicInstallTree(root string, mutations []ports.ResourceMutation) error {
	if !filepath.IsAbs(root) || len(mutations) == 0 {
		return fmt.Errorf("atomic tree requires an absolute root and members")
	}
	if _, err := os.Lstat(root); err == nil {
		return fmt.Errorf("atomic tree target already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".boatstack-tree-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, mutation := range mutations {
		relative, relErr := filepath.Rel(root, mutation.Path)
		if relErr != nil || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("atomic tree member escapes root")
		}
		target := filepath.Join(stage, relative)
		if err := atomicWrite(target, mutation.Target, os.FileMode(mutation.Mode)); err != nil {
			return err
		}
	}
	if err := syncDirectory(stage); err != nil {
		return err
	}
	if err := os.Rename(stage, root); err != nil {
		return err
	}
	return syncDirectory(parent)
}
