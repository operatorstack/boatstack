package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/core"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
)

const flowCompilerVersion = "control-program.compiler.5"

type flowCommandOptions struct {
	repository string
	source     string
	artifact   string
	lock       string
	frontend   string
}

func runFlowCommand(arguments []string) error {
	if len(arguments) == 0 {
		return fmt.Errorf("usage: boatstack flow <compile|check|authorize|revoke|run|work|input> [flags]")
	}
	action := arguments[0]
	if action == "authorize" {
		return runFlowAuthorize(arguments[1:])
	}
	if action == "revoke" {
		return runFlowRevoke(arguments[1:])
	}
	if action == "run" {
		return runFlowContinuation(arguments[1:])
	}
	if action == "work" {
		return runFlowWork(arguments[1:])
	}
	if action == "input" {
		return runFlowInput(arguments[1:])
	}
	flags := flag.NewFlagSet("flow "+action, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := flowCommandOptions{}
	flags.StringVar(&options.repository, "repo", ".", "repository containing .boatstack/flows")
	flags.StringVar(&options.source, "source", "", "Flow TypeScript source path")
	flags.StringVar(&options.artifact, "artifact", "", "compiled Flow artifact path")
	flags.StringVar(&options.lock, "lock", "package-lock.json", "frontend dependency lock path")
	flags.StringVar(&options.frontend, "frontend", "", "exact boatstack-flow-frontend executable path")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected flow arguments: %s", strings.Join(flags.Args(), " "))
	}
	repository, err := filepath.Abs(options.repository)
	if err != nil {
		return err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return err
	}
	options.repository = repository
	switch action {
	case "compile":
		return compileFlow(context.Background(), options)
	case "check":
		return checkFlow(context.Background(), options)
	default:
		return fmt.Errorf("unknown flow action %q", action)
	}
}

func compileFlow(ctx context.Context, options flowCommandOptions) error {
	source, err := resolveFlowSource(options.repository, options.source)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(source, ".flow.ts") {
		return fmt.Errorf("Flow source must end with .flow.ts")
	}
	lockPath, err := exactRepositoryPath(options.repository, options.lock)
	if err != nil {
		return err
	}
	frontend, err := resolveFrontend(options.repository, options.frontend)
	if err != nil {
		return err
	}
	sourceRaw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	lockRaw, err := os.ReadFile(lockPath)
	if err != nil {
		return err
	}
	rawIR, err := boatstackruntime.RunFlowFrontend(ctx, frontend, source, sourceRaw)
	if err != nil {
		return err
	}
	if err := requireUnchangedCompileInput(source, sourceRaw); err != nil {
		return err
	}
	if err := requireUnchangedCompileInput(lockPath, lockRaw); err != nil {
		return err
	}
	resolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return err
	}
	compiled, err := controlprogram.LoadWithAssets(bytes.NewReader(rawIR), resolver, controlprogram.RepositoryAssetResolver{Repository: options.repository})
	if err != nil {
		return err
	}
	if err := validateCompiledFlow(ctx, options.repository, compiled, resolver); err != nil {
		return err
	}
	artifactPath, err := resolveArtifactPath(options.repository, options.artifact, compiled.Document.Program.ID)
	if err != nil {
		return err
	}
	skills, err := softwareflow.GenerateSkills(compiled, []string{"codex", "claude"})
	if err != nil {
		return err
	}
	sourceRelative, _ := filepath.Rel(options.repository, source)
	lockRelative, _ := filepath.Rel(options.repository, lockPath)
	artifact, artifactRaw, err := controlprogram.NewArtifact(compiled, controlprogram.ArtifactInput{
		CompilerVersion: flowCompilerVersion, SourcePath: filepath.ToSlash(sourceRelative), Source: sourceRaw,
		DependencyLockPath: filepath.ToSlash(lockRelative), DependencyLock: lockRaw, GeneratedSkills: skills,
	})
	if err != nil {
		return err
	}
	removals, artifactPrevious, priorSkills, ownership, err := ownedProjectionChanges(options.repository, filepath.ToSlash(sourceRelative), artifactPath, artifact.GeneratedSkills)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(skills))
	for path := range skills {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	writes := make([]boatstackruntime.ProjectionWrite, 0, len(paths)+1)
	for _, path := range paths {
		absolute, pathErr := exactRepositoryPath(options.repository, path)
		if pathErr != nil {
			return pathErr
		}
		writes = append(writes, boatstackruntime.ProjectionWrite{
			Path: absolute, Content: skills[path], Mode: 0o644, ExpectedPreviousSHA256: priorSkills[path],
		})
	}
	writes = append(writes, boatstackruntime.ProjectionWrite{
		Path: artifactPath, Content: artifactRaw, Mode: 0o644, ExpectedPreviousSHA256: artifactPrevious, PublishLast: true,
	})
	expectations := []boatstackruntime.ProjectionExpectation{
		{Path: source, Exists: true, ExpectedSHA256: fileDigest(sourceRaw)},
		{Path: lockPath, Exists: true, ExpectedSHA256: fileDigest(lockRaw)},
	}
	compileInputs := []string{source, lockPath}
	assetPaths := make([]string, 0, len(artifact.Assets))
	for relative := range artifact.Assets {
		assetPaths = append(assetPaths, relative)
	}
	sort.Strings(assetPaths)
	for _, relative := range assetPaths {
		absolute, pathErr := exactRepositoryPath(options.repository, relative)
		if pathErr != nil {
			return pathErr
		}
		compileInputs = append(compileInputs, absolute)
		expectations = append(expectations, boatstackruntime.ProjectionExpectation{
			Path: absolute, Exists: true, ExpectedSHA256: artifact.Assets[relative],
		})
	}
	if err := rejectProjectionInputOverlap(compileInputs, writes, removals); err != nil {
		return err
	}
	artifactRelative, _ := filepath.Rel(options.repository, artifactPath)
	nextOwnership := boatstackruntime.NewFlowProjectionOwnership(filepath.ToSlash(sourceRelative), filepath.ToSlash(artifactRelative), artifactRaw, skills)
	if err := boatstackruntime.ApplyOwnedFlowProjection(options.repository, writes, removals, expectations, ownership, nextOwnership); err != nil {
		return err
	}
	return renderFlowResult("compiled", artifactPath, artifact)
}

