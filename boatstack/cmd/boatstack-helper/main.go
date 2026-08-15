package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/analysis"
	"github.com/operatorstack/boatstack/boatstack/delivery"
	"github.com/operatorstack/boatstack/boatstack/distribution"
	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/delegation"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/surfaces"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type commandOptions struct {
	repository                          string
	format                              string
	objectiveID                         string
	targetID                            string
	trustedObjectiveClass               string
	deliveryID                          string
	programID                           string
	flowProgramFingerprint              string
	activeFlowBound                     bool
	entryID                             string
	runID                               string
	transitionID                        string
	correlationID                       string
	prescriptionID                      string
	expectedInstanceID                  string
	expectedStateRevision               uint64
	expectedProgramFingerprint          string
	expectedSnapshotFingerprint         string
	expectedObjectiveBindingFingerprint string
	authorityFingerprint                string
	requiredCapabilities                stringList
	effectiveCapabilities               stringList
	idempotencyKey                      string
	humanActor                          string
	repositoryPolicy                    bool
	acceptProgramChange                 bool
	parameters                          stringList
	authorityReceipts                   stringList
	trustedAuthorityReceipts            []protocol.AuthorityReceipt
	follow                              bool
	host                                string
	command                             string
	delegationBindingFingerprint        string
	delegationRequestFingerprint        string
	delegationAuthorities               stringList
	delegationDescription               string
	delegationRequest                   delegation.Request
	workInputs                          map[string]string
	workID                              string
	workQuestionPrompt                  string
	workQuestionSchemaPath              string
	workQuestionID                      string
	workAnswerPath                      string
	workBlockReason                     string
	workResultFingerprint               string
}

func main() {
	if boatstackruntime.ShouldDispatch(os.Args[0]) {
		code, err := boatstackruntime.Dispatch(os.Args[1:])
		if err != nil {
			rendered, renderErr := boatstackruntime.RenderBootstrapDiagnostic(os.Stderr, err, os.Args[1:])
			if renderErr != nil {
				fmt.Fprintln(os.Stderr, "boatstack:", renderErr)
			} else if !rendered {
				fmt.Fprintln(os.Stderr, "boatstack:", err)
			}
			os.Exit(1)
		}
		os.Exit(code)
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "boatstack:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return usageError()
	}
	command := arguments[0]
	if command == "version" || command == "--version" {
		fmt.Println(boatstack.Version)
		return nil
	}
	if command == "rpc" {
		return runRPC()
	}
	if command == "retro" {
		return runRetrospective(arguments[1:])
	}
	if command == "flow" {
		return runFlowCommand(arguments[1:])
	}
	operation, transition, defaults, err := classifyCommand(command)
	if err != nil {
		return err
	}
	options, err := parseOptions(command, arguments[1:], transition, defaults)
	if err != nil {
		return err
	}
	options, err = bindFlowEntry(context.Background(), options)
	if err != nil {
		return err
	}
	request, err := buildRequest(operation, options)
	if err != nil {
		return err
	}
	delegationLock, delegationResponse, err := prepareDelegation(context.Background(), &request)
	if err != nil {
		return err
	}
	if delegationLock != nil {
		defer delegationLock.Release()
	}
	if delegationResponse != nil {
		return renderResponse(*delegationResponse, options.format)
	}
	lease, err := acquireFlowExecutionLease(request)
	if err != nil {
		return err
	}
	defer lease.Release()
	kernel, err := standardKernel(context.Background(), request)
	if err != nil {
		return err
	}
	if (operation == surfaces.OperationApply || operation == surfaces.OperationRecover) && request.Prescription.ID == "" && command != "apply" && command != "recover" {
		resolveRequest := request
		resolveRequest.Operation = surfaces.OperationResolve
		if resolveRequest.ProgramID == "" {
			resolveRequest.FlowID = ""
		}
		resolveRequest.Prescription = protocol.Prescription{}
		resolved, resolveErr := kernel.Handle(context.Background(), resolveRequest)
		if resolveErr != nil || resolved.Prescription == nil {
			if renderErr := renderResponse(resolved, options.format); renderErr != nil {
				return renderErr
			}
			if resolveErr == nil {
				if resolved.Decision != nil && resolved.Decision.Reason != "" {
					return errors.New(resolved.Decision.Reason)
				}
				return fmt.Errorf("transition %q was not prescribed", request.TransitionID)
			}
			return resolveErr
		}
		request.Prescription = *resolved.Prescription
	}
	response, handleErr := kernel.Handle(context.Background(), request)
	if operation != surfaces.OperationExplain {
		if settleErr := settleDelegationAtTarget(context.Background(), request, response, kernel.TargetSatisfied(response.Snapshot, request.Objective), delegationLock != nil); settleErr != nil && handleErr == nil {
			handleErr = settleErr
		}
	}
	if command == "events" && options.follow {
		if options.format != "jsonl" {
			return fmt.Errorf("events --follow requires --format jsonl")
		}
		return followEvents(kernel, request)
	}
	if renderErr := renderResponse(response, options.format); renderErr != nil {
		return renderErr
	}
	return handleErr
}

