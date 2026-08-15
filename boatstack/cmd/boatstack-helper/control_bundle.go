package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

type hostSkillProjectionManifest struct {
	SchemaVersion int               `json:"schema_version"`
	Files         map[string]string `json:"files"`
}

func buildRepositoryControlBundle(ctx context.Context, repository string) (boatstackruntime.ControlBundleSnapshot, error) {
	return buildRepositoryControlBundleAllowingInitialization(ctx, repository, false)
}

func buildRepositoryControlBundleAllowingInitialization(ctx context.Context, repository string, allowMissingProject bool) (boatstackruntime.ControlBundleSnapshot, error) {
	repository, err := filepath.Abs(repository)
	if err != nil {
		return boatstackruntime.ControlBundleSnapshot{}, err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return boatstackruntime.ControlBundleSnapshot{}, err
	}
	paths := map[string]struct{}{}
	absent := []string{}
	if _, statErr := os.Lstat(filepath.Join(repository, ".boatstack", "project.json")); statErr == nil {
		paths[".boatstack/project.json"] = struct{}{}
	} else if os.IsNotExist(statErr) && allowMissingProject {
		absent = append(absent, ".boatstack/project.json")
	} else if statErr != nil {
		return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: .boatstack/project.json is required: %w", statErr)
	}
	if _, statErr := os.Lstat(filepath.Join(repository, ".boatstack", "runtime.json")); statErr == nil {
		paths[".boatstack/runtime.json"] = struct{}{}
	} else if os.IsNotExist(statErr) {
		absent = append(absent, ".boatstack/runtime.json")
	} else if !os.IsNotExist(statErr) {
		return boatstackruntime.ControlBundleSnapshot{}, statErr
	}
	manifestPath := filepath.Join(repository, ".boatstack", "host-skills.json")
	if raw, readErr := os.ReadFile(manifestPath); readErr == nil {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var manifest hostSkillProjectionManifest
		decodeErr := decoder.Decode(&manifest)
		var trailing any
		trailingErr := decoder.Decode(&trailing)
		if decodeErr != nil || trailingErr != io.EOF || manifest.SchemaVersion != 1 || manifest.Files == nil {
			return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: host-skill manifest is malformed")
		}
		paths[".boatstack/host-skills.json"] = struct{}{}
		for path, expected := range manifest.Files {
			absolute, pathErr := exactRepositoryPath(repository, filepath.FromSlash(path))
			if pathErr != nil {
				return boatstackruntime.ControlBundleSnapshot{}, pathErr
			}
			raw, fileErr := os.ReadFile(absolute)
			if fileErr != nil {
				return boatstackruntime.ControlBundleSnapshot{}, fileErr
			}
			digest := sha256.Sum256(raw)
			if hex.EncodeToString(digest[:]) != expected {
				return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_STALE: host skill %s does not match its manifest", path)
			}
			paths[filepath.ToSlash(path)] = struct{}{}
		}
	} else if !os.IsNotExist(readErr) {
		return boatstackruntime.ControlBundleSnapshot{}, readErr
	} else {
		absent = append(absent, ".boatstack/host-skills.json")
	}
	artifacts, err := filepath.Glob(filepath.Join(repository, ".boatstack", "flows", "*.flow.ir.json"))
	if err != nil {
		return boatstackruntime.ControlBundleSnapshot{}, err
	}
	sort.Strings(artifacts)
	resolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return boatstackruntime.ControlBundleSnapshot{}, err
	}
	for _, artifactPath := range artifacts {
		raw, readErr := os.ReadFile(artifactPath)
		if readErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, readErr
		}
		artifact, loadErr := controlprogram.LoadArtifact(bytes.NewReader(raw))
		if loadErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, loadErr
		}
		if _, checkErr := controlprogram.CheckArtifact(repository, artifact, flowCompilerVersion, resolver, generateSoftwareFlowSkills); checkErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, checkErr
		}
		relative, relErr := filepath.Rel(repository, artifactPath)
		if relErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, relErr
		}
		paths[filepath.ToSlash(relative)] = struct{}{}
		paths[artifact.SourcePath] = struct{}{}
		paths[artifact.DependencyLockPath] = struct{}{}
		for path := range artifact.Assets {
			paths[path] = struct{}{}
		}
		for path := range artifact.GeneratedSkills {
			paths[path] = struct{}{}
		}
	}
	files := make(map[string][]byte, len(paths))
	for path := range paths {
		absolute, pathErr := exactRepositoryPath(repository, filepath.FromSlash(path))
		if pathErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, pathErr
		}
		info, statErr := os.Lstat(absolute)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: %s is not a regular file", path)
		}
		raw, readErr := os.ReadFile(absolute)
		if readErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, readErr
		}
		files[filepath.ToSlash(path)] = raw
	}
	return boatstackruntime.NewControlBundleSnapshotWithAbsent(files, absent)
}

