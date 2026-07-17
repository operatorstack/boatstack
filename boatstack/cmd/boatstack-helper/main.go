package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	boatstack "github.com/operatorstack/boatstack/boatstack"
)

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "BLOCKED:", err)
	return 1
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
		fmt.Printf("PASS: %d generated files match Boatstack %s\n", len(bundle.Files), boatstack.Version)
		return 0
	}
	if *write {
		if err := boatstack.WriteExport(*repo, bundle.Files); err != nil {
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
	return 0
}

func prContextCommand(arguments []string) int {
	flags := flag.NewFlagSet("pr-context", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose branch should be projected")
	feature := flags.String("feature", "", "managed Boatstack feature slug; omit for evidence-limited ad-hoc mode")
	base := flags.String("base", "", "base branch; defaults to the Boatstack project configuration")
	format := flags.String("format", "json", "json or template")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	context, err := boatstack.PreparePRContext(boatstack.PRContextOptions{Repo: *repo, Feature: *feature, Base: *base})
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
	return 0
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: boatstack-helper <init|export|check-source-plan|planning-write|check-plan|record-approval|activate-plan|pr-context|check-pr|publish-pr|doctor|version>")
		return 2
	}
	switch os.Args[1] {
	case "init":
		return initCommand(os.Args[2:])
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
	case "pr-context":
		return prContextCommand(os.Args[2:])
	case "check-pr":
		return checkPRCommand(os.Args[2:])
	case "publish-pr":
		return publishPRCommand(os.Args[2:])
	case "doctor":
		return doctorCommand(os.Args[2:])
	case "version":
		fmt.Printf("Boatstack %s (%s)\n", boatstack.Version, boatstack.SourceCommit)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		return 2
	}
}

func main() { os.Exit(run()) }