func rejectProjectionInputOverlap(inputs []string, writes []boatstackruntime.ProjectionWrite, removals []boatstackruntime.ProjectionRemoval) error {
	bound := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		bound[filepath.Clean(input)] = true
	}
	for _, write := range writes {
		if bound[filepath.Clean(write.Path)] {
			return fmt.Errorf("FLOW_COMPILE_INPUT_OVERLAP: compile input %s is a projection output", write.Path)
		}
	}
	for _, removal := range removals {
		if bound[filepath.Clean(removal.Path)] {
			return fmt.Errorf("FLOW_COMPILE_INPUT_OVERLAP: compile input %s is a retired projection output", removal.Path)
		}
	}
	return nil
}

func requireUnchangedCompileInput(path string, expected []byte) error {
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, expected) {
		return fmt.Errorf("FLOW_COMPILE_INPUT_CHANGED: %s changed while the frontend was running", path)
	}
	return nil
}

func ownedProjectionChanges(repository, sourceRelative, artifactPath string, next map[string]string) ([]boatstackruntime.ProjectionRemoval, string, map[string]string, boatstackruntime.FlowProjectionOwnershipSnapshot, error) {
	ownership, err := boatstackruntime.LoadFlowProjectionOwnership(repository, sourceRelative)
	if err != nil || !ownership.Exists() {
		return nil, "", map[string]string{}, ownership, err
	}
	prior := ownership.Record
	retired := make([]boatstackruntime.ProjectionRemoval, 0, len(prior.GeneratedSkills)+1)
	for relative, expected := range prior.GeneratedSkills {
		if _, retained := next[relative]; retained {
			continue
		}
		path, pathErr := exactRepositoryPath(repository, relative)
		if pathErr != nil {
			return nil, "", nil, ownership, pathErr
		}
		retired = append(retired, boatstackruntime.ProjectionRemoval{Path: path, ExpectedSHA256: expected, AllowMissing: true})
	}
	artifactRelative, _ := filepath.Rel(repository, artifactPath)
	artifactPrevious := ""
	if filepath.ToSlash(artifactRelative) == prior.ArtifactPath {
		artifactPrevious = prior.ArtifactSHA256
	} else {
		priorArtifact, pathErr := exactRepositoryPath(repository, prior.ArtifactPath)
		if pathErr != nil {
			return nil, "", nil, ownership, pathErr
		}
		retired = append(retired, boatstackruntime.ProjectionRemoval{Path: priorArtifact, ExpectedSHA256: prior.ArtifactSHA256, AllowMissing: true})
	}
	sort.Slice(retired, func(i, j int) bool { return retired[i].Path < retired[j].Path })
	return retired, artifactPrevious, prior.GeneratedSkills, ownership, nil
}

