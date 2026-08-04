package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	boatstack "github.com/operatorstack/boatstack/boatstack"
	"github.com/operatorstack/boatstack/boatstack/internal/deliverycontrol"
)

func fail(err error) int {
	fmt.Fprintln(os.Stderr, boatstack.FormatBlocked(os.Stderr, err.Error()))
	return 1
}

const cursorHookSettleDelay = 50 * time.Millisecond

var hookOutputSleep = time.Sleep

func emitHookOutput(writer io.Writer, host string, value []byte) error {
	if len(value) == 0 {
		return nil
	}
	if _, err := writer.Write(value); err != nil {
		return err
	}
	// Cursor currently has a host-side race that can lose output from compiled
	// hooks which exit immediately. Keep the workaround isolated to its adapter.
	if strings.EqualFold(strings.TrimSpace(host), "cursor") {
		hookOutputSleep(cursorHookSettleDelay)
	}
	return nil
}

func failSafetyHook(err error) int {
	fmt.Fprintln(os.Stderr, boatstack.FormatBlocked(os.Stderr, err.Error()))
	// Claude Code and Codex both define exit 2 as a blocking PreToolUse error.
	// Exit 1 is non-blocking in Claude and must never represent policy failure.
	return 2
}

func initCommand(arguments []string) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to initialize")
	binary := flags.String("binary", "", "helper binary to install project-locally; must self-report this process's version (use the target binary's own update for a different version)")
	integrations := flags.String("integrations", "", "core, gstack, spec-kit, or both")
	yes := flags.Bool("yes", false, "accept the generated-file preview; optional integrations still default to core")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	err := boatstack.RunInit(boatstack.InitOptions{Repo: *repo, BinaryPath: *binary, IntegrationChoice: *integrations, Yes: *yes})
	if err != nil {
		return fail(err)
	}
	return 0
}

func updateCommand(arguments []string) int {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to update")
	binary := flags.String("binary", "", "helper binary to install; its own self-reported version is installed (cross-version updates re-exec it)")
	yes := flags.Bool("yes", false, "accept the generated-file preview")
	repair := flags.Bool("repair", false, "repair only fingerprinted Boatstack-owned control state")
	allowDowngrade := flags.Bool("allow-downgrade", false, "permit an explicitly repaired downgrade")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	err := boatstack.RunUpdate(boatstack.InitOptions{Repo: *repo, BinaryPath: *binary, Yes: *yes, Repair: *repair, AllowDowngrade: *allowDowngrade})
	if err != nil {
		return fail(err)
	}
	return 0
}

func emitJSON(value any) int {
	raw, err := boatstack.MarshalJSON(value)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(raw))
	return 0
}

func applyStateRoot(override string) {
	if strings.TrimSpace(override) != "" {
		_ = os.Setenv("BOATSTACK_STATE_ROOT", override)
	}
}

