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
	"github.com/operatorstack/boatstack/boatstack/internal/hostprojection"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/effects"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/plant"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
)

type hostProjectionManifest struct {
	SchemaVersion                  int               `json:"schema_version"`
	Projections                    []string          `json:"projections"`
	ProjectionSelectionFingerprint string            `json:"projection_selection_fingerprint"`
	Files                          map[string]string `json:"files"`
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
	var projectConfig *protocol.ProjectConfig
	if info, statErr := os.Lstat(filepath.Join(repository, ".boatstack", "project.json")); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: .boatstack/project.json is not a regular file")
		}
		raw, readErr := os.ReadFile(filepath.Join(repository, ".boatstack", "project.json"))
		if readErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, readErr
		}
		config, decodeErr := protocol.DecodeProjectConfig(raw)
		if decodeErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, decodeErr
		}
		projectConfig = &config
		paths[".boatstack/project.json"] = struct{}{}
	} else if os.IsNotExist(statErr) && allowMissingProject {
		absent = append(absent, ".boatstack/project.json")
	} else if statErr != nil {
		return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: .boatstack/project.json is required: %w", statErr)
	}
	runtimeExists := false
	if _, statErr := os.Lstat(filepath.Join(repository, ".boatstack", "runtime.json")); statErr == nil {
		runtimeExists = true
		paths[".boatstack/runtime.json"] = struct{}{}
	} else if os.IsNotExist(statErr) {
		absent = append(absent, ".boatstack/runtime.json")
	} else if !os.IsNotExist(statErr) {
		return boatstackruntime.ControlBundleSnapshot{}, statErr
	}
	manifestPath := filepath.Join(repository, ".boatstack", "host-projections.json")
	if raw, readErr := os.ReadFile(manifestPath); readErr == nil {
		manifest, decodeErr := decodeHostProjectionManifest(raw)
		if decodeErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, decodeErr
		}
		if projectConfig == nil {
			return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: host projections require project configuration")
		}
		selected, selectionErr := projectConfig.ProjectionIDs()
		if selectionErr != nil || !sameProjectionIDs(manifest.Projections, hostprojection.Strings(selected)) {
			return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_STALE: host projection manifest selection does not match project configuration")
		}
		paths[".boatstack/host-projections.json"] = struct{}{}
		for path, expected := range manifest.Files {
			if !hostprojection.ValidMaintenancePath(path) {
				return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: invalid host projection path %q", path)
			}
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
				return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_STALE: host projection %s does not match its manifest", path)
			}
			paths[filepath.ToSlash(path)] = struct{}{}
		}
	} else if !os.IsNotExist(readErr) {
		return boatstackruntime.ControlBundleSnapshot{}, readErr
	} else if projectConfig != nil && runtimeExists && !allowMissingProject {
		return boatstackruntime.ControlBundleSnapshot{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: .boatstack/host-projections.json is required")
	} else {
		absent = append(absent, ".boatstack/host-projections.json")
	}
	artifacts, err := filepath.Glob(filepath.Join(repository, ".boatstack", "flows", "*.flow.ir.json"))
	if err != nil {
		return boatstackruntime.ControlBundleSnapshot{}, err
	}
	sort.Strings(artifacts)
	artifactPaths := make([]string, 0, len(artifacts))
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
		if _, checkErr := checkArtifactForCurrentProject(repository, artifact, resolver); checkErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, checkErr
		}
		relative, relErr := filepath.Rel(repository, artifactPath)
		if relErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, relErr
		}
		relative = filepath.ToSlash(relative)
		artifactPaths = append(artifactPaths, relative)
		paths[relative] = struct{}{}
		paths[artifact.SourcePath] = struct{}{}
		paths[artifact.DependencyLockPath] = struct{}{}
		for path := range artifact.Assets {
			paths[path] = struct{}{}
		}
		for path := range artifact.GeneratedProjections {
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
	return boatstackruntime.NewControlBundleSnapshotWithMemberSets(files, absent, []boatstackruntime.ControlBundleMemberSet{{
		Root: ".boatstack/flows", Suffix: ".flow.ir.json", Paths: artifactPaths,
	}})
}