func fileDigest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func checkFlow(ctx context.Context, options flowCommandOptions) error {
	artifactPath, err := resolveCheckArtifact(options.repository, options.artifact)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		return err
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	resolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return err
	}
	compiled, err := controlprogram.CheckArtifact(options.repository, artifact, flowCompilerVersion, resolver, generateSoftwareFlowSkills)
	if err != nil {
		return err
	}
	if err := validateCompiledFlow(ctx, options.repository, compiled, resolver); err != nil {
		return err
	}
	return renderFlowResult("valid", artifactPath, artifact)
}

func validateCompiledFlow(ctx context.Context, repository string, compiled controlprogram.Compiled, resolver softwareflow.Resolver) error {
	declarative, err := declarativeFlow(compiled.Document)
	if err != nil {
		return fmt.Errorf("FLOW_RUNTIME_INVALID: %w", err)
	}
	if declarative {
		return validateDeclarativeFlow(compiled)
	}
	return validateSoftwareFlow(ctx, repository, compiled, resolver)
}

func declarativeFlow(document controlprogram.Document) (bool, error) {
	inline, bound := 0, 0
	for _, operator := range document.Operators {
		if operator.Binding == nil {
			inline++
		} else {
			bound++
		}
	}
	if inline != 0 && bound != 0 {
		return false, fmt.Errorf("a Flow cannot mix inline and adapter-bound operators")
	}
	return inline != 0, nil
}