func usageError() error {
	return errors.New("usage: boatstack <status|next|explain|apply|recover|doctor|events|catalog|guard|flow|rpc|retro|init|attach|detach|version> [flags]")
}

func runRPC() error {
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 4<<20))
	decoder.DisallowUnknownFields()
	var request surfaces.Request
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("decode Boatstack RPC request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("Boatstack RPC request contains trailing JSON")
	}
	request, err := bindRPCFlowEntry(context.Background(), request)
	if err != nil {
		return err
	}
	delegationLock, delegationResponse, err := prepareDelegation(context.Background(), &request)
	if err != nil {
		return err
	}
	if delegationLock != nil {
		defer delegationLock.Release()
	}
	if delegationResponse != nil {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(delegationResponse)
	}
	lease, err := acquireFlowExecutionLease(request)
	if err != nil {
		return err
	}
	defer lease.Release()
	kernel, err := standardKernel(context.Background(), request)
	if err != nil {
		return err
	}
	response, handleErr := kernel.Handle(context.Background(), request)
	if request.Operation != surfaces.OperationExplain {
		if settleErr := settleDelegationAtTarget(context.Background(), request, response, kernel.TargetSatisfied(response.Snapshot, request.Objective), delegationLock != nil); settleErr != nil && handleErr == nil {
			handleErr = settleErr
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		return err
	}
	return handleErr
}

