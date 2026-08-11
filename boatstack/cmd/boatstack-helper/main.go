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
	"strings"
	"time"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/analysis"
	"github.com/operatorstack/boatstack/boatstack/distribution"
	"github.com/operatorstack/boatstack/boatstack/internal/buildinfo"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/supervisor"
	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/surfaces"
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type commandOptions struct {
	repository          string
	format              string
	goalID              string
	goalKind            string
	deliveryID          string
	flowID              string
	transitionID        string
	idempotencyKey      string
	humanActor          string
	repositoryPolicy    bool
	acceptProgramChange bool
	parameters          stringList
	authorityReceipts   stringList
	follow              bool
	host                string
	command             string
}

func main() {
	if boatstackruntime.ShouldDispatch(os.Args[0]) {
		code, err := boatstackruntime.Dispatch(os.Args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "boatstack:", err)
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
	operation, transition, defaults, err := classifyCommand(command)
	if err != nil {
		return err
	}
	options, err := parseOptions(command, arguments[1:], transition, defaults)
	if err != nil {
		return err
	}
	request, err := buildRequest(operation, options)
	if err != nil {
		return err
	}
	kernel, err := standardKernel(context.Background(), request)
	if err != nil {
		return err
	}
	response, handleErr := kernel.Handle(context.Background(), request)
	if command == "update" && options.acceptProgramChange && handleErr != nil && response.ProgramChange != nil && response.Decision != nil &&
		response.Decision.Kind == supervisor.DecisionUnresolved && response.Decision.Reason == supervisor.ReasonProgramDrift {
		request.TransitionID = "installation.reconcile-update"
		request.Parameters = append(request.Parameters, protocol.Parameter{Name: "accept_obligation_change", Value: "true"}).Canonical()
		response, handleErr = kernel.Handle(context.Background(), request)
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
	return errors.New("usage: boatstack <status|apply|recover|doctor|events|catalog|guard|rpc|retro|init|attach|detach|version> [flags]")
}

func runRPC() error {
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 4<<20))
	decoder.DisallowUnknownFields()
	var request surfaces.Request
	if err := decoder.Decode(&request); err != nil {
		return fmt.Errorf("decode V2 RPC request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("V2 RPC request contains trailing JSON")
	}
	kernel, err := standardKernel(context.Background(), request)
	if err != nil {
		return err
	}
	response, handleErr := kernel.Handle(context.Background(), request)
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
		"hydrate-runtime": "runtime.hydrate", "configure": "configuration.mutate", "goal-configure": "goal.configure",
		"plan-create": "plan.create", "plan-validate": "plan.validate", "plan-approve": "plan.approve", "plan-activate": "plan.activate", "plan-amend": "plan.amend",
		"workspace-cut": "workspace.cut", "workspace-sync": "workspace.sync", "workspace-cleanup": "workspace.cleanup", "workspace-reap": "workspace.reap",
		"record-build": "gate.build.record", "record-test": "gate.test.record", "record-review": "gate.review.record", "record-change": "gate.change.record", "record-journey": "gate.journey.record",
		"publication-preview": "publication.preview", "publish-pr": "publication.execute", "observe-pr": "publication.observe", "correct-pr": "publication.correct",
		"abandon": "plan.abandon",
	}
	switch command {
	case "status", "next", "next-status":
		return surfaces.OperationResolve, "", nil, nil
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
	options := commandOptions{format: "json", transitionID: string(transition), host: "cli"}
	if defaults != nil {
		options.goalKind, options.deliveryID, options.goalID = defaults["goal-kind"], defaults["delivery"], defaults["goal-id"]
	}
	flags.StringVar(&options.repository, "repo", ".", "explicit invoking repository or worktree")
	flags.StringVar(&options.format, "format", options.format, "json, text, or jsonl")
	flags.StringVar(&options.goalID, "goal-id", options.goalID, "configured goal identity")
	flags.StringVar(&options.goalKind, "goal-kind", options.goalKind, "approved-plan, verified-implementation, open-or-updated-pr, merged-delivery, or safely-abandoned")
	flags.StringVar(&options.deliveryID, "delivery", options.deliveryID, "delivery identity")
	flags.StringVar(&options.flowID, "flow", "", "flow identity")
	flags.StringVar(&options.transitionID, "transition", options.transitionID, "stable semantic transition id")
	flags.StringVar(&options.idempotencyKey, "idempotency-key", "", "exact prior admission idempotency key for safe replay")
	flags.StringVar(&options.humanActor, "human", "", "explicit command-scoped human authority actor")
	flags.BoolVar(&options.repositoryPolicy, "repository-authority", false, "derive repository-policy authority from the V2 project configuration")
	flags.BoolVar(&options.acceptProgramChange, "accept-program-change", false, "explicitly accept the exact prior-to-candidate control-program delta during update")
	flags.Var(&options.parameters, "param", "transition parameter name=value (repeatable)")
	flags.Var(&options.authorityReceipts, "authority-receipt", "authority receipt JSON path (repeatable)")
	flags.BoolVar(&options.follow, "follow", false, "follow passive process events (events with jsonl only)")
	flags.StringVar(&options.host, "host", options.host, "cli, sdk, cursor, codex, claude, gemini, or mcp")
	flags.StringVar(&options.command, "command", "", "raw command to classify at the guard boundary")
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
		if command == "reconcile-update" {
			if !options.acceptProgramChange {
				return commandOptions{}, fmt.Errorf("reconcile-update requires explicit --accept-program-change")
			}
			options.parameters = append(options.parameters, "accept_obligation_change=true")
		}
	case "correct-pr":
		if err := populateFileFingerprint(&options, "body_path", "body_sha256"); err != nil {
			return commandOptions{}, err
		}
	}
	return options, nil
}