func decodeHostProjectionManifest(raw []byte) (hostProjectionManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest hostProjectionManifest
	if err := decoder.Decode(&manifest); err != nil {
		return hostProjectionManifest{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: host projection manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return hostProjectionManifest{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: host projection manifest contains trailing data")
	}
	if manifest.SchemaVersion != 2 || manifest.Projections == nil || manifest.Files == nil {
		return hostProjectionManifest{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: host projection manifest is incomplete")
	}
	projections, err := hostprojection.ParseIDs(manifest.Projections)
	if err != nil || !sameProjectionIDs(manifest.Projections, hostprojection.Strings(projections)) {
		return hostProjectionManifest{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: host projection selection is not canonical")
	}
	fingerprint, err := hostprojection.SelectionFingerprint(projections)
	if err != nil || fingerprint != manifest.ProjectionSelectionFingerprint {
		return hostProjectionManifest{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: host projection selection fingerprint mismatch")
	}
	for path, digest := range manifest.Files {
		if !hostprojection.ValidMaintenancePath(path) || !hostprojection.ValidSHA256(digest) {
			return hostProjectionManifest{}, fmt.Errorf("CONTROL_BUNDLE_INVALID: invalid host projection binding")
		}
	}
	return manifest, nil
}

func sameProjectionIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func replaceHostProjectionBundle(repository string, snapshot boatstackruntime.ControlBundleSnapshot, desired map[string][]byte, manifestRaw []byte) (boatstackruntime.ControlBundleSnapshot, error) {
	projected := snapshot
	manifestPath := filepath.Join(repository, ".boatstack", "host-projections.json")
	if priorRaw, err := os.ReadFile(manifestPath); err == nil {
		prior, decodeErr := decodeHostProjectionManifest(priorRaw)
		if decodeErr != nil {
			return boatstackruntime.ControlBundleSnapshot{}, decodeErr
		}
		for path := range prior.Files {
			if _, keep := desired[path]; keep {
				continue
			}
			if hostprojection.IsSharedCheckoutPath(path) {
				referenced, referenceErr := boatstackruntime.SharedFlowProjectionReferenced(repository, path, prior.Files[path])
				if referenceErr != nil {
					return boatstackruntime.ControlBundleSnapshot{}, referenceErr
				}
				if referenced {
					continue
				}
			}
			projected, decodeErr = boatstackruntime.ReplaceControlBundleFileAbsent(projected, path)
			if decodeErr != nil {
				return boatstackruntime.ControlBundleSnapshot{}, decodeErr
			}
		}
	} else if !os.IsNotExist(err) {
		return boatstackruntime.ControlBundleSnapshot{}, err
	}
	var err error
	projected, err = boatstackruntime.ReplaceControlBundleFile(projected, ".boatstack/host-projections.json", manifestRaw)
	if err != nil {
		return boatstackruntime.ControlBundleSnapshot{}, err
	}
	for path, raw := range desired {
		projected, err = boatstackruntime.ReplaceControlBundleFile(projected, path, raw)
		if err != nil {
			return boatstackruntime.ControlBundleSnapshot{}, err
		}
	}
	return projected, nil
}

func validateRepositoryFlowArtifactsForProjections(ctx context.Context, repository string, projections []hostprojection.ID) error {
	artifacts, err := filepath.Glob(filepath.Join(repository, ".boatstack", "flows", "*.flow.ir.json"))
	if err != nil {
		return err
	}
	sort.Strings(artifacts)
	resolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return err
	}
	for _, artifactPath := range artifacts {
		raw, readErr := os.ReadFile(artifactPath)
		if readErr != nil {
			return readErr
		}
		artifact, loadErr := controlprogram.LoadArtifact(bytes.NewReader(raw))
		if loadErr != nil {
			return loadErr
		}
		if _, checkErr := controlprogram.CheckArtifact(repository, artifact, flowCompilerVersion, resolver, projections, generateSoftwareFlowProjections); checkErr != nil {
			relative, _ := filepath.Rel(repository, artifactPath)
			return fmt.Errorf("CONTROL_BUNDLE_TARGET_INVALID: Flow artifact %s does not match candidate project configuration: %w", filepath.ToSlash(relative), checkErr)
		}
	}
	return nil
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
		projections, projectionErr := config.ProjectionIDs()
		if projectionErr != nil {
			return nil, "", projectionErr
		}
		if projectionErr = validateRepositoryFlowArtifactsForProjections(ctx, repository, projections); projectionErr != nil {
			return nil, "", projectionErr
		}
		hostFiles, manifestRaw, projectionErr := effects.ProjectedHostProjectionFiles(projections)
		if projectionErr != nil {
			return nil, "", projectionErr
		}
		projected, projectErr = replaceHostProjectionBundle(repository, projected, hostFiles, manifestRaw)
		if projectErr != nil {
			return nil, "", projectErr
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
		projections, projectionErr := config.ProjectionIDs()
		if projectionErr != nil {
			return nil, "", projectionErr
		}
		hostFiles, manifestRaw, projectionErr := effects.ProjectedHostProjectionFiles(projections)
		if projectionErr != nil {
			return nil, "", projectionErr
		}
		projected, projectionErr := replaceHostProjectionBundle(repository, snapshot, hostFiles, manifestRaw)
		if projectionErr != nil {
			return nil, "", projectionErr
		}
		target = &projected
	case "workspace.cut":
		baseRef, exists := parameters.Get("base_ref")
		if !exists {
			return nil, "", fmt.Errorf("WORKSPACE_CONTROL_BUNDLE_UNCOMMITTED: workspace.cut requires an exact base_ref")
		}
		resolvedRevision, resolveErr := boatstackruntime.ResolveWorkspaceBaseRevision(ctx, repository, baseRef)
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
	request.ControlBundleRevision = ""
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

func verifyTrustedRequestControlBundle(ctx context.Context, request surfaces.Request) error {
	if request.ControlBundle == nil {
		if controlBundleRequired(request.TransitionID) {
			return fmt.Errorf("CONTROL_BUNDLE_REQUIRED: transition %q has no trusted bundle", request.TransitionID)
		}
		return nil
	}
	if request.ControlBundleRevision != "" {
		revision, err := boatstackruntime.ResolveCommitRevision(ctx, request.Repository, "HEAD")
		if err != nil {
			return err
		}
		if revision != request.ControlBundleRevision {
			return &flowCommitRequiredError{
				programID: request.ProgramID, entryID: request.EntryID, runID: request.FlowID,
				revision: revision, controlBundleFingerprint: request.ControlBundleFingerprint, operation: request.Operation,
				cause: fmt.Errorf("CONTROL_BUNDLE_REVISION_DRIFT: expected revision %s", request.ControlBundleRevision),
			}
		}
		if err := boatstackruntime.VerifyControlBundleRevision(ctx, request.Repository, revision, request.ControlBundle.Source); err != nil {
			return &flowCommitRequiredError{
				programID: request.ProgramID, entryID: request.EntryID, runID: request.FlowID,
				revision: revision, controlBundleFingerprint: request.ControlBundleFingerprint, operation: request.Operation, cause: err,
			}
		}
	}
	return boatstackruntime.VerifyControlBundleRoot(request.Repository, request.ControlBundle.Source)
}