func runRetrospective(arguments []string) error {
	flags := flag.NewFlagSet("retro", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	input, format, source := "-", "", "stdin"
	flags.StringVar(&input, "input", input, "transcript path or - for stdin")
	flags.StringVar(&format, "transcript-format", format, "events, claudecode, plaintext, or empty for detection")
	flags.StringVar(&source, "source", source, "privacy-safe source label")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected retro arguments: %s", strings.Join(flags.Args(), " "))
	}
	var content []byte
	var err error
	if input == "-" {
		content, err = io.ReadAll(io.LimitReader(os.Stdin, 16<<20))
	} else {
		content, err = os.ReadFile(input)
		if err == nil && len(content) > 16<<20 {
			return fmt.Errorf("retrospective input exceeds 16 MiB")
		}
	}
	if err != nil {
		return err
	}
	report, err := analysis.DeriveRetrospective(format, source, content)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func classifyCommand(command string) (surfaces.Operation, catalog.TransitionID, map[string]string, error) {
	aliases := map[string]catalog.TransitionID{
		"init": "installation.initialize", "update": "installation.update", "reconcile-update": "installation.reconcile-update", "attach": "repository.attach", "detach": "repository.detach",
		"hydrate-runtime": "runtime.hydrate", "configure": "configuration.mutate", "objective-bind": "objective.bind",
		"plan-create": "plan.create", "plan-validate": "plan.validate", "plan-approve": "plan.approve", "plan-activate": "plan.activate", "plan-amend": "plan.amend",
		"workspace-cut": "workspace.cut", "workspace-sync": "workspace.sync", "workspace-cleanup": "workspace.cleanup", "workspace-reap": "workspace.reap",
		"record-build": "gate.build.record", "record-test": "gate.test.record", "record-review": "gate.review.record", "record-change": "gate.change.record", "record-journey": "gate.journey.record",
		"publication-preview": "publication.preview", "publish-pr": "publication.execute", "observe-pr": "publication.observe", "correct-pr": "publication.correct",
		"abandon": "plan.abandon",
	}
	switch command {
	case "status", "next", "next-status":
		return surfaces.OperationResolve, "", nil, nil
	case "explain":
		return surfaces.OperationExplain, "", nil, nil
	case "apply":
		return surfaces.OperationApply, "", nil, nil
	case "recover":
		return surfaces.OperationRecover, "", nil, nil
	case "doctor":
		return surfaces.OperationDoctor, "", nil, nil
	case "events":
		return surfaces.OperationEvents, "", nil, nil
	case "catalog":
		return surfaces.OperationCatalog, "", nil, nil
	case "guard":
		return surfaces.OperationGuard, "", nil, nil
	}
	if transition, ok := aliases[command]; ok {
		return surfaces.OperationApply, transition, nil, nil
	}
	return "", "", nil, fmt.Errorf("unknown command %q", command)
}

func parseOptions(command string, arguments []string, transition catalog.TransitionID, defaults map[string]string) (commandOptions, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	defaultFormat := "json"
	if command == "explain" {
		defaultFormat = "text"
	}
	options := commandOptions{format: defaultFormat, transitionID: string(transition), host: "cli"}
	if defaults != nil {
		options.targetID, options.deliveryID, options.objectiveID = defaults["target-id"], defaults["delivery"], defaults["objective-id"]
	}
	flags.StringVar(&options.repository, "repo", ".", "explicit invoking repository or worktree")
	flags.StringVar(&options.format, "format", options.format, "json, text, or jsonl")
	flags.StringVar(&options.objectiveID, "objective-id", options.objectiveID, "configured objective identity")
	flags.StringVar(&options.targetID, "target-id", options.targetID, "program-scoped marked target identity")
	flags.StringVar(&options.deliveryID, "delivery", options.deliveryID, "delivery identity")
	flags.StringVar(&options.programID, "flow", "", "repository Control Program identity")
	flags.StringVar(&options.entryID, "entry", "", "named Flow entry")
	flags.StringVar(&options.runID, "run-id", "", "opaque active run identity")
	flags.StringVar(&options.transitionID, "transition", options.transitionID, "stable semantic transition id")
	flags.StringVar(&options.correlationID, "correlation", "", "command-scoped correlation identity from resolution")
	flags.StringVar(&options.prescriptionID, "prescription-id", "", "exact prescription identity from resolution")
	flags.StringVar(&options.expectedInstanceID, "expected-instance-id", "", "exact control instance identity observed during resolution")
	flags.Uint64Var(&options.expectedStateRevision, "expected-state-revision", 0, "exact durable state revision observed during resolution")
	flags.StringVar(&options.expectedProgramFingerprint, "expected-program-fingerprint", "", "exact executable control-program fingerprint observed during resolution")
	flags.StringVar(&options.expectedSnapshotFingerprint, "expected-snapshot-fingerprint", "", "exact admission-relevant snapshot fingerprint observed during resolution")
	flags.StringVar(&options.expectedObjectiveBindingFingerprint, "expected-objective-binding-fingerprint", "", "exact objective binding fingerprint observed during resolution")
	flags.StringVar(&options.authorityFingerprint, "authority-fingerprint", "", "exact authority projection fingerprint from resolution")
	flags.Var(&options.requiredCapabilities, "required-capability", "required capability from resolution (repeatable)")
	flags.Var(&options.effectiveCapabilities, "effective-capability", "effective capability from resolution (repeatable)")
	flags.StringVar(&options.idempotencyKey, "idempotency-key", "", "exact prior admission idempotency key for safe replay")
	flags.StringVar(&options.humanActor, "human", "", "explicit command-scoped human authority actor")
	flags.BoolVar(&options.repositoryPolicy, "repository-authority", false, "derive repository-policy authority from Boatstack project configuration")
	flags.BoolVar(&options.acceptProgramChange, "accept-program-change", false, "explicitly accept the exact prior-to-candidate control-program delta during update")
	flags.Var(&options.parameters, "param", "transition parameter name=value (repeatable)")
	flags.Var(&options.authorityReceipts, "authority-receipt", "authority receipt JSON path (repeatable)")
	flags.BoolVar(&options.follow, "follow", false, "follow passive process events (events with jsonl only)")
	flags.StringVar(&options.host, "host", options.host, "cli, sdk, cursor, codex, claude, gemini, or mcp")
	flags.StringVar(&options.command, "command", "", "raw command to classify at the guard boundary")
	flags.StringVar(&options.workID, "work-id", "", "foreground work contract identity")
	flags.StringVar(&options.workQuestionPrompt, "prompt", "", "bounded foreground work question")
	flags.StringVar(&options.workQuestionSchemaPath, "question-schema", "", "JSON Schema path for a foreground work answer")
	flags.StringVar(&options.workQuestionID, "question-id", "", "exact foreground work question identity")
	flags.StringVar(&options.workAnswerPath, "answer", "", "JSON answer path")
	flags.StringVar(&options.workBlockReason, "reason", "", "foreground work blocker")
	flags.StringVar(&options.workResultFingerprint, "work-result-fingerprint", "", "exact foreground work result from resolution")
	if err := flags.Parse(arguments); err != nil {
		return commandOptions{}, err
	}
	if flags.NArg() != 0 {
		return commandOptions{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	switch command {
	case "init":
		if err := populateInitParameters(&options); err != nil {
			return commandOptions{}, err
		}
	case "update", "reconcile-update", "hydrate-runtime":
		if err := populateRuntimeParameters(&options); err != nil {
			return commandOptions{}, err
		}
		if command == "reconcile-update" || (command == "update" && options.acceptProgramChange) {
			if !options.acceptProgramChange {
				return commandOptions{}, fmt.Errorf("reconcile-update requires explicit --accept-program-change")
			}
			options.transitionID = "installation.reconcile-update"
			options.parameters = append(options.parameters, "accept_obligation_change=true")
		}
	case "correct-pr":
		if err := populateFileFingerprint(&options, "body_path", "body_sha256"); err != nil {
			return commandOptions{}, err
		}
	}
	return options, nil
}

func standardKernel(ctx context.Context, request surfaces.Request) (boatstack.DeliveryController, error) {
	programRequest := distribution.RepositoryProgramRequest{
		Repository: request.Repository, Host: request.Host, CorrelationID: request.CorrelationID,
	}
	if request.TransitionID == "installation.initialize" || request.TransitionID == "configuration.initialize" {
		programRequest.ConfigurationPath, _ = request.Parameters.Get("config_path")
		programRequest.ConfigurationFingerprint, _ = request.Parameters.Get("config_sha256")
	}
	var program delivery.ControlProgram
	var err error
	if request.ProgramID != "" {
		definition, definitionErr := loadFlowDefinition(ctx, request.Repository, request.ProgramID)
		if definitionErr != nil {
			return boatstack.DeliveryController{}, definitionErr
		}
		if definition.Fingerprint() != request.ProgramFingerprint {
			return boatstack.DeliveryController{}, fmt.Errorf("FLOW_PROGRAM_DRIFT: bound fingerprint %q does not match current artifact %q", request.ProgramFingerprint, definition.Fingerprint())
		}
		program, err = distribution.ProgramForRepository(ctx, programRequest, definition)
	} else {
		program, err = distribution.StandardProgramForRepository(ctx, programRequest)
	}
	if err != nil {
		return boatstack.DeliveryController{}, err
	}
	return boatstack.NewDeliveryController("", program)
}

func acquireFlowExecutionLease(request surfaces.Request) (*boatstackruntime.FlowProjectionLease, error) {
	if request.ProgramID == "" || (request.Operation != surfaces.OperationApply && request.Operation != surfaces.OperationRecover) {
		return &boatstackruntime.FlowProjectionLease{}, nil
	}
	return boatstackruntime.AcquireFlowProjectionLease(request.Repository)
}

func followEvents(kernel boatstack.DeliveryController, request surfaces.Request) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	encoder := json.NewEncoder(os.Stdout)
	seen := 0
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		response, err := kernel.Handle(ctx, request)
		if err != nil {
			return err
		}
		if len(response.Events) < seen {
			seen = 0
		}
		for _, event := range response.Events[seen:] {
			if err := encoder.Encode(event); err != nil {
				return err
			}
		}
		seen = len(response.Events)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func populateInitParameters(options *commandOptions) error {
	if err := populateRuntimeParameters(options); err != nil {
		return err
	}
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return err
	}
	if _, ok := parameters.Get("config_path"); !ok {
		return fmt.Errorf("init requires --param config_path=<Boatstack-config.json>")
	}
	configPath, _ := parameters.Get("config_path")
	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if _, ok := parameters.Get("config_sha256"); !ok {
		_, fingerprint, fingerprintErr := protocol.ProjectConfigFingerprint(configRaw)
		if fingerprintErr != nil {
			return fingerprintErr
		}
		options.parameters = append(options.parameters, "config_sha256="+fingerprint)
	}
	if options.humanActor == "" {
		return fmt.Errorf("init requires explicit --human <actor>")
	}
	return nil
}

// populateRuntimeParameters binds runtime transitions to the exact bytes and
// source revision of the process that is requesting admission. Installers may
// still restate the canonical path and hash, but they cannot substitute a
// release tag for the embedded source commit.
func populateRuntimeParameters(options *commandOptions) error {
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return err
	}
	if _, ok := parameters.Get("runtime_version"); !ok {
		options.parameters = append(options.parameters, "runtime_version="+buildinfo.Version)
	}
	if _, ok := parameters.Get("runtime_sha256"); !ok {
		runtimePath, executableErr := os.Executable()
		if executableErr != nil {
			return executableErr
		}
		runtimeRaw, readErr := os.ReadFile(runtimePath)
		if readErr != nil {
			return readErr
		}
		options.parameters = append(options.parameters, "runtime_sha256="+hash(runtimeRaw))
	}
	if _, ok := parameters.Get("source_revision"); !ok {
		options.parameters = append(options.parameters, "source_revision="+buildRevision())
	}
	return nil
}

func populateFileFingerprint(options *commandOptions, pathName, fingerprintName string) error {
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return err
	}
	if _, exists := parameters.Get(fingerprintName); exists {
		return nil
	}
	path, exists := parameters.Get(pathName)
	if !exists {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	options.parameters = append(options.parameters, fingerprintName+"="+hash(raw))
	return nil
}

func buildRevision() string {
	if buildinfo.SourceCommit != "" && buildinfo.SourceCommit != "unknown" {
		return buildinfo.SourceCommit
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return setting.Value
			}
		}
	}
	return boatstack.Version
}

