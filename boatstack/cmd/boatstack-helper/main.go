package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	boatstack "github.com/operatorstack/boatstack/boatstack"
)

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "BLOCKED:", err)
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
	fmt.Fprintln(os.Stderr, "BLOCKED:", err)
	// Claude Code and Codex both define exit 2 as a blocking PreToolUse error.
	// Exit 1 is non-blocking in Claude and must never represent policy failure.
	return 2
}

func initCommand(arguments []string) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository to initialize")
	binary := flags.String("binary", "", "verified helper binary to install project-locally")
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
	binary := flags.String("binary", "", "verified replacement helper binary")
	yes := flags.Bool("yes", false, "accept the generated-file preview")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	err := boatstack.RunUpdate(boatstack.InitOptions{Repo: *repo, BinaryPath: *binary, Yes: *yes})
	if err != nil {
		return fail(err)
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
	fmt.Printf("PASS: Markdown plan is structurally valid\nPLAN_FINGERPRINT=%s\nSOURCE_PLAN=%s\nSPEC=%s\n", check.Fingerprint, check.SourcePlanPath, check.SpecPath)
	return 0
}

func checkSourcePlanCommand(arguments []string) int {
	flags := flag.NewFlagSet("check-source-plan", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose bounded plan locations should be searched")
	plan := flags.String("plan", "", "optional explicit plan file created by the host Plan mode")
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
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if options.PlanPath == "" || options.ApprovalPath == "" || options.OutDir == "" || options.OutputPath == "" {
		return fail(fmt.Errorf("activate-plan requires --plan, --approval, --out-dir, and --output"))
	}
	if err := boatstack.ActivatePlan(options); err != nil {
		return fail(fmt.Errorf("plan activation failed: %w", err))
	}
	fmt.Printf("PASS: approved Markdown plan activated and locked: %s\n", options.OutputPath)
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
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *plan == "" || *approvedBy == "" || *approvedAt == "" || *fingerprint == "" {
		return fail(fmt.Errorf("record-approval requires --plan, --approved-by, --approved-at, and --fingerprint"))
	}
	if err := boatstack.RecordApproval(boatstack.ApprovalRecordOptions{
		PlanPath: *plan, OutputPath: *output, ApprovedBy: *approvedBy,
		ApprovedAt: *approvedAt, Fingerprint: *fingerprint,
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
	flags.StringVar(&options.SliceID, "slice", "", "active delivery slice id")
	flags.StringVar(&options.Gate, "gate", "", "test or review")
	flags.StringVar(&options.Status, "status", "", "PASS or PASS_WITH_GAPS")
	flags.StringVar(&options.BaseBranch, "base", "", "delivery base branch; defaults from the active slice or project")
	flags.StringVar(&options.EvidencePath, "evidence", "", "current evidence ledger")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if options.Feature == "" || options.SliceID == "" || options.Gate == "" || options.Status == "" {
		return fail(fmt.Errorf("record-delivery-gate requires --feature, --slice, --gate, and --status"))
	}
	receipt, err := boatstack.RecordDeliveryGate(options)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("PASS: %s gate recorded for delivery slice %s\nSLICE=%s\nGATE=%s\nSTATUS=%s\nHEAD_COMMIT=%s\nDIFF_SHA256=%s\n", strings.ToUpper(receipt.Gate), receipt.SliceID, receipt.SliceID, receipt.Gate, receipt.Status, receipt.HeadCommit, receipt.DiffSHA256)
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
	jsonOutput := flags.Bool("json", false, "print the versioned structured status")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	status, err := boatstack.ResolveNext(*repo)
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
		fmt.Print(boatstack.FormatNextStatus(status))
	}
	return 0
}

func runPreflightCommand(arguments []string) int {
	flags := flag.NewFlagSet("run-preflight", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose Git state should be verified before boatstack run")
	jsonOutput := flags.Bool("json", false, "print the versioned structured preflight")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	status := boatstack.CheckRunPreflight(*repo)
	if *jsonOutput {
		value, err := boatstack.MarshalJSON(status)
		if err != nil {
			return fail(err)
		}
		fmt.Print(string(value))
	} else {
		fmt.Printf("Boatstack run preflight: %s\nReason: %s\n", status.VerificationStatus, status.Reason)
	}
	if status.VerificationStatus != "VERIFIED" {
		return 1
	}
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
	flags.StringVar(&options.Classification, "classification", "", "implementation_repair, verification_repair, review_repair, requirement_amendment, or needs_clarification")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if options.Feature == "" || options.Message == "" || options.SourceStage == "" || options.Classification == "" {
		return fail(fmt.Errorf("record-change requires --feature, --message, --source-stage, and --classification"))
	}
	observation, state, err := boatstack.RecordChangeObservation(options)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("PASS: change observation recorded\nOBSERVATION_ID=%s\nCLASSIFICATION=%s\nMODE=%s\nRESUME_STAGE=%s\n", observation.ID, observation.Classification, state.Mode, state.ResumeStage)
	return 0
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
	fmt.Printf("PASS: Boatstack %s installation and generated adapters are healthy\n", boatstack.Version)
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

func prContextCommand(arguments []string) int {
	flags := flag.NewFlagSet("pr-context", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose branch should be projected")
	feature := flags.String("feature", "", "managed Boatstack feature slug; omit for evidence-limited ad-hoc mode")
	slice := flags.String("slice", "", "active managed delivery slice")
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
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *previewPath == "" || *fingerprint == "" || *action == "" {
		return fail(fmt.Errorf("publish-pr requires --preview, --preview-fingerprint, and --action"))
	}
	url, err := boatstack.PublishPR(boatstack.PRPublishOptions{
		Repo: *repo, PreviewPath: *previewPath, ExpectedFingerprint: *fingerprint, Action: *action,
	})
	if err != nil {
		return fail(err)
	}
	verb := "opened"
	if *action == "update" {
		verb = "updated"
	}
	fmt.Printf("PASS: PR %s without merge authorization\nPR_URL=%s\n", verb, url)
	if update, ok := boatstack.PostShipUpdateNotice(*repo); ok {
		fmt.Printf("UPDATE_AVAILABLE=%s\nUPDATE_RELEASE_URL=%s\n", update.LatestVersion, update.ReleaseURL)
	}
	return 0
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: boatstack-helper <init|update|check-update|release-classify|next-patch|export|check-source-plan|planning-write|check-plan|record-approval|activate-plan|delivery-status|next-status|run-preflight|record-change|record-delivery-gate|check-safety|safety-hook|diagnose-hook|pr-context|check-pr|publish-pr|doctor|version>")
		return 2
	}
	switch os.Args[1] {
	case "init":
		return initCommand(os.Args[2:])
	case "update":
		return updateCommand(os.Args[2:])
	case "check-update":
		return checkUpdateCommand(os.Args[2:])
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
	case "activate-plan":
		return activatePlanCommand(os.Args[2:])
	case "delivery-status":
		return deliveryStatusCommand(os.Args[2:])
	case "next-status":
		return nextStatusCommand(os.Args[2:])
	case "run-preflight":
		return runPreflightCommand(os.Args[2:])
	case "record-change":
		return recordChangeCommand(os.Args[2:])
	case "record-delivery-gate":
		return recordDeliveryGateCommand(os.Args[2:])
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
	case "safety-hook":
		return safetyHookCommand(os.Args[2:])
	case "bootstrap-safety-hook":
		return bootstrapSafetyHookCommand(os.Args[2:])
	case "check-safety":
		return checkSafetyCommand(os.Args[2:])
	case "version":
		fmt.Printf("Boatstack %s (%s)\n", boatstack.Version, boatstack.SourceCommit)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		return 2
	}
}

func main() { os.Exit(run()) }