func controlBundleRequired(id catalog.TransitionID) bool {
	switch id {
	case "runtime.hydrate", "runtime.replace", "runtime.reconcile", "installation.initialize", "installation.update", "installation.reconcile-update", "catalog.reconcile",
		"workspace.cut", "workspace.cleanup", "workspace.reap", "workspace.reconcile":
		return true
	default:
		return false
	}
}

func bindControlBundle(ctx context.Context, repository string, transitionID catalog.TransitionID, parameters protocol.Parameters) (*boatstackruntime.ControlBundleContract, string, error) {
	snapshot, err := buildRepositoryControlBundleAllowingInitialization(ctx, repository, transitionID == "installation.initialize")
	if err != nil {
		return nil, "", err
	}
	var sourcePin *boatstackruntime.Pin
	if pinRaw, readErr := os.ReadFile(boatstackruntime.PinPath(repository)); readErr == nil {
		pin, decodeErr := boatstackruntime.DecodePin(pinRaw)
		if decodeErr != nil {
			return nil, "", fmt.Errorf("CONTROL_BUNDLE_INVALID: decode runtime pin: %w", decodeErr)
		}
		sourcePin = &pin
	} else if !os.IsNotExist(readErr) {
		return nil, "", readErr
	}
	if !controlBundleRequired(transitionID) {
		contract, contractErr := boatstackruntime.NewControlBundleContractWithPins(snapshot, nil, "", sourcePin, nil)
		return &contract, snapshot.Fingerprint, contractErr
	}
	var target *boatstackruntime.ControlBundleSnapshot
	targetRevision := ""
	switch transitionID {
	case "installation.initialize":
		configPath, ok := parameters.Get("config_path")
		if !ok {
			return nil, "", fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: installation.initialize requires config_path")
		}
		configInfo, statErr := os.Lstat(configPath)
		if statErr != nil || configInfo.Mode()&os.ModeSymlink != 0 || !configInfo.Mode().IsRegular() {
			return nil, "", fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: project configuration is not a regular file")
		}
		configRaw, readErr := os.ReadFile(configPath)
		if readErr != nil {
			return nil, "", fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: read project configuration: %w", readErr)
		}
		projected, projectErr := boatstackruntime.ReplaceControlBundleFile(snapshot, ".boatstack/project.json", configRaw)
		if projectErr != nil {
			return nil, "", projectErr
		}
		config, decodeErr := protocol.DecodeProjectConfig(configRaw)
		if decodeErr != nil {
			return nil, "", fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: decode project configuration: %w", decodeErr)
		}
		_, configFingerprint, fingerprintErr := protocol.ProjectConfigFingerprint(configRaw)
		if fingerprintErr != nil {
			return nil, "", fingerprintErr
		}
		if expected, exists := parameters.Get("config_sha256"); !exists || expected != configFingerprint {
			return nil, "", fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: project configuration fingerprint changed")
		}
		hostFiles, manifestRaw, projectionErr := effects.ProjectedHostSkillFiles(config.Hosts)
		if projectionErr != nil {
			return nil, "", projectionErr
		}
		projected, projectErr = boatstackruntime.ReplaceControlBundleFile(projected, ".boatstack/host-skills.json", manifestRaw)
		if projectErr != nil {
			return nil, "", projectErr
		}
		for path, raw := range hostFiles {
			projected, projectErr = boatstackruntime.ReplaceControlBundleFile(projected, path, raw)
			if projectErr != nil {
				return nil, "", projectErr
			}
		}
		target = &projected
	case "installation.update", "installation.reconcile-update":
		configRaw, readErr := os.ReadFile(filepath.Join(repository, ".boatstack", "project.json"))
		if readErr != nil {
			return nil, "", fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: read project configuration: %w", readErr)
		}
		config, decodeErr := protocol.DecodeProjectConfig(configRaw)
		if decodeErr != nil {
			return nil, "", decodeErr
		}
		hostFiles, manifestRaw, projectionErr := effects.ProjectedHostSkillFiles(config.Hosts)
		if projectionErr != nil {
			return nil, "", projectionErr
		}
		projected, projectionErr := boatstackruntime.ReplaceControlBundleFile(snapshot, ".boatstack/host-skills.json", manifestRaw)
		if projectionErr != nil {
			return nil, "", projectionErr
		}
		for path, raw := range hostFiles {
			projected, projectionErr = boatstackruntime.ReplaceControlBundleFile(projected, path, raw)
			if projectionErr != nil {
				return nil, "", projectionErr
			}
		}
		target = &projected
	case "workspace.cut":
		baseRef, exists := parameters.Get("base_ref")
		if !exists {
			return nil, "", fmt.Errorf("WORKSPACE_CONTROL_BUNDLE_UNCOMMITTED: workspace.cut requires an exact base_ref")
		}
		resolvedRevision, resolveErr := boatstackruntime.ResolveCommitRevision(ctx, repository, baseRef)
		if resolveErr != nil {
			return nil, "", fmt.Errorf("WORKSPACE_CONTROL_BUNDLE_UNCOMMITTED: resolve base_ref %q: %w", baseRef, resolveErr)
		}
		targetRevision = resolvedRevision
		if verifyErr := boatstackruntime.VerifyControlBundleRevision(ctx, repository, targetRevision, snapshot); verifyErr != nil {
			return nil, "", fmt.Errorf("WORKSPACE_CONTROL_BUNDLE_UNCOMMITTED: %w", verifyErr)
		}
		copy := snapshot
		copy.Files = append([]boatstackruntime.ControlBundleFile(nil), snapshot.Files...)
		target = &copy
	case "workspace.cleanup", "workspace.reap":
		copy := snapshot
		copy.Files = append([]boatstackruntime.ControlBundleFile(nil), snapshot.Files...)
		target = &copy
	case "workspace.reconcile":
		transactionID, ok := parameters.Get("transaction_id")
		if !ok {
			return nil, "", fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: workspace.reconcile requires transaction_id")
		}
		resolver, resolverErr := plant.NewResolver("")
		if resolverErr != nil {
			return nil, "", resolverErr
		}
		invocation, invocationErr := resolver.ResolveInvocation(ctx, repository, "cli", "workspace-control-bundle-recovery")
		if invocationErr != nil {
			return nil, "", invocationErr
		}
		layout, _, layoutErr := resolver.ResolveLayout(ctx, invocation)
		if layoutErr != nil {
			return nil, "", layoutErr
		}
		recoveredTarget, recoveredRevision, recoveryErr := effects.InterruptedWorkspaceTarget(layout, transactionID)
		if recoveryErr != nil {
			return nil, "", recoveryErr
		}
		target, targetRevision = &recoveredTarget, recoveredRevision
	}
	var targetPin *boatstackruntime.Pin
	if target != nil {
		targetPin = sourcePin
	}
	contract, err := boatstackruntime.NewControlBundleContractWithPins(snapshot, target, targetRevision, sourcePin, targetPin)
	return &contract, snapshot.Fingerprint, err
}

func bindTrustedRequestControlBundle(ctx context.Context, request *surfaces.Request) error {
	if request == nil {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: request is nil")
	}
	request.ControlBundle = nil
	request.ControlBundleFingerprint = ""
	if request.ProgramID == "" && !controlBundleRequired(request.TransitionID) {
		return nil
	}
	bundle, fingerprint, err := bindControlBundle(ctx, request.Repository, request.TransitionID, request.Parameters)
	if err != nil {
		return err
	}
	request.ControlBundle = bundle
	request.ControlBundleFingerprint = fingerprint
	return nil
}

func verifyTrustedRequestControlBundle(request surfaces.Request) error {
	if request.ControlBundle == nil {
		if controlBundleRequired(request.TransitionID) {
			return fmt.Errorf("CONTROL_BUNDLE_REQUIRED: transition %q has no trusted bundle", request.TransitionID)
		}
		return nil
	}
	return boatstackruntime.VerifyControlBundleRoot(request.Repository, request.ControlBundle.Source)
}