func buildRequest(operation surfaces.Operation, options commandOptions) (surfaces.Request, error) {
	now := time.Now().UTC()
	correlation := options.correlationID
	if correlation == "" {
		correlation = fmt.Sprintf("cli-%d-%d", os.Getpid(), now.UnixNano())
	}
	objective := model.Objective{}
	if options.targetID != "" || options.objectiveID != "" || options.deliveryID != "" {
		objective = model.Objective{ID: options.objectiveID, TargetID: model.TargetID(options.targetID), TrustedClass: model.TargetID(options.trustedObjectiveClass), DeliveryID: options.deliveryID}
		if err := objective.Validate(); err != nil {
			return surfaces.Request{}, err
		}
	}
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return surfaces.Request{}, err
	}
	if options.transitionID == "publication.execute" {
		previewFingerprint, ok := parameters.Get("preview_fingerprint")
		if ok && previewFingerprint != "" {
			receipt, resolveErr := resolveGitHubProviderAuthority(context.Background(), options.repository, previewFingerprint, now)
			if resolveErr != nil {
				return surfaces.Request{}, resolveErr
			}
			options.trustedAuthorityReceipts = append(options.trustedAuthorityReceipts, receipt)
		}
	}
	authority, err := loadAuthority(options, correlation, objective, parameters, now)
	if err != nil {
		return surfaces.Request{}, err
	}
	requiredCapabilities, err := parseCapabilities("--required-capability", options.requiredCapabilities)
	if err != nil {
		return surfaces.Request{}, err
	}
	effectiveCapabilities, err := parseCapabilities("--effective-capability", options.effectiveCapabilities)
	if err != nil {
		return surfaces.Request{}, err
	}
	flowID := options.runID
	if flowID == "" && objective.ID != "" {
		flowID = "flow-" + objective.ID
	}
	if flowID == "" && (operation == surfaces.OperationApply || operation == surfaces.OperationRecover) {
		flowID = "flow-" + correlation
	}
	return surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: operation, Repository: options.repository, Host: options.host, CorrelationID: correlation,
		ProgramID: options.programID, ProgramFingerprint: options.flowProgramFingerprint, EntryID: options.entryID, FlowID: flowID, Objective: objective, TransitionID: catalog.TransitionID(options.transitionID), Authority: authority, Parameters: parameters,
		Prescription: protocol.Prescription{SchemaVersion: protocol.PrescriptionSchemaVersion, ID: options.prescriptionID,
			TransitionID: catalog.TransitionID(options.transitionID), Freshness: general.Freshness{
				ExpectedInstanceID: options.expectedInstanceID, ExpectedStateRevision: options.expectedStateRevision, ExpectedProgramFingerprint: options.expectedProgramFingerprint,
				ExpectedSnapshotFingerprint: options.expectedSnapshotFingerprint, ExpectedObjectiveBindingFingerprint: options.expectedObjectiveBindingFingerprint,
				AuthorityFingerprint: options.authorityFingerprint,
			}, RequiredCapabilities: requiredCapabilities, EffectiveCapabilities: effectiveCapabilities, WorkResultFingerprint: options.workResultFingerprint},
		RepositoryAuthority: options.repositoryPolicy, IdempotencyKey: options.idempotencyKey, Command: options.command,
		DelegationBindingFingerprint: options.delegationBindingFingerprint,
		DelegationRequestFingerprint: options.delegationRequestFingerprint,
		DelegatedAuthorities:         delegationClasses(options.delegationAuthorities),
		WorkInputs:                   options.workInputs,
		WorkID:                       options.workID,
		WorkQuestionPrompt:           options.workQuestionPrompt,
		WorkQuestionID:               options.workQuestionID,
		WorkBlockReason:              options.workBlockReason,
	}, nil
}