// validateDeclarativeFlow admits the smallest domain-neutral executable
// adapter: repository-independent, assignment-only state transitions. The
// generic Control Program compiler remains broader; a domain adapter must
// explicitly own every additional producer or effect mechanism.
func validateDeclarativeFlow(compiled controlprogram.Compiled) error {
	const stateVerifier = "state-effect"
	facets := make(map[string]controlprogram.Facet, len(compiled.Document.Facets))
	for _, facet := range compiled.Document.Facets {
		facets[facet.ID] = facet
	}
	for _, entry := range compiled.Document.Entries {
		if entry.Delegation != nil || entry.Diagnostics != nil {
			return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative entries do not support delegation or domain diagnostics")
		}
	}
	for _, work := range compiled.Document.Work {
		return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative runtime has no foreground-work adapter for %q", work.ID)
	}
	for _, operator := range compiled.Document.Operators {
		if len(operator.Capabilities) != 0 || len(operator.Effects) != 0 || operator.Verifier != stateVerifier || operator.Recovery != "" || operator.StateEffect == nil || operator.StateEffect.Kind != "assignments" || operator.ExecutionContext != "preserve" {
			return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative operator %q must be effect-free, assignment-only, and preserve context", operator.ID)
		}
		if !declarativeAuthoritySupported(operator.Authority.AnyOf) || !declarativeAuthoritySupported(operator.Authority.AllOf) {
			return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative operator %q uses unsupported authority", operator.ID)
		}
		for _, parameter := range operator.Parameters {
			if !declarativeAuthoritySupported(parameter.Authority.AnyOf) || !declarativeAuthoritySupported(parameter.Authority.AllOf) {
				return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative operator %q parameter %q uses unsupported authority", operator.ID, parameter.ID)
			}
		}
	}
	for _, transition := range compiled.Document.Transitions {
		if !declarativeAuthoritySupported(transition.Requires.Authorities) {
			return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative transition %q uses unsupported authority", transition.ID)
		}
		operator, ok := findCompiledOperator(compiled.Document.Operators, transition.Operator)
		if !ok || operator.StateEffect == nil {
			return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative transition %q has no executable operator", transition.ID)
		}
		contracts := make(map[string]controlprogram.OperatorParameter, len(operator.Parameters))
		bindings := make(map[string]bool, len(transition.Parameters))
		for _, contract := range operator.Parameters {
			contracts[contract.ID] = contract
		}
		for _, binding := range transition.Parameters {
			bindings[binding.Parameter] = true
			switch binding.Producer.Kind {
			case controlprogram.ParameterSourceEntryInput, controlprogram.ParameterSourceState, controlprogram.ParameterSourceHostInput:
				if binding.Producer.Kind == controlprogram.ParameterSourceHostInput && (binding.Producer.Request == nil || !declarativeAuthoritySupported(binding.Producer.Request.Authorities)) {
					return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative transition %q host-input parameter %q uses unsupported authority", transition.ID, binding.Parameter)
				}
			default:
				return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative transition %q requires an adapter for producer %q", transition.ID, binding.Producer.Kind)
			}
		}
		for _, precondition := range operator.StateEffect.Preconditions {
			if !predicateRequiresFacetValue(transition.Guard, precondition.Facet, precondition.Values) {
				return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative transition %q guard does not establish precondition %q", transition.ID, precondition.Facet)
			}
		}
		for _, assignment := range operator.StateEffect.Assignments {
			facet := facets[assignment.Facet]
			if assignment.Value != nil {
				if facet.Kind == "boolean" && *assignment.Value != "true" && *assignment.Value != "false" {
					return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative assignment %q has a value outside the facet type", assignment.Facet)
				}
				continue
			}
			if assignment.ValueFrom == nil {
				continue
			}
			if assignment.ValueFrom.Parameter == "" || assignment.ValueFrom.Admission != "" || assignment.ValueFrom.Invocation != "" {
				return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative assignment %q has an unsupported value source", assignment.Facet)
			}
			contract, declared := contracts[assignment.ValueFrom.Parameter]
			if !declared || !bindings[assignment.ValueFrom.Parameter] {
				return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative assignment %q requires bound operator parameter %q", assignment.Facet, assignment.ValueFrom.Parameter)
			}
			if facet.Kind == "enum" || contract.Type.Kind != facet.Kind {
				return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative assignment %q cannot prove parameter %q belongs to the facet", assignment.Facet, assignment.ValueFrom.Parameter)
			}
		}
		if !assignmentsEstablishPredicate(*operator.StateEffect, transition.Guard, transition.Target) {
			return fmt.Errorf("FLOW_RUNTIME_INVALID: declarative transition %q assignments do not establish its target", transition.ID)
		}
	}
	return nil
}

func declarativeAuthoritySupported(authorities []string) bool {
	for _, authority := range authorities {
		if authority != "human" {
			return false
		}
	}
	return true
}

// predicateRequiresFacetValue is intentionally conservative. It accepts only
// facts whose truth is structurally required by every path through a guard.
func predicateRequiresFacetValue(predicate controlprogram.Predicate, facet string, values []string) bool {
	if predicate.Fact != nil {
		fact := predicate.Fact
		if fact.Facet != facet || len(fact.Values) == 0 {
			return false
		}
		for _, value := range fact.Values {
			if !containsString(values, value) {
				return false
			}
		}
		return true
	}
	if len(predicate.All) != 0 {
		for _, child := range predicate.All {
			if predicateRequiresFacetValue(child, facet, values) {
				return true
			}
		}
		return false
	}
	if len(predicate.Any) != 0 {
		for _, child := range predicate.Any {
			if !predicateRequiresFacetValue(child, facet, values) {
				return false
			}
		}
		return true
	}
	return false
}

func assignmentsEstablishPredicate(effect controlprogram.StateEffect, guard, target controlprogram.Predicate) bool {
	assigned := make(map[string]string, len(effect.Assignments))
	mutated := make(map[string]bool, len(effect.Assignments))
	for _, assignment := range effect.Assignments {
		mutated[assignment.Facet] = true
		if assignment.Value != nil {
			assigned[assignment.Facet] = *assignment.Value
		}
	}
	var established func(controlprogram.Predicate) bool
	established = func(predicate controlprogram.Predicate) bool {
		if predicate.True != nil {
			return *predicate.True
		}
		if predicate.Fact != nil {
			fact := predicate.Fact
			if value, ok := assigned[fact.Facet]; ok {
				if len(fact.Statuses) != 0 && !containsString(fact.Statuses, "known") {
					return false
				}
				return len(fact.Values) == 0 || containsString(fact.Values, value)
			}
			if mutated[fact.Facet] {
				return false
			}
			return predicateRequiresFacetValue(guard, fact.Facet, fact.Values)
		}
		if len(predicate.All) != 0 {
			for _, child := range predicate.All {
				if !established(child) {
					return false
				}
			}
			return true
		}
		if len(predicate.Any) != 0 {
			for _, child := range predicate.Any {
				if established(child) {
					return true
				}
			}
		}
		return false
	}
	return established(target)
}