func attachCommand(arguments []string) int {
	flags := flag.NewFlagSet("attach", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to attach")
	mode := flags.String("mode", "detached", "supervision mode; only \"detached\" is supported by attach")
	stateRoot := flags.String("state-root", "", "external control-state root (overrides the default user state directory)")
	force := flags.Bool("force", false, "re-attach even if the repository is already attached")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *mode != "detached" {
		return fail(fmt.Errorf("attach supports only --mode detached"))
	}
	applyStateRoot(*stateRoot)
	result, err := boatstack.AttachDetached(boatstack.AttachOptions{Repo: *repo, Force: *force})
	if err != nil {
		return fail(err)
	}
	code := emitJSON(result)
	if result.VerificationStatus == "BLOCKED" {
		return 1
	}
	return code
}

func detachCommand(arguments []string) int {
	flags := flag.NewFlagSet("detach", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to detach")
	stateRoot := flags.String("state-root", "", "external control-state root (overrides the default user state directory)")
	preserve := flags.Bool("preserve-state", false, "keep the external controller state instead of removing it")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	applyStateRoot(*stateRoot)
	result, err := boatstack.DetachDetached(boatstack.DetachOptions{Repo: *repo, PreserveState: *preserve})
	if err != nil {
		return fail(err)
	}
	code := emitJSON(result)
	if result.VerificationStatus == "BLOCKED" {
		return 1
	}
	return code
}

func detachedStatusCommand(arguments []string) int {
	flags := flag.NewFlagSet("detached-status", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to inspect")
	stateRoot := flags.String("state-root", "", "external control-state root (overrides the default user state directory)")
	_ = flags.Bool("json", true, "emit the versioned JSON projection (always JSON)")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	applyStateRoot(*stateRoot)
	result, err := boatstack.DetachedStatus(*repo)
	if err != nil {
		return fail(err)
	}
	return emitJSON(result)
}

func activateCommand(arguments []string) int {
	flags := flag.NewFlagSet("activate", flag.ContinueOnError)
	repo := flags.String("repo", ".", "attached repository to activate")
	host := flags.String("host", "", "limit to one coding agent (cursor|claude|codex|gemini); default all")
	stateRoot := flags.String("state-root", "", "external control-state root (overrides the default user state directory)")
	print := flags.Bool("print", false, "only print the per-agent config to add; do not install it")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	applyStateRoot(*stateRoot)
	var hosts []string
	if strings.TrimSpace(*host) != "" {
		hosts = []string{*host}
	}
	if *print {
		result, err := boatstack.DetachedActivationPlan(*repo, hosts)
		if err != nil {
			return fail(err)
		}
		return emitJSON(result)
	}
	result, err := boatstack.InstallAmbientHooks(*repo, hosts)
	if err != nil {
		return fail(err)
	}
	code := emitJSON(result)
	if result.VerificationStatus == "BLOCKED" {
		return 1
	}
	return code
}

func deactivateCommand(arguments []string) int {
	flags := flag.NewFlagSet("deactivate", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to deactivate")
	host := flags.String("host", "", "limit to one coding agent (cursor|claude|codex|gemini); default all")
	stateRoot := flags.String("state-root", "", "external control-state root (overrides the default user state directory)")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	applyStateRoot(*stateRoot)
	var hosts []string
	if strings.TrimSpace(*host) != "" {
		hosts = []string{*host}
	}
	result, err := boatstack.RemoveAmbientHooks(*repo, hosts)
	if err != nil {
		return fail(err)
	}
	code := emitJSON(result)
	if result.VerificationStatus == "BLOCKED" {
		return 1
	}
	return code
}

func contextCommand(arguments []string) int {
	flags := flag.NewFlagSet("context", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to project context for")
	operation := flags.String("operation", "", "the operation about to run (advisory)")
	host := flags.String("host", "", "the coding-agent host (advisory)")
	stateRoot := flags.String("state-root", "", "external control-state root (overrides the default user state directory)")
	_ = flags.Bool("json", true, "emit the versioned JSON projection (always JSON)")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	applyStateRoot(*stateRoot)
	result, err := boatstack.ProjectOperatorContext(*repo, *operation, *host)
	if err != nil {
		return fail(err)
	}
	return emitJSON(result)
}

func repairStatusCommand(arguments []string) int {
	flags := flag.NewFlagSet("repair-status", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository installation to inspect")
	allowDowngrade := flags.Bool("allow-downgrade", false, "include explicit downgrade authority in the projection")
	jsonOutput := flags.Bool("json", false, "emit the versioned JSON projection")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	config, _, err := boatstack.LoadConfig(boatstack.WorkspaceFor(*repo).SourceConfigPath())
	if err != nil {
		return fail(err)
	}
	result, err := boatstack.ClassifyInstallationRepair(*repo, config.Adapters, *allowDowngrade)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(result)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		fmt.Print(string(value))
	} else {
		fmt.Printf("REPAIR_STATUS=%s\nDIRECTION=%s\nPACKAGE_FINGERPRINT=%s\nNEXT_OPERATION=%s\n", result.VerificationStatus, result.Direction, result.PackageFingerprint, result.NextOperation)
	}
	if result.VerificationStatus == "BLOCKED" {
		return 1
	}
	return 0
}

func checkUpdateCommand(arguments []string) int {
	flags := flag.NewFlagSet("check-update", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose Boatstack release should be checked")
	force := flags.Bool("force", false, "ignore the 24-hour release cache")
	notify := flags.Bool("notify", false, "record a bounded post-ship notification")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	result, err := boatstack.CheckForUpdate(boatstack.UpdateCheckOptions{Repo: *repo, Force: *force, Notify: *notify})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("UPDATE_STATUS=%s\nCURRENT_VERSION=%s\nLATEST_VERSION=%s\nRELEASE_NAME=%q\nRELEASE_NOTES=%q\nRELEASE_URL=%s\nUPDATE_NOTIFY=%t\nUPDATE_FROM_CACHE=%t\n", result.Status, result.CurrentVersion, result.LatestVersion, result.ReleaseName, result.ReleaseNotes, result.ReleaseURL, result.ShouldNotify, result.FromCache)
	return 0
}

func operationStatusCommand(arguments []string) int {
	flags := flag.NewFlagSet("operation-status", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose durable operation state should be inspected")
	operationID := flags.String("operation-id", "", "specific operation identity; omit only when the current branch has at most one unfinished operation")
	jsonOutput := flags.Bool("json", false, "emit the versioned JSON projection")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	status, err := boatstack.ResolveOperationStatus(*repo, *operationID)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(status)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		fmt.Print(string(value))
	} else if status.Operation == nil {
		fmt.Printf("OPERATION_STATUS=%s\nNEXT_OPERATION=%s\n", status.VerificationStatus, status.NextOperation)
	} else {
		fmt.Printf("OPERATION_STATUS=%s\nOPERATION_ID=%s\nSTATE=%s\nATTEMPT=%d/%d\nNEXT_OPERATION=%s\n", status.VerificationStatus, status.Operation.OperationID, status.Operation.State, status.Operation.Attempt, status.Operation.MaxAttempts, status.NextOperation)
	}
	if status.VerificationStatus == "AMBIGUOUS" {
		return 1
	}
	return 0
}

func prepareUpdatePRCommand(arguments []string) int {
	flags := flag.NewFlagSet("prepare-update-pr", flag.ContinueOnError)
	repo := flags.String("repo", ".", "updated Boatstack repository")
	version := flags.String("version", "", "exact installed stable version")
	jsonOutput := flags.Bool("json", false, "emit the fingerprinted preview as JSON")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	preview, err := boatstack.PrepareUpdatePublication(*repo, *version)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(preview)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		fmt.Print(string(value))
	} else {
		fmt.Printf("UPDATE_PREVIEW=%s\nPREVIEW_FINGERPRINT=%s\nPACKAGE_FINGERPRINT=%s\n", preview.PreviewPath, preview.Fingerprint, preview.PackageFingerprint)
	}
	return 0
}

func publishUpdatePRCommand(arguments []string) int {
	flags := flag.NewFlagSet("publish-update-pr", flag.ContinueOnError)
	repo := flags.String("repo", ".", "updated Boatstack repository")
	preview := flags.String("preview", "", "exact machine-local update preview path")
	fingerprint := flags.String("preview-fingerprint", "", "fingerprint confirmed by the human")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *preview == "" || *fingerprint == "" {
		return fail(fmt.Errorf("publish-update-pr requires --preview and --preview-fingerprint"))
	}
	url, err := boatstack.PublishUpdatePublication(boatstack.UpdatePublishOptions{Repo: *repo, PreviewPath: *preview, ExpectedFingerprint: *fingerprint})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("PR_URL=%s\n", url)
	return 0
}

func releaseClassifyCommand(arguments []string) int {
	flags := flag.NewFlagSet("release-classify", flag.ContinueOnError)
	repo := flags.String("repo", ".", "projected Boatstack repository")
	base := flags.String("base", "", "latest released tag or commit")
	head := flags.String("head", "HEAD", "candidate release commit")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	classification, err := boatstack.ClassifyReleaseDiff(*repo, *base, *head)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("release_required=%t\nrelease_paths=%s\n", classification.Required, strings.Join(classification.Paths, ","))
	return 0
}

func nextPatchCommand(arguments []string) int {
	flags := flag.NewFlagSet("next-patch", flag.ContinueOnError)
	version := flags.String("version", "", "current stable vMAJOR.MINOR.PATCH version")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	next, err := boatstack.NextPatchVersion(*version)
	if err != nil {
		return fail(err)
	}
	fmt.Println(next)
	return 0
}

func exportCommand(arguments []string) int {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	repo := flags.String("repo", "", "repository to export into")
	configPath := flags.String("config", "", "Boatstack project config")
	adapterName := flags.String("adapter-name", "boatstack", "generated adapter slug")
	adapters := flags.String("adapters", "", "comma-separated adapter override")
	write := flags.Bool("write", false, "write generated files")
	check := flags.Bool("check", false, "check generated files for drift")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *repo == "" || *configPath == "" || (*write && *check) {
		return fail(fmt.Errorf("export requires --repo and --config; --write and --check are mutually exclusive"))
	}
	config, raw, err := boatstack.LoadConfig(*configPath)
	if err != nil {
		return fail(err)
	}
	if *adapters != "" {
		config.Adapters = strings.Split(*adapters, ",")
	}
	bundle, err := boatstack.BuildExportBundle(*configPath, config, raw, *adapterName)
	if err != nil {
		return fail(err)
	}
	if *check {
		if err := boatstack.CheckExport(*repo, bundle.Files); err != nil {
			return fail(err)
		}
		if err := boatstack.CheckHostHooks(*repo, bundle.Config.Adapters); err != nil {
			return fail(err)
		}
		fmt.Printf("PASS: %d generated files match Boatstack %s\n", len(bundle.Files), boatstack.Version)
		return 0
	}
	if *write {
		if err := boatstack.WriteExport(*repo, bundle.Files); err != nil {
			return fail(err)
		}
		if err := boatstack.InstallHostHooks(*repo, bundle.Config.Adapters); err != nil {
			return fail(err)
		}
		fmt.Printf("PASS: wrote %d generated files to %s\n", len(bundle.Files), *repo)
		return 0
	}
	fmt.Printf("dry run: would generate %d files in %s\n", len(bundle.Files), *repo)
	for _, path := range func() []string {
		paths := make([]string, 0, len(bundle.Files))
		for path := range bundle.Files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		return paths
	}() {
		fmt.Println("  " + path)
	}
	for _, path := range boatstack.HostHookPaths(bundle.Config.Adapters) {
		fmt.Println("  " + path + " (merge safety hook)")
	}
	return 0
}

func checkPlanCommand(arguments []string) int {
	flags := flag.NewFlagSet("check-plan", flag.ContinueOnError)
	plan := flags.String("plan", "", "Markdown structured plan")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *plan == "" {
		return fail(fmt.Errorf("check-plan requires --plan"))
	}
	check, err := boatstack.CheckPlan(*plan)
	if err != nil {
		return fail(fmt.Errorf("invalid Markdown plan: %w", err))
	}
	baseline, err := boatstack.PlanningBaselineForPlan(*plan)
	if err != nil {
		return fail(fmt.Errorf("cannot fingerprint the pre-activation product baseline: %w", err))
	}
	readinessFingerprint := ""
	if version, _ := check.Plan["schema_version"].(float64); version >= 3 {
		readiness, readinessErr := boatstack.CheckPlanReadiness(*plan)
		repo, _ := boatstack.ResolveControllerRepository(filepath.Dir(*plan))
		if readinessErr != nil {
			boatstack.RecordFlowAttribution(repo, "readiness", deliverycontrol.CostQuery, true, readinessErr.Error())
			return fail(readinessErr)
		}
		readinessFingerprint = readiness.Fingerprint
		boatstack.RecordFlowAttribution(repo, "readiness", deliverycontrol.CostQuery, false, "current")
	}
	paths, _ := json.Marshal(baseline.ChangedPaths)
	fmt.Printf("PASS: Markdown plan is structurally valid\nPLAN_FINGERPRINT=%s\nREADINESS_FINGERPRINT=%s\nSOURCE_PLAN=%s\nSPEC=%s\nBASELINE_DIFF_SHA256=%s\nBASELINE_CHANGED_PATHS=%s\n", check.Fingerprint, readinessFingerprint, check.SourcePlanPath, check.SpecPath, baseline.DiffSHA256, paths)
	return 0
}

func checkSourcePlanCommand(arguments []string) int {
	flags := flag.NewFlagSet("check-source-plan", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository the source plan is validated against")
	plan := flags.String("plan", "", "required in-repo path to the plan produced in the host conversation")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	discovered, err := boatstack.DiscoverSourcePlan(*repo, *plan)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("PASS: source plan is present\nSOURCE_PLAN=%s\n", discovered)
	return 0
}

func activatePlanCommand(arguments []string) int {
	flags := flag.NewFlagSet("activate-plan", flag.ContinueOnError)
	options := boatstack.ActivationOptions{}
	flags.StringVar(&options.PlanPath, "plan", "", "approved Markdown plan")
	flags.StringVar(&options.ApprovalPath, "approval", "", "Markdown approval receipt")
	flags.StringVar(&options.OutDir, "out-dir", "", "compiled artifact directory")
	flags.StringVar(&options.OutputPath, "output", "", "plan lock path")
	flags.StringVar(&options.SourceCommit, "source-commit", "", "source Git commit")
	flags.StringVar(&options.AutonomyPath, "autonomy", "", "fingerprinted autonomy.md receipt")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if options.PlanPath == "" || options.OutDir == "" || options.OutputPath == "" {
		return fail(fmt.Errorf("activate-plan requires --plan, --out-dir, and --output; human_plan_approval additionally requires --approval unless a valid --autonomy receipt targets verified or pr"))
	}
	if err := boatstack.ActivatePlan(options); err != nil {
		return fail(fmt.Errorf("plan activation failed: %w", err))
	}
	boatstack.RecordFlowAttribution(filepath.Dir(options.PlanPath), "authorization_freshness", deliverycontrol.CostQuery, false, "immutable lock current")
	fmt.Printf("PASS: approved Markdown plan activated and locked: %s\n", options.OutputPath)
	return 0
}

func recordAutonomyCommand(arguments []string) int {
	flags := flag.NewFlagSet("record-autonomy", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository containing the feature package")
	plan := flags.String("plan", "", "validated Markdown plan")
	target := flags.String("target", "", "plan, verified, or pr")
	output := flags.String("output", "", "autonomy.md path; defaults beside plan.md")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *plan == "" || *target == "" {
		return fail(fmt.Errorf("record-autonomy requires --plan and --target plan|verified|pr"))
	}
	parsed, err := boatstack.ParseRunTarget(*target)
	if err != nil {
		return fail(err)
	}
	receipt, err := boatstack.RecordAutonomy(boatstack.AutonomyRecordOptions{Repo: *repo, PlanPath: *plan, Target: parsed, OutputPath: *output})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("PASS: scoped autonomous run recorded\nRUN_TARGET=%s\nAUTONOMY_FINGERPRINT=%s\n", receipt.Target, receipt.Fingerprint)
	return 0
}

func planningWriteCommand(arguments []string) int {
	flags := flag.NewFlagSet("planning-write", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository containing the feature package")
	feature := flags.String("feature", "", "lowercase kebab-case feature slug")
	artifact := flags.String("artifact", "", "known Markdown planning artifact name")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *feature == "" || *artifact == "" {
		return fail(fmt.Errorf("planning-write requires --feature and --artifact; Markdown content is read from stdin"))
	}
	content, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fail(err)
	}
	path, err := boatstack.WritePlanningArtifact(boatstack.PlanningWriteOptions{
		Repo: *repo, Feature: *feature, Artifact: *artifact, Content: content,
	})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("PASS: wrote bounded planning Markdown: %s\n", path)
	return 0
}

func recordApprovalCommand(arguments []string) int {
	flags := flag.NewFlagSet("record-approval", flag.ContinueOnError)
	plan := flags.String("plan", "", "approved Markdown plan")
	output := flags.String("output", "", "approval.md path; defaults beside plan.md")
	approvedBy := flags.String("approved-by", "", "named human approver")
	approvedAt := flags.String("approved-at", "", "RFC3339 approval timestamp")
	fingerprint := flags.String("fingerprint", "", "exact fingerprint displayed before approval")
	baselineDiffSHA256 := flags.String("baseline-diff-sha256", "", "exact product baseline fingerprint displayed before approval; omit only when clean")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *plan == "" || *approvedBy == "" || *approvedAt == "" || *fingerprint == "" {
		return fail(fmt.Errorf("record-approval requires --plan, --approved-by, --approved-at, and --fingerprint"))
	}
	if err := boatstack.RecordApproval(boatstack.ApprovalRecordOptions{
		PlanPath: *plan, OutputPath: *output, ApprovedBy: *approvedBy,
		ApprovedAt: *approvedAt, Fingerprint: *fingerprint, BaselineDiffSHA256: *baselineDiffSHA256,
	}); err != nil {
		return fail(err)
	}
	fmt.Println("PASS: exact Markdown plan approval recorded")
	return 0
}

func recordDeliveryGateCommand(arguments []string) int {
	flags := flag.NewFlagSet("record-delivery-gate", flag.ContinueOnError)
	options := boatstack.DeliveryGateOptions{}
	flags.StringVar(&options.Repo, "repo", ".", "repository containing the managed delivery")
	flags.StringVar(&options.Feature, "feature", "", "managed Boatstack feature slug")
	flags.StringVar(&options.SliceID, "slice", "", "delivery slice id; redirects to the named active or published-open slice (default: active slice)")
	flags.StringVar(&options.Gate, "gate", "", "test or review")
	flags.StringVar(&options.Status, "status", "", "PASS or PASS_WITH_GAPS")
	flags.StringVar(&options.BaseBranch, "base", "", "delivery base branch; defaults from the active slice or project")
	flags.StringVar(&options.EvidencePath, "evidence", "", "current evidence ledger")
	flags.StringVar(&options.ReviewerIdentity, "reviewer-identity", "", "reviewer identity required for configured high-risk independent review")
	flags.StringVar(&options.ReviewMethod, "review-method", "", "human_peer or separate_agent")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if options.Feature == "" || options.SliceID == "" || options.Gate == "" || options.Status == "" {
		return fail(fmt.Errorf("record-delivery-gate requires --feature, --slice, --gate, and --status"))
	}
	transition := boatstack.GateTransition(options.Gate)
	guard := boatstack.GuardFlowMove(options.Repo, options.Feature, transition)
	if !guard.Allow {
		boatstack.RecordFlowTransition(options.Repo, guard.Transition, guard.From, false)
		return fail(fmt.Errorf("%s", guard.Message))
	}
	receipt, err := boatstack.RecordDeliveryGate(options)
	boatstack.RecordFlowTransition(options.Repo, transition, guard.From, err == nil)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("PASS: %s gate recorded for delivery slice %s\nSLICE=%s\nGATE=%s\nSTATUS=%s\nHEAD_COMMIT=%s\nDIFF_SHA256=%s\n", strings.ToUpper(receipt.Gate), receipt.SliceID, receipt.SliceID, receipt.Gate, receipt.Status, receipt.HeadCommit, receipt.DiffSHA256)
	return 0
}

func recordPRVisualEvidenceCommand(arguments []string) int {
	flags := flag.NewFlagSet("record-pr-visual-evidence", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose Git-common state owns the evidence")
	manifest := flags.String("manifest", "", "JSON manifest containing local PNG paths")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *manifest == "" {
		return fail(fmt.Errorf("record-pr-visual-evidence requires --manifest"))
	}
	recorded, err := boatstack.ImportPRVisualEvidence(*repo, *manifest)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(recorded)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	return 0
}

func captureEvidenceCommand(arguments []string) int {
	flags := flag.NewFlagSet("capture-evidence", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose Git-common state owns the evidence")
	capability := flags.String("capability", "visual", "evidence capability to capture")
	feature := flags.String("feature", "", "managed Boatstack feature slug")
	base := flags.String("base", "", "base branch for the product diff (defaults to the project default branch)")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *feature == "" {
		return fail(fmt.Errorf("capture-evidence requires --feature"))
	}
	captured, err := boatstack.CaptureEvidence(boatstack.CaptureEvidenceOptions{
		Repo: *repo, Capability: *capability, Feature: *feature, Base: *base,
	})
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(captured)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	return 0
}

func provisionCapabilityCommand(arguments []string) int {
	flags := flag.NewFlagSet("provision-capability", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to inspect for evidence-capability provisioning")
	capability := flags.String("capability", "visual", "evidence capability to provision")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	guide, err := boatstack.CapabilityProvisionGuide(*repo, *capability)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(guide)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	return 0
}

func capabilityRegisterCommand(arguments []string) int {
	flags := flag.NewFlagSet("capability-register", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose Boatstack configuration owns the command")
	capability := flags.String("capability", "visual", "evidence capability to register a command for")
	surface := flags.String("surface", "", "optional product surface (e.g. web, ops) to scope the command to")
	command := flags.String("command", "", "repository command that produces the evidence")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *command == "" {
		return fail(fmt.Errorf("capability-register requires --command"))
	}
	registered, err := boatstack.RegisterCapabilityCommand(*repo, *capability, *surface, *command)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(registered)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	return 0
}

func recordPRVisualPublicationCommand(arguments []string) int {
	flags := flag.NewFlagSet("record-pr-visual-publication", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose Git-common state owns the evidence")
	key := flags.String("key", "", "managed feature or ad-hoc branch evidence key")
	prURL := flags.String("pr-url", "", "published pull request URL")
	commentURL := flags.String("comment-url", "", "observable Boatstack evidence comment URL")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *key == "" || *prURL == "" || *commentURL == "" {
		return fail(fmt.Errorf("record-pr-visual-publication requires --key, --pr-url, and --comment-url"))
	}
	recorded, err := boatstack.RecordPRVisualPublication(*repo, *key, *prURL, *commentURL)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(recorded)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	return 0
}

func attachEvidenceCommand(arguments []string) int {
	flags := flag.NewFlagSet("attach-evidence", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose Git-common state owns the evidence")
	feature := flags.String("feature", "", "managed Boatstack feature slug")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *feature == "" {
		return fail(fmt.Errorf("attach-evidence requires --feature"))
	}
	manifest, err := boatstack.RetryVisualAttachment(*repo, *feature, boatstack.SelectVisualPublisher(*repo))
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(manifest)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	return 0
}

func deliveryStatusCommand(arguments []string) int {
	flags := flag.NewFlagSet("delivery-status", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository containing the managed delivery")
	feature := flags.String("feature", "", "managed Boatstack feature slug")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *feature == "" {
		return fail(fmt.Errorf("delivery-status requires --feature"))
	}
	state, err := boatstack.CurrentDeliveryState(*repo, *feature)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(state)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	return 0
}

func nextStatusCommand(arguments []string) int {
	flags := flag.NewFlagSet("next-status", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose Boatstack stage should be inspected")
	feature := flags.String("feature", "", "optional specific managed feature to inspect")
	jsonOutput := flags.Bool("json", false, "print the versioned structured status")
	render := flags.Bool("render", false, "print the branded, human-facing status banner")
	format := flags.String("format", "", `optional output format: "response" renders the canonical response contract (banner, outcome line, one ### Next step)`)
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *format != "" && *format != "response" {
		return fail(fmt.Errorf(`unsupported format %q; use --format response`, *format))
	}
	status, err := boatstack.ResolveNext(*repo, *feature)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		value, marshalErr := boatstack.MarshalJSON(status)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		fmt.Print(string(value))
	} else if *format == "response" {
		output, renderErr := boatstack.RenderNextStatusResponse(*repo, status)
		if renderErr != nil {
			return fail(renderErr)
		}
		fmt.Print(output)
	} else if *render {
		fmt.Print(boatstack.RenderNextStatusBanner(status))
	} else {
		fmt.Print(boatstack.FormatNextStatus(status))
	}
	return 0
}

func recoveryStatusCommand(arguments []string) int {
	flags := flag.NewFlagSet("recovery-status", flag.ContinueOnError)
	options := boatstack.RecoveryStatusOptions{}
	flags.StringVar(&options.Repo, "repo", ".", "repository whose managed delivery should be resolved")
	flags.StringVar(&options.Feature, "feature", "", "optional specific active or published feature")
	flags.StringVar(&options.Message, "message", "", "exact reported correction")
	flags.StringVar(&options.SourceStage, "source-stage", "", "ci, review, publication, or user")
	flags.StringVar(&options.Evidence, "evidence", "", "bounded failure or review reference")
	flags.StringVar(&options.ObservedHeadSHA, "observed-head-sha", "", "optional PR head tied to the reported evidence")
	jsonOutput := flags.Bool("json", false, "print the versioned structured recovery decision")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	status, err := boatstack.ResolveRecovery(options)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		value, marshalErr := boatstack.MarshalJSON(status)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		fmt.Print(string(value))
	} else {
		fmt.Printf("Recovery: %s\nFeature: %s\nLifecycle: %s\nNext operation: %s\nReason: %s\n", status.VerificationStatus, status.Feature, status.Lifecycle, status.NextOperation, status.Reason)
	}
	if status.VerificationStatus == "BLOCKED" {
		return 1
	}
	return 0
}

func repairStateCommand(arguments []string) int {
	flags := flag.NewFlagSet("repair-state", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose stuck feature draft should be repaired")
	feature := flags.String("feature", "", "optional specific feature draft to repair")
	jsonOutput := flags.Bool("json", false, "print the versioned structured repair decision")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	result, err := boatstack.RepairState(*repo, *feature)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		value, marshalErr := boatstack.MarshalJSON(result)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		fmt.Print(string(value))
	} else {
		fmt.Printf("Repair: %s\nFeature: %s\nAction: %s\nQuarantine: %s\nNext operation: %s\nReason: %s\n",
			result.VerificationStatus, result.Feature, result.Action, result.QuarantinePath, result.NextOperation, result.Reason)
	}
	if result.VerificationStatus == "BLOCKED" {
		return 1
	}
	return 0
}

func mutationStatusCommand(arguments []string) int {
	flags := flag.NewFlagSet("mutation-status", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose managed-artifact mutation receipts should be listed")
	mutation := flags.String("mutation", "", "optional specific mutation id to inspect")
	jsonOutput := flags.Bool("json", false, "print the structured mutation receipt(s)")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if strings.TrimSpace(*mutation) != "" {
		receipt, ok, err := boatstack.GetMutationReceipt(*repo, *mutation)
		if err != nil {
			return fail(err)
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "no mutation receipt for %s\n", *mutation)
			return 1
		}
		if *jsonOutput {
			value, marshalErr := boatstack.MarshalJSON(receipt)
			if marshalErr != nil {
				return fail(marshalErr)
			}
			fmt.Print(string(value))
		} else {
			fmt.Printf("Mutation: %s\nKind: %s\nStatus: %s\nRecorded: %s\nScope: %s\n", receipt.MutationID, receipt.Kind, receipt.Status, receipt.RecordedAt, strings.Join(receipt.Scope, ", "))
		}
		return 0
	}
	receipts, err := boatstack.ListMutationReceipts(*repo)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		value, marshalErr := boatstack.MarshalJSON(receipts)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		fmt.Print(string(value))
	} else {
		if len(receipts) == 0 {
			fmt.Println("No mutation receipts.")
		}
		for _, receipt := range receipts {
			fmt.Printf("%s\t%s\t%s\t%s\n", receipt.MutationID, receipt.Kind, receipt.Status, receipt.RecordedAt)
		}
	}
	return 0
}

func undoCommand(arguments []string) int {
	flags := flag.NewFlagSet("undo", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository containing the managed-artifact mutation to reverse")
	mutation := flags.String("mutation", "", "mutation id to undo (its receipt is the inverse command)")
	jsonOutput := flags.Bool("json", false, "print the structured undo receipt")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if strings.TrimSpace(*mutation) == "" {
		fmt.Fprintln(os.Stderr, "undo requires --mutation <id>")
		return 2
	}
	receipt, err := boatstack.UndoManagedMutation(*repo, *mutation)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		value, marshalErr := boatstack.MarshalJSON(receipt)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		fmt.Print(string(value))
	} else {
		fmt.Printf("Undo: %s\nKind: %s\nStatus: %s\nScope: %s\n", receipt.MutationID, receipt.Kind, receipt.Status, strings.Join(receipt.Scope, ", "))
	}
	return 0
}

func runPreflightCommand(arguments []string) int {
	flags := flag.NewFlagSet("run-preflight", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose Git state should be verified before boatstack run")
	feature := flags.String("feature", "", "optional specific managed feature to verify")
	jsonOutput := flags.Bool("json", false, "print the versioned structured preflight")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	status := boatstack.CheckRunPreflight(*repo, *feature)
	if *jsonOutput {
		value, err := boatstack.MarshalJSON(status)
		if err != nil {
			return fail(err)
		}
		fmt.Print(string(value))
	} else {
		fmt.Printf("Boatstack run preflight: %s\nAuthority: %s\nAuthority reason: %s\nReason: %s\n", status.VerificationStatus, status.AuthorityStatus, status.AuthorityReason, status.Reason)
	}
	if status.VerificationStatus != "VERIFIED" {
		return 1
	}
	return 0
}

func authorityContextCommand(arguments []string) int {
	flags := flag.NewFlagSet("authority-context", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose authority binding should be projected")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	context, err := boatstack.ResolveAuthorityContext(*repo)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(context)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	return 0
}

func recordChangeCommand(arguments []string) int {
	flags := flag.NewFlagSet("record-change", flag.ContinueOnError)
	options := boatstack.ChangeObservationOptions{}
	flags.StringVar(&options.Repo, "repo", ".", "repository containing the managed delivery")
	flags.StringVar(&options.Feature, "feature", "", "managed Boatstack feature slug")
	flags.StringVar(&options.Message, "message", "", "exact user change request")
	flags.StringVar(&options.SourceStage, "source-stage", "", "stage where the change was observed")
	flags.StringVar(&options.Expected, "expected", "", "approved or requested expected behavior")
	flags.StringVar(&options.Actual, "actual", "", "observed behavior")
	flags.StringVar(&options.Evidence, "evidence", "", "bounded evidence or reproduction reference")
	flags.StringVar(&options.Mechanism, "mechanism", "", "repair mechanism used to address the observed failure")
	flags.StringVar(&options.Classification, "classification", "", "implementation_repair, verification_repair, review_repair, requirement_amendment, needs_clarification, or plan_invalid")
	flags.StringVar(&options.SliceID, "slice", "", "delivery slice id the correction targets; redirects to the named active or published-open slice (default: the correction's branch, then the active slice)")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if options.Feature == "" || options.Message == "" || options.SourceStage == "" || options.Classification == "" {
		return fail(fmt.Errorf("record-change requires --feature, --message, --source-stage, and --classification"))
	}
	if strings.HasSuffix(strings.ToLower(options.Classification), "_repair") && strings.TrimSpace(options.Mechanism) == "" {
		return fail(fmt.Errorf("record-change requires --mechanism for repair classifications"))
	}
	observation, state, err := boatstack.RecordChangeObservation(options)
	if err != nil {
		if strings.HasSuffix(strings.ToLower(options.Classification), "_repair") &&
			(strings.Contains(err.Error(), "friction:") || strings.Contains(err.Error(), "budget exhausted")) {
			boatstack.RecordFlowAttribution(options.Repo, "repair."+strings.ToLower(options.Classification), deliverycontrol.CostFriction, true, err.Error())
		}
		return fail(err)
	}
	if strings.HasSuffix(strings.ToLower(options.Classification), "_repair") {
		boatstack.RecordFlowAttribution(options.Repo, "repair."+strings.ToLower(options.Classification), deliverycontrol.CostRecovery, false, options.Mechanism)
	}
	// A recorded correction is the honest moment coding rework is initiated;
	// record one unit of coding effort as telemetry (never a gate, never J_flow).
	boatstack.RecordCodingEffort(options.Repo, 1, string(observation.Classification))
	fmt.Printf("PASS: change observation recorded\nOBSERVATION_ID=%s\nCLASSIFICATION=%s\nOUTCOME=%s\nMODE=%s\nRESUME_STAGE=%s\n", observation.ID, observation.Classification, observation.Outcome, state.Mode, state.ResumeStage)
	if observation.Outcome == "CORRECTIVE_CHILD_REQUIRED" {
		fmt.Printf("PARENT_DELIVERY=%s\nSUGGESTED_FEATURE_ID=%s\n", observation.ParentDelivery, observation.SuggestedFeatureID)
	}
	return 0
}

func recordJourneyResultsCommand(arguments []string) int {
	flags := flag.NewFlagSet("record-journey-results", flag.ContinueOnError)
	options := boatstack.JourneyResultsOptions{}
	flags.StringVar(&options.Repo, "repo", ".", "repository containing the managed delivery")
	flags.StringVar(&options.Feature, "feature", "", "managed Boatstack feature slug")
	flags.StringVar(&options.BaseBranch, "base", "", "delivery base branch")
	flags.StringVar(&options.InputPath, "results", "", "JSON file containing typed oracle results")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if options.Feature == "" || options.InputPath == "" {
		return fail(fmt.Errorf("record-journey-results requires --feature and --results"))
	}
	result, err := boatstack.RecordJourneyResults(options)
	if err != nil {
		boatstack.RecordFlowAttribution(options.Repo, "journey_discovery", deliverycontrol.CostQuery, false, err.Error())
		return fail(err)
	}
	boatstack.RecordFlowAttribution(options.Repo, "journey_discovery", deliverycontrol.CostQuery, false, "results bound to manifest and diff")
	fmt.Printf("PASS: journey results recorded\nMANIFEST_SHA256=%s\nHEAD_COMMIT=%s\nDIFF_SHA256=%s\n", result.ManifestSHA256, result.HeadCommit, result.DiffSHA256)
	return 0
}

func ignoreDeliveryCommand(arguments []string) int {
	flags := flag.NewFlagSet("ignore-delivery", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository containing the Boatstack installation")
	feature := flags.String("feature", "", "feature slug of the past delivery to ignore")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *feature == "" {
		return fail(fmt.Errorf("ignore-delivery requires --feature"))
	}
	added, err := boatstack.IgnoreDelivery(*repo, *feature)
	if err != nil {
		return fail(err)
	}
	if added {
		fmt.Printf("PASS: delivery %s added to workflow.ignored_deliveries\n", *feature)
	} else {
		fmt.Printf("PASS: delivery %s already ignored\n", *feature)
	}
	return 0
}

func discardDeliveryCommand(arguments []string) int {
	flags := flag.NewFlagSet("discard-delivery", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository containing the Boatstack installation")
	feature := flags.String("feature", "", "feature slug of the delivery whose state should be discarded")
	force := flags.Bool("force", false, "discard even a delivery that has published slices (git history and merged PRs are unaffected)")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *feature == "" {
		return fail(fmt.Errorf("discard-delivery requires --feature"))
	}
	result, err := boatstack.DiscardDelivery(*repo, *feature, *force)
	if err != nil {
		return fail(err)
	}
	switch result.Action {
	case "discarded":
		fmt.Printf("PASS: delivery %s discarded; state archived to %s\n", result.Feature, result.ArchivePath)
		return 0
	case "none":
		fmt.Printf("PASS: %s\n", result.Reason)
		return 0
	default: // refused
		if len(result.Published) > 0 {
			fmt.Printf("BLOCKED: delivery %s has published slices (%s); %s\n", result.Feature, strings.Join(result.Published, ", "), result.Reason)
		} else {
			fmt.Printf("BLOCKED: delivery %s: %s\n", result.Feature, result.Reason)
		}
		return 1
	}
}

func doctorCommand(arguments []string) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose Boatstack installation should be checked")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if err := boatstack.DoctorRepairHint(boatstack.Doctor(*repo)); err != nil {
		return fail(err)
	}
	root, err := boatstack.ResolveRepository(*repo)
	if err != nil {
		return fail(err)
	}
	ctx, err := boatstack.ResolveWorkspaceContext(root)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("PASS: Boatstack %s installation and generated adapters are healthy\n", boatstack.Version)
	fmt.Printf("SUPERVISION_MODE=%s\nCONTROLLER_ROOT=%s\nHEALTH=VERIFIED\n", ctx.Mode, ctx.ExportRoot())
	hosts, err := boatstack.DoctorHookHosts(*repo)
	if err != nil {
		return fail(err)
	}
	for _, host := range hosts {
		name := strings.ToUpper(host)
		fmt.Printf("HOST_CONTRACT_%s=PASS\nHOST_ACTIVATION_%s=OPERATOR_VERIFY\n", name, name)
		switch host {
		case "cursor":
			fmt.Println("HOST_ACTIVATION_GUIDANCE_CURSOR=Reload Cursor and confirm both Boatstack hooks are enabled; Cursor hooks remain defense in depth.")
		case "claude":
			fmt.Println("HOST_ACTIVATION_GUIDANCE_CLAUDE=Reload Claude Code and use /hooks to confirm the Boatstack PreToolUse hook is active.")
		case "codex":
			fmt.Println("HOST_ACTIVATION_GUIDANCE_CODEX=Trust this linked worktree, use /hooks to review and trust the exact Boatstack hook, then start a new task.")
		}
	}
	if update, ok := boatstack.CachedUpdate(*repo); ok {
		fmt.Printf("UPDATE_AVAILABLE=%s\nRELEASE_URL=%s\n", update.LatestVersion, update.ReleaseURL)
	}
	return 0
}

func renderDenialCommand(arguments []string) int {
	flags := flag.NewFlagSet("render-denial", flag.ContinueOnError)
	mode := flags.String("mode", "ansi", "render mode: ansi | plain | markdown")
	host := flags.String("host", "claude", "coding host: claude | codex | cursor | gemini")
	demo := flags.Bool("demo", false, "render a representative set of denials")
	if err := flags.Parse(arguments); err != nil {
		return fail(err)
	}
	if !*demo {
		return fail(fmt.Errorf("render-denial requires --demo (optional: --mode, --host)"))
	}
	fmt.Println(boatstack.DenialDemo(*host, boatstack.ParseRenderMode(*mode)))
	return 0
}

func diagnoseHookCommand(arguments []string) int {
	flags := flag.NewFlagSet("diagnose-hook", flag.ContinueOnError)
	host := flags.String("host", "", "cursor, claude, or codex")
	repo := flags.String("repo", ".", "repository whose installed hook should be probed")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	diagnostic, err := boatstack.DiagnoseHook(*repo, *host)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("HOOK_CONTRACT_%s=%s\nLIVE_HOST_EVENT=NOT_OBSERVED\n", strings.ToUpper(diagnostic.Host), diagnostic.ContractStatus)
	if diagnostic.Host == "cursor" {
		fmt.Println("NEXT=If Cursor still reports HOST_PAYLOAD_MALFORMED, preserve edits and start a new Cursor task; this probe cannot inspect Cursor's live event.")
	} else {
		name := strings.ToUpper(diagnostic.Host[:1]) + diagnostic.Host[1:]
		fmt.Printf("NEXT=If %s still reports HOST_PAYLOAD_MALFORMED, preserve edits and start a new host session; this probe cannot inspect the live event.\n", name)
	}
	return 0
}

func safetyHookCommand(arguments []string) int {
	flags := flag.NewFlagSet("safety-hook", flag.ContinueOnError)
	host := flags.String("host", "", "cursor, claude, or codex")
	repo := flags.String("repo", ".", "repository protected by the hook")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		input = nil
	}
	value, _ := boatstack.HookDecision(boatstack.SafetyHookOptions{Host: *host, Repo: *repo, Input: input})
	if err := emitHookOutput(os.Stdout, *host, value); err != nil {
		return failSafetyHook(fmt.Errorf("cannot emit hook decision: %w", err))
	}
	return 0
}

// ambientSafetyHookCommand is the guard entry for a developer-level (user-scoped)
// hook that runs for every repository. It enforces Boatstack only on managed
// repositories and no-ops everywhere else, so detached activation can install one
// user-level hook without controlling unattached repositories.
func ambientSafetyHookCommand(arguments []string) int {
	flags := flag.NewFlagSet("ambient-safety-hook", flag.ContinueOnError)
	host := flags.String("host", "", "cursor, claude, codex, or gemini")
	repo := flags.String("repo", ".", "repository the coding agent is operating in")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		input = nil
	}
	value, _ := boatstack.AmbientHookDecision(boatstack.SafetyHookOptions{Host: *host, Repo: *repo, Input: input})
	if err := emitHookOutput(os.Stdout, *host, value); err != nil {
		return failSafetyHook(fmt.Errorf("cannot emit hook decision: %w", err))
	}
	return 0
}

func bootstrapSafetyHookCommand(arguments []string) int {
	flags := flag.NewFlagSet("bootstrap-safety-hook", flag.ContinueOnError)
	host := flags.String("host", "", "cursor, claude, or codex")
	repo := flags.String("repo", ".", "worktree protected by the hook")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		input = nil
	}
	if err := boatstack.HydrateWorktree(*repo); err != nil {
		return failSafetyHook(fmt.Errorf("worktree runtime activation failed: %w", err))
	}
	value, _ := boatstack.HookDecision(boatstack.SafetyHookOptions{Host: *host, Repo: *repo, Input: input})
	if err := emitHookOutput(os.Stdout, *host, value); err != nil {
		return failSafetyHook(fmt.Errorf("cannot emit hook decision: %w", err))
	}
	return 0
}

// hydrateRuntimeCommand populates the version-keyed shared runtime slot (and
// this worktree's ignored bin/) from the RUNNING binary, without switching
// branches or touching any committed generated file. The safety guard invokes
// it — via the verified installer's hydrate mode — to self-heal a clone whose
// slot is empty after a version bump or a fresh checkout, so a teammate never
// sees a hard "shared runtime is missing" deny.
func hydrateRuntimeCommand(arguments []string) int {
	flags := flag.NewFlagSet("hydrate-runtime", flag.ContinueOnError)
	repo := flags.String("repo", ".", "worktree whose shared runtime slot should be populated")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if err := boatstack.RunHydrateRuntime(*repo); err != nil {
		return fail(err)
	}
	return 0
}

func checkSafetyCommand(arguments []string) int {
	flags := flag.NewFlagSet("check-safety", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose operational diff should be checked")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	report, err := boatstack.CheckRepositorySafety(*repo)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(report)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	if report.Status != "PASS" {
		return 1
	}
	return 0
}

type MigrateConfigReport struct {
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	FromVersion int    `json:"from_version"`
	ToVersion   int    `json:"to_version"`
	Changed     bool   `json:"changed"`
}

func migrateConfigCommand(arguments []string) int {
	flags := flag.NewFlagSet("migrate-config", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose configuration should be migrated")
	check := flags.Bool("check", false, "dry-run check mode")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	configPath := boatstack.WorkspaceFor(*repo).SourceConfigPath()
	raw, err := os.ReadFile(configPath)
	if err != nil {
		report := MigrateConfigReport{
			Status:  "FAIL",
			Message: fmt.Sprintf("failed to read config: %v", err),
		}
		value, _ := json.Marshal(report)
		fmt.Print(string(value))
		return 1
	}
	upgraded, fromVer, toVer, changed, err := boatstack.MigrateConfigBytes(raw)
	if err != nil {
		report := MigrateConfigReport{
			Status:  "FAIL",
			Message: fmt.Sprintf("migration failed: %v", err),
		}
		value, _ := json.Marshal(report)
		fmt.Print(string(value))
		return 1
	}
	if changed && !*check {
		if err := os.WriteFile(configPath, upgraded, 0o644); err != nil {
			report := MigrateConfigReport{
				Status:  "FAIL",
				Message: fmt.Sprintf("failed to write migrated config: %v", err),
			}
			value, _ := json.Marshal(report)
			fmt.Print(string(value))
			return 1
		}
	}
	report := MigrateConfigReport{
		Status:      "PASS",
		FromVersion: fromVer,
		ToVersion:   toVer,
		Changed:     changed,
	}
	value, err := json.Marshal(report)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	return 0
}

func prContextCommand(arguments []string) int {
	flags := flag.NewFlagSet("pr-context", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose branch should be projected")
	feature := flags.String("feature", "", "managed Boatstack feature slug; omit for evidence-limited ad-hoc mode")
	slice := flags.String("slice", "", "managed delivery slice; redirects to the named active or published-open slice (default: active slice)")
	base := flags.String("base", "", "base branch; defaults to the Boatstack project configuration")
	format := flags.String("format", "json", "json or template")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	context, err := boatstack.PreparePRContext(boatstack.PRContextOptions{Repo: *repo, Feature: *feature, SliceID: *slice, Base: *base})
	if err != nil {
		return fail(err)
	}
	switch *format {
	case "json":
		value, err := boatstack.PRContextJSON(context)
		if err != nil {
			return fail(err)
		}
		fmt.Print(string(value))
	case "template":
		fmt.Print(boatstack.PRPreviewTemplate(context))
	default:
		return fail(fmt.Errorf("pr-context format must be json or template"))
	}
	return 0
}

func checkPRCommand(arguments []string) int {
	flags := flag.NewFlagSet("check-pr", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository containing the PR preview")
	previewPath := flags.String("preview", "", "reviewed pr.md preview")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *previewPath == "" {
		return fail(fmt.Errorf("check-pr requires --preview"))
	}
	preview, context, err := boatstack.CheckPRPreview(*repo, *previewPath)
	if err != nil {
		return fail(err)
	}
	action, url, actionErr := boatstack.RecommendedPRAction(*repo)
	fmt.Printf("PASS: exact PR preview matches the current branch and evidence\nPR_ACTION=%s\nPR_TITLE=%s\nPREVIEW_FINGERPRINT=%s\nCONTEXT_FINGERPRINT=%s\n", action, preview.Title, preview.Fingerprint, context.ContextFingerprint)
	if url != "" {
		fmt.Printf("PR_URL=%s\n", url)
	}
	if actionErr != nil {
		fmt.Printf("PUBLICATION_NOTE=%s\n", actionErr)
	}
	fmt.Printf("--- PR BODY ---\n%s\n--- END PR BODY ---\n", string(boatstack.PRBody(preview)))
	return 0
}

func publishPRCommand(arguments []string) int {
	flags := flag.NewFlagSet("publish-pr", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository containing the PR preview")
	previewPath := flags.String("preview", "", "reviewed pr.md preview")
	fingerprint := flags.String("preview-fingerprint", "", "exact preview fingerprint confirmed by the human")
	action := flags.String("action", "", "open or update")
	autonomy := flags.String("autonomy", "", "fingerprinted autonomy.md receipt authorizing the PR target")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *previewPath == "" || *fingerprint == "" || *action == "" {
		return fail(fmt.Errorf("publish-pr requires --preview, --preview-fingerprint, and --action"))
	}
	feature := ""
	if preview, previewErr := boatstack.ParsePRPreview(*previewPath); previewErr == nil {
		feature = preview.Feature
	}
	guard := boatstack.GuardFlowMove(*repo, feature, boatstack.PublishTransition)
	if !guard.Allow {
		boatstack.RecordFlowTransition(*repo, guard.Transition, guard.From, false)
		return fail(fmt.Errorf("%s", guard.Message))
	}
	url, err := boatstack.PublishPR(boatstack.PRPublishOptions{
		Repo: *repo, PreviewPath: *previewPath, ExpectedFingerprint: *fingerprint, Action: *action,
		AutonomyPath:    *autonomy,
		VisualPublisher: boatstack.SelectVisualPublisher(*repo),
	})
	boatstack.RecordFlowTransition(*repo, boatstack.PublishTransition, guard.From, err == nil)
	if err != nil {
		return fail(err)
	}
	verb := "opened"
	if *action == "update" {
		verb = "updated"
	}
	fmt.Printf("PASS: PR %s without merge authorization\nPR_URL=%s\n", verb, url)

	if update, ok := boatstack.PostShipUpdateNotice(*repo, feature); ok {
		fmt.Printf("UPDATE_AVAILABLE=%s\nUPDATE_RELEASE_URL=%s\n", update.LatestVersion, update.ReleaseURL)
	}
	return 0
}

func workspaceCutCommand(arguments []string) int {
	flags := flag.NewFlagSet("workspace-cut", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to cut the feature workspace in")
	feature := flags.String("feature", "", "feature slug used to derive the branch name")
	branch := flags.String("branch", "", "explicit branch name; overrides --feature derivation")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	result, err := boatstack.CutFeatureWorkspace(boatstack.WorkspaceCutOptions{Repo: *repo, Feature: *feature, Branch: *branch})
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(result)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	if result.VerificationStatus != "VERIFIED" {
		return 1
	}
	return 0
}

func workspaceCleanupCommand(arguments []string) int {
	flags := flag.NewFlagSet("workspace-cleanup", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose finished workspace should be removed")
	branch := flags.String("branch", "", "branch whose workspace should be cleaned up")
	confirm := flags.Bool("confirm", false, "human confirmation to remove the workspace")
	force := flags.Bool("force", false, "override the merge gate and discard uncommitted or unmerged work")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	result, err := boatstack.CleanupFeatureWorkspace(boatstack.WorkspaceCleanupOptions{Repo: *repo, Branch: *branch, Confirm: *confirm, Force: *force})
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(result)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	if result.VerificationStatus == "BLOCKED" {
		return 1
	}
	return 0
}

func workspaceReapCommand(arguments []string) int {
	flags := flag.NewFlagSet("workspace-reap", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose terminal workspaces should be reclaimed")
	confirm := flags.Bool("confirm", false, "operator confirmation to reclaim the merged or abandoned workspaces")
	force := flags.Bool("force", false, "override the merge gate and discard uncommitted or unmerged work")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	result, err := boatstack.ReapWorkspaces(boatstack.WorkspaceReapOptions{Repo: *repo, Confirm: *confirm, Force: *force})
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(result)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	if result.VerificationStatus == "BLOCKED" {
		return 1
	}
	return 0
}

func workspaceStatusCommand(arguments []string) int {
	flags := flag.NewFlagSet("workspace-status", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to inspect")
	branch := flags.String("branch", "", "branch whose workspace should be reported")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	result, err := boatstack.FeatureWorkspaceStatus(*repo, *branch)
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(result)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	return 0
}

func workspaceSyncCommand(arguments []string) int {
	flags := flag.NewFlagSet("workspace-sync", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository containing the branch to synchronize")
	branch := flags.String("branch", "", "local branch to align; defaults to the current branch")
	source := flags.String("source", "", "remote branch to fetch and align to, for example origin/main")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if strings.TrimSpace(*source) == "" {
		return fail(fmt.Errorf("workspace-sync requires --source"))
	}
	result, err := boatstack.SyncWorkspace(boatstack.WorkspaceSyncOptions{Repo: *repo, Branch: *branch, Source: *source})
	if err != nil {
		return fail(err)
	}
	value, err := boatstack.MarshalJSON(result)
	if err != nil {
		return fail(err)
	}
	fmt.Print(string(value))
	if result.Status == "BLOCKED" {
		return 1
	}
	return 0
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: boatstack-helper <attach|detach|detached-status|context|activate|deactivate|init|update|check-update|repair-status|operation-status|prepare-update-pr|publish-update-pr|release-classify|next-patch|export|check-source-plan|planning-write|check-plan|record-approval|record-autonomy|activate-plan|delivery-status|next-status|recovery-status|repair-state|mutation-status|undo|run-preflight|authority-context|record-change|record-journey-results|ignore-delivery|record-delivery-gate|record-pr-visual-evidence|capture-evidence|provision-capability|capability-register|record-pr-visual-publication|attach-evidence|check-safety|migrate-config|safety-hook|ambient-safety-hook|diagnose-hook|render-denial|pr-context|check-pr|publish-pr|workspace-cut|workspace-cleanup|workspace-reap|workspace-status|workspace-sync|flow|retro|insight|doctor|version>")
		return 2
	}
	switch os.Args[1] {
	case "attach":
		return attachCommand(os.Args[2:])
	case "detach":
		return detachCommand(os.Args[2:])
	case "detached-status":
		return detachedStatusCommand(os.Args[2:])
	case "context":
		return contextCommand(os.Args[2:])
	case "activate":
		return activateCommand(os.Args[2:])
	case "deactivate":
		return deactivateCommand(os.Args[2:])
	case "init":
		return initCommand(os.Args[2:])
	case "update":
		return updateCommand(os.Args[2:])
	case "check-update":
		return checkUpdateCommand(os.Args[2:])
	case "repair-status":
		return repairStatusCommand(os.Args[2:])
	case "operation-status":
		return operationStatusCommand(os.Args[2:])
	case "prepare-update-pr":
		return prepareUpdatePRCommand(os.Args[2:])
	case "publish-update-pr":
		return publishUpdatePRCommand(os.Args[2:])
	case "release-classify":
		return releaseClassifyCommand(os.Args[2:])
	case "next-patch":
		return nextPatchCommand(os.Args[2:])
	case "export":
		return exportCommand(os.Args[2:])
	case "check-source-plan":
		return checkSourcePlanCommand(os.Args[2:])
	case "check-plan":
		return checkPlanCommand(os.Args[2:])
	case "planning-write":
		return planningWriteCommand(os.Args[2:])
	case "record-approval":
		return recordApprovalCommand(os.Args[2:])
	case "record-autonomy":
		return recordAutonomyCommand(os.Args[2:])
	case "activate-plan":
		return activatePlanCommand(os.Args[2:])
	case "delivery-status":
		return deliveryStatusCommand(os.Args[2:])
	case "next-status":
		return nextStatusCommand(os.Args[2:])
	case "recovery-status":
		return recoveryStatusCommand(os.Args[2:])
	case "repair-state":
		return repairStateCommand(os.Args[2:])
	case "mutation-status":
		return mutationStatusCommand(os.Args[2:])
	case "undo":
		return undoCommand(os.Args[2:])
	case "run-preflight":
		return runPreflightCommand(os.Args[2:])
	case "authority-context":
		return authorityContextCommand(os.Args[2:])
	case "record-change":
		return recordChangeCommand(os.Args[2:])
	case "record-journey-results":
		return recordJourneyResultsCommand(os.Args[2:])
	case "ignore-delivery":
		return ignoreDeliveryCommand(os.Args[2:])
	case "discard-delivery":
		return discardDeliveryCommand(os.Args[2:])
	case "record-delivery-gate":
		return recordDeliveryGateCommand(os.Args[2:])
	case "record-pr-visual-evidence":
		return recordPRVisualEvidenceCommand(os.Args[2:])
	case "capture-evidence":
		return captureEvidenceCommand(os.Args[2:])
	case "provision-capability":
		return provisionCapabilityCommand(os.Args[2:])
	case "capability-register":
		return capabilityRegisterCommand(os.Args[2:])
	case "record-pr-visual-publication":
		return recordPRVisualPublicationCommand(os.Args[2:])
	case "attach-evidence":
		return attachEvidenceCommand(os.Args[2:])
	case "pr-context":
		return prContextCommand(os.Args[2:])
	case "check-pr":
		return checkPRCommand(os.Args[2:])
	case "publish-pr":
		return publishPRCommand(os.Args[2:])
	case "doctor":
		return doctorCommand(os.Args[2:])
	case "diagnose-hook":
		return diagnoseHookCommand(os.Args[2:])
	case "render-denial":
		return renderDenialCommand(os.Args[2:])
	case "safety-hook":
		return safetyHookCommand(os.Args[2:])
	case "ambient-safety-hook":
		return ambientSafetyHookCommand(os.Args[2:])
	case "bootstrap-safety-hook":
		return bootstrapSafetyHookCommand(os.Args[2:])
	case "hydrate-runtime":
		return hydrateRuntimeCommand(os.Args[2:])
	case "check-safety":
		return checkSafetyCommand(os.Args[2:])
	case "workspace-cut":
		return workspaceCutCommand(os.Args[2:])
	case "workspace-cleanup":
		return workspaceCleanupCommand(os.Args[2:])
	case "workspace-reap":
		return workspaceReapCommand(os.Args[2:])
	case "workspace-status":
		return workspaceStatusCommand(os.Args[2:])
	case "workspace-sync":
		return workspaceSyncCommand(os.Args[2:])
	case "migrate-config":
		return migrateConfigCommand(os.Args[2:])
	case "flow":
		return flowCommand(os.Args[2:])
	case "retro":
		return retroCommand(os.Args[2:])
	case "insight":
		return insightCommand(os.Args[2:])
	case "version":
		fmt.Printf("Boatstack %s (%s)\n", boatstack.Version, boatstack.SourceCommit)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		return 2
	}
}

func main() { os.Exit(run()) }