func delegationClasses(values []string) []catalog.AuthorityClass {
	result := make([]catalog.AuthorityClass, len(values))
	for index, value := range values {
		result[index] = catalog.AuthorityClass(value)
	}
	return result
}

func parseCapabilities(field string, values []string) ([]catalog.Capability, error) {
	capabilities := make([]catalog.Capability, len(values))
	for index, value := range values {
		capabilities[index] = catalog.Capability(value)
	}
	normalized, err := catalog.NormalizeCapabilities(field, capabilities)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func parseParameters(values []string) (protocol.Parameters, error) {
	parameters := make(protocol.Parameters, 0, len(values))
	for _, value := range values {
		name, parameterValue, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(name) == "" || parameterValue == "" {
			return nil, fmt.Errorf("--param requires name=value")
		}
		name = strings.TrimSpace(name)
		if isPathParameter(name) {
			absolute, err := filepath.Abs(parameterValue)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", name, err)
			}
			parameterValue = filepath.Clean(absolute)
			if name != "destination" {
				resolved, resolveErr := filepath.EvalSymlinks(absolute)
				if resolveErr != nil {
					return nil, fmt.Errorf("resolve %s: %w", name, resolveErr)
				}
				parameterValue = resolved
			}
		}
		parameters = append(parameters, protocol.Parameter{Name: name, Value: parameterValue})
	}
	return parameters.Canonical(), nil
}