func standardKernel(ctx context.Context, request surfaces.Request) (boatstack.Kernel, error) {
	programRequest := distribution.RepositoryProgramRequest{
		Repository: request.Repository, Host: request.Host, CorrelationID: request.CorrelationID,
	}
	if request.TransitionID == "installation.initialize" || request.TransitionID == "configuration.initialize" {
		programRequest.ConfigurationPath, _ = request.Parameters.Get("config_path")
		programRequest.ConfigurationFingerprint, _ = request.Parameters.Get("config_sha256")
	}
	program, err := distribution.StandardProgramForRepository(ctx, programRequest)
	if err != nil {
		return boatstack.Kernel{}, err
	}
	return boatstack.NewKernel("", program)
}

func followEvents(kernel boatstack.Kernel, request surfaces.Request) error {
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
		return fmt.Errorf("init requires --param config_path=<V2-config.json>")
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
	correlation := fmt.Sprintf("cli-%d-%d", os.Getpid(), now.UnixNano())
	goal := model.Goal{}
	if options.goalKind != "" || options.goalID != "" || options.deliveryID != "" {
		goal = model.Goal{ID: options.goalID, Kind: model.GoalKind(options.goalKind), DeliveryID: options.deliveryID}
		if err := goal.Validate(); err != nil {
			return surfaces.Request{}, err
		}
	}
	parameters, err := parseParameters(options.parameters)
	if err != nil {
		return surfaces.Request{}, err
	}
	authority, err := loadAuthority(options, correlation, goal, now)
	if err != nil {
		return surfaces.Request{}, err
	}
	flowID := options.flowID
	if flowID == "" && goal.ID != "" {
		flowID = "flow-" + goal.ID
	}
	if flowID == "" && (operation == surfaces.OperationApply || operation == surfaces.OperationRecover) {
		flowID = "flow-" + correlation
	}
	return surfaces.Request{
		SchemaVersion: surfaces.SchemaVersion, Operation: operation, Repository: options.repository, Host: options.host, CorrelationID: correlation,
		FlowID: flowID, Goal: goal, TransitionID: catalog.TransitionID(options.transitionID), Authority: authority, Parameters: parameters,
		RepositoryAuthority: options.repositoryPolicy, IdempotencyKey: options.idempotencyKey, Command: options.command,
	}, nil
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

func loadAuthority(options commandOptions, correlation string, goal model.Goal, now time.Time) (protocol.AuthorityBundle, error) {
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
		bundle.Receipts = append(bundle.Receipts, receipt)
	}
	if options.humanActor != "" {
		fingerprint := hash([]byte(strings.Join([]string{correlation, goal.ID, options.transitionID, options.humanActor}, "\x00")))
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
			fmt.Printf("healthy=%t kernel=%s core=%s@%s flow=%s@%s core_transitions=%d flow_transitions=%d extension_transitions=%d transitions=%d program=%s drift=%t runtime_healthy=%t update_ready=%t recovery_required=%t snapshot=%s\n%s\n",
				response.Doctor.Healthy, response.Doctor.KernelVersion, response.Doctor.CoreSystemID, response.Doctor.CoreSystemVersion,
				response.Doctor.PrimaryFlowID, response.Doctor.PrimaryFlowVersion, response.Doctor.CoreTransitionCount,
				response.Doctor.FlowTransitionCount, response.Doctor.ExtensionTransitionCount, response.Doctor.TransitionCount,
				response.Doctor.ProgramFingerprint, response.Doctor.UnresolvedProgramDrift, response.Doctor.RuntimeHealthy, response.Doctor.UpdateReady, response.Doctor.RecoveryRequired, response.Doctor.Snapshot, response.Doctor.Detail)
			return nil
		}
		if response.Decision != nil {
			fmt.Printf("%s: %s\n", response.Decision.Kind, response.Decision.Reason)
			if response.Decision.Transition != nil {
				fmt.Println("transition:", response.Decision.Transition.ID)
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

func hash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