func validateSoftwareFlow(ctx context.Context, _ string, compiled controlprogram.Compiled, resolver softwareflow.Resolver) error {
	for _, flowEntry := range compiled.Document.Entries {
		if _, err := softwareflow.PlanInboxForEntry(flowEntry); err != nil {
			return fmt.Errorf("FLOW_RUNTIME_INVALID: %w", err)
		}
	}
	definition, err := softwareflow.NewDefinition(compiled, resolver)
	if err != nil {
		return fmt.Errorf("FLOW_RUNTIME_INVALID: %w", err)
	}
	if _, err := delivery.Compile(ctx, delivery.CompileRequest{KernelVersion: boatstack.Version, Core: core.System(), Runtime: definition, Settings: map[string]string{"validation": "flow"}}); err != nil {
		return fmt.Errorf("FLOW_RUNTIME_INVALID: %w", err)
	}
	return nil
}

func resolveFlowSource(repository, requested string) (string, error) {
	if requested != "" {
		return exactRepositoryPath(repository, requested)
	}
	matches, err := filepath.Glob(filepath.Join(repository, ".boatstack", "flows", "*.flow.ts"))
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("FLOW_SOURCE_SELECTION_REQUIRED: found %d Flow sources", len(matches))
	}
	return filepath.Clean(matches[0]), nil
}

func resolveArtifactPath(repository, requested, programID string) (string, error) {
	expected, err := exactRepositoryPath(repository, filepath.Join(".boatstack", "flows", programID+".flow.ir.json"))
	if err != nil {
		return "", err
	}
	if requested == "" {
		return expected, nil
	}
	actual, err := exactRepositoryPath(repository, requested)
	if err != nil {
		return "", err
	}
	if actual != expected {
		return "", fmt.Errorf("FLOW_ARTIFACT_ID_MISMATCH: program %s must compile to %s", programID, expected)
	}
	return actual, nil
}

func resolveCheckArtifact(repository, requested string) (string, error) {
	if requested != "" {
		return exactRepositoryPath(repository, requested)
	}
	matches, err := filepath.Glob(filepath.Join(repository, ".boatstack", "flows", "*.flow.ir.json"))
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("FLOW_ARTIFACT_SELECTION_REQUIRED: found %d Flow artifacts", len(matches))
	}
	return filepath.Clean(matches[0]), nil
}

func resolveFrontend(_ string, requested string) (string, error) {
	if requested == "" {
		return "", fmt.Errorf("FLOW_FRONTEND_REQUIRED: pass an explicitly authorized absolute --frontend path")
	}
	if !filepath.IsAbs(requested) || filepath.Clean(requested) != requested {
		return "", fmt.Errorf("--frontend must be exact and absolute")
	}
	info, err := os.Stat(requested)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("FLOW_FRONTEND_REQUIRED: explicit frontend is unavailable")
	}
	return requested, nil
}

func exactRepositoryPath(repository, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("repository path must be non-empty and relative")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("repository path escapes the repository")
	}
	absolute := filepath.Join(repository, clean)
	rel, err := filepath.Rel(repository, absolute)
	if err != nil || rel != clean {
		return "", fmt.Errorf("repository path is not canonical")
	}
	return absolute, nil
}

func renderFlowResult(status, artifactPath string, artifact controlprogram.Artifact) error {
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"status": status, "program_id": artifact.Program.Program.ID, "program_fingerprint": artifact.ProgramFingerprint,
		"artifact": artifactPath, "entries": entryIDs(artifact.Program.Entries),
	})
}

func entryIDs(entries []controlprogram.Entry) []string {
	result := make([]string, len(entries))
	for index, entry := range entries {
		result[index] = entry.ID
	}
	sort.Strings(result)
	return result
}