func isPathParameter(name string) bool {
	switch name {
	case "source_path", "config_path", "destination", "evidence_path", "manifest_path", "body_path":
		return true
	default:
		return false
	}
}

func loadAuthority(options commandOptions, correlation string, objective model.Objective, parameters protocol.Parameters, now time.Time) (protocol.AuthorityBundle, error) {
	bundle := protocol.AuthorityBundle{}
	for _, path := range options.authorityReceipts {
		raw, err := os.ReadFile(path)
		if err != nil {
			return protocol.AuthorityBundle{}, err
		}
		var receipt protocol.AuthorityReceipt
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&receipt); err != nil {
			return protocol.AuthorityBundle{}, err
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return protocol.AuthorityBundle{}, fmt.Errorf("authority receipt contains trailing JSON")
		}
		if receipt.Class == catalog.AuthorityProvider {
			return protocol.AuthorityBundle{}, fmt.Errorf("PROVIDER_AUTHORITY_UNTRUSTED: external-provider authority must be derived by the trusted provider boundary")
		}
		bundle.Receipts = append(bundle.Receipts, receipt)
	}
	for _, receipt := range options.trustedAuthorityReceipts {
		if receipt.Class != catalog.AuthorityProvider {
			return protocol.AuthorityBundle{}, fmt.Errorf("trusted authority channel accepts only external-provider receipts")
		}
		bundle.Receipts = append(bundle.Receipts, receipt)
	}
	if options.humanActor != "" {
		parameterRaw, err := json.Marshal(parameters.Canonical())
		if err != nil {
			return protocol.AuthorityBundle{}, err
		}
		fingerprint := hash([]byte(strings.Join([]string{correlation, objective.ID, options.transitionID, options.humanActor, string(parameterRaw)}, "\x00")))
		bundle.Receipts = append(bundle.Receipts, protocol.AuthorityReceipt{
			ID: "human-" + fingerprint[:16], Class: catalog.AuthorityHuman, Subject: options.humanActor, Fingerprint: fingerprint,
			IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
		})
	}
	return bundle, nil
}

func renderResponse(response surfaces.Response, format string) error {
	if response.Operation == surfaces.OperationCatalog {
		switch format {
		case "markdown":
			fmt.Print(surfaces.RenderCatalogMarkdown(response.Catalog))
			return nil
		case "mermaid":
			fmt.Print(surfaces.RenderCatalogMermaid(response.Catalog))
			return nil
		case "standard-flow-mermaid":
			fmt.Print(surfaces.RenderStandardFlowMermaid(response.Catalog))
			return nil
		case "locus-safety":
			value, err := surfaces.RenderCatalogLocusSafety(response.Catalog)
			if err != nil {
				return err
			}
			fmt.Print(value)
			return nil
		case "locus-liveness":
			value, err := surfaces.RenderCatalogLocusLiveness(response.Catalog)
			if err != nil {
				return err
			}
			fmt.Print(value)
			return nil
		}
	}
	switch format {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(response)
	case "jsonl":
		encoder := json.NewEncoder(os.Stdout)
		if response.Operation == surfaces.OperationEvents {
			for _, event := range response.Events {
				if err := encoder.Encode(event); err != nil {
					return err
				}
			}
			return nil
		}
		return encoder.Encode(response)
	case "text":
		if response.Operation == surfaces.OperationExplain {
			return renderExplanation(response)
		}
		if response.Error != "" {
			fmt.Println("UNRESOLVED:", response.Error)
			if response.ProgramChange != nil {
				fmt.Printf("program_change prior=%s candidate=%s delta=%s transition=%s accept=%s\n",
					response.ProgramChange.PriorProgramFingerprint, response.ProgramChange.CandidateProgramFingerprint,
					response.ProgramChange.ProgramDeltaFingerprint, response.ProgramChange.RequiredTransition, response.ProgramChange.AcceptanceFlag)
			}
			return nil
		}
		if response.Doctor != nil {
			fmt.Printf("healthy=%t kernel=%s core=%s@%s program=%s@%s core_transitions=%d runtime_transitions=%d extension_transitions=%d transitions=%d fingerprint=%s drift=%t runtime_healthy=%t update_ready=%t recovery_required=%t snapshot=%s\n%s\n",
				response.Doctor.Healthy, response.Doctor.KernelVersion, response.Doctor.CoreSystemID, response.Doctor.CoreSystemVersion,
				response.Doctor.ProgramID, response.Doctor.ProgramVersion, response.Doctor.CoreTransitionCount,
				response.Doctor.RuntimeTransitionCount, response.Doctor.ExtensionTransitionCount, response.Doctor.TransitionCount,
				response.Doctor.ProgramFingerprint, response.Doctor.UnresolvedProgramDrift, response.Doctor.RuntimeHealthy, response.Doctor.UpdateReady, response.Doctor.RecoveryRequired, response.Doctor.Snapshot, response.Doctor.Detail)
			return nil
		}
		if response.Decision != nil {
			fmt.Printf("%s: %s\n", response.Decision.Kind, response.Decision.Reason)
			if response.Decision.Transition != nil {
				fmt.Println("transition:", response.Decision.Transition.ID)
			}
		}
		if response.Prescription != nil {
			correlation := ""
			if response.Snapshot != nil {
				correlation = response.Snapshot.Invocation.Correlation
			}
			fmt.Printf("prescription=%s state_revision=%d program=%s snapshot=%s correlation=%s\n", response.Prescription.ID,
				response.Prescription.ExpectedStateRevision, response.Prescription.ExpectedProgramFingerprint,
				response.Prescription.ExpectedSnapshotFingerprint, correlation)
		}
		if response.Work != nil {
			fmt.Printf("work=%s status=%s revision=%d staging=%s\n", response.Work.Request.Contract.ID, response.Work.Status, response.Work.Revision, response.Work.Request.StagingRoot)
			if response.Work.Question != nil {
				fmt.Printf("question=%s %s\n", response.Work.Question.ID, response.Work.Question.Prompt)
			}
			if response.Work.BlockReason != "" {
				fmt.Println("blocker:", response.Work.BlockReason)
			}
		}
		if response.Receipt != nil {
			fmt.Println("receipt:", response.Receipt.ID)
		}
		if response.Guard != nil {
			fmt.Printf("allowed=%t operation=%s reason=%s\n", response.Guard.Allowed, response.Guard.Intent.Operation, response.Guard.Reason)
			if response.Guard.RequiredTransition != "" {
				fmt.Println("transition:", response.Guard.RequiredTransition)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func renderExplanation(response surfaces.Response) error {
	trace := response.Trace
	if trace == nil {
		if response.Error != "" {
			fmt.Println("UNRESOLVED:", response.Error)
			fmt.Println("No effect was executed.")
			return nil
		}
		return fmt.Errorf("explain response has no decision trace")
	}
	if response.RunID != "" {
		fmt.Println("Run:", response.RunID)
	}
	if response.ProgramID != "" {
		fmt.Println("Flow:", response.ProgramID)
	}
	if response.EntryID != "" {
		fmt.Println("Entry:", response.EntryID)
	}
	if response.Objective.TargetID != "" {
		fmt.Println("Target:", response.Objective.TargetID)
	}
	fmt.Printf("State: revision %d, mode %s\n", trace.StateRevision, trace.CurrentMode)
	fmt.Printf("Decision: %s\n", trace.Decision.Kind)
	if trace.Decision.Reason != "" {
		fmt.Println("Reason:", trace.Decision.Reason)
	}
	primary := trace.Decision.Transition
	if primary == "" && len(trace.Decision.Candidates) != 0 {
		primary = trace.Decision.Candidates[0]
	}
	for _, candidate := range trace.Candidates {
		if candidate.TransitionID != primary {
			continue
		}
		fmt.Println("\nCandidate:", candidate.TransitionID)
		var satisfied []string
		for label, evaluation := range map[string]general.EvaluationTrace{
			"source state": candidate.SourceMode, "recovery compatibility": candidate.RecoveryCompatible,
			"objective": candidate.ObjectiveScope, "objective binding": candidate.ObjectiveMutation,
			"domain predicate": candidate.DomainAdmissible,
		} {
			if evaluation.Evaluated && evaluation.Satisfied {
				satisfied = append(satisfied, label)
			}
		}
		sort.Strings(satisfied)
		if len(satisfied) != 0 {
			fmt.Println("\nSatisfied:")
			for _, value := range satisfied {
				fmt.Println("  " + value)
			}
		}
		if len(candidate.Authority.RequiredAny) != 0 || len(candidate.Authority.RequiredAll) != 0 {
			fmt.Println("\nAuthority:")
			if len(candidate.Authority.RequiredAny) != 0 {
				fmt.Println("  any-of:", renderCapabilities(candidate.Authority.RequiredAny))
			}
			if len(candidate.Authority.RequiredAll) != 0 {
				fmt.Println("  all-of:", renderCapabilities(candidate.Authority.RequiredAll))
			}
		}
		missing := append(append([]general.Capability(nil), candidate.Authority.MissingAll...), candidate.Authority.MissingAny...)
		if len(missing) != 0 {
			fmt.Println("\nMissing:")
			for _, value := range missing {
				fmt.Println("  " + strings.TrimPrefix(string(value), "authority."))
			}
		}
		break
	}
	var others []general.CandidateTrace
	for _, candidate := range trace.Candidates {
		if candidate.TransitionID != primary && candidate.Disposition != general.DispositionIrrelevantToRequest {
			others = append(others, candidate)
		}
	}
	if len(others) != 0 {
		fmt.Println("\nOther candidates:")
		for _, candidate := range others {
			reason := candidateReason(candidate, trace.Decision)
			fmt.Printf("  %s\n    %s: %s\n", candidate.TransitionID, candidate.Disposition, reason)
		}
	}
	fmt.Println("\nNo effect was executed.")
	return nil
}

func candidateReason(candidate general.CandidateTrace, decision general.DecisionTraceValue) string {
	switch candidate.Disposition {
	case general.DispositionAuthorityFrontier:
		return "required authority is missing"
	case general.DispositionAmbiguous:
		return "candidate is part of an unresolved canonical ambiguity"
	case general.DispositionSelected:
		if decision.Reason != "" {
			return decision.Reason
		}
		return "candidate was selected by the canonical relation"
	case general.DispositionExplicitlyRefused:
		if decision.Reason != "" {
			return decision.Reason
		}
		return "candidate was explicitly refused"
	}
	for _, evaluation := range []general.EvaluationTrace{candidate.SourceMode, candidate.RecoveryCompatible, candidate.ObjectiveScope, candidate.ObjectiveMutation, candidate.DomainAdmissible, candidate.Selection} {
		if evaluation.Evaluated && !evaluation.Satisfied && evaluation.Reason != "" {
			return evaluation.Reason
		}
	}
	switch candidate.Disposition {
	case general.DispositionShadowed:
		return "another canonical candidate was preferred"
	default:
		return "candidate was not selected"
	}
}

func renderCapabilities(values []general.Capability) string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.TrimPrefix(string(value), "authority.")
	}
	return strings.Join(result, ", ")
}

func hash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
