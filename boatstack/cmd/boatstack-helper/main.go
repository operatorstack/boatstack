package main

import (
	"flag"
	"fmt"
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

func compileCommand(arguments []string) int {
	flags := flag.NewFlagSet("compile-plan", flag.ContinueOnError)
	plan := flags.String("plan", "", "approved structured plan")
	outDir := flags.String("out-dir", "", "compiled artifact directory")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *plan == "" || *outDir == "" {
		return fail(fmt.Errorf("compile-plan requires --plan and --out-dir"))
	}
	if err := boatstack.CompilePlanFiles(*plan, *outDir); err != nil {
		return fail(fmt.Errorf("invalid approved plan: %w", err))
	}
	fmt.Printf("PASS: compiled approved plan into %s\n", *outDir)
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

func approveCommand(arguments []string) int {
	flags := flag.NewFlagSet("approve-plan", flag.ContinueOnError)
	options := boatstack.ApprovalOptions{}
	flags.StringVar(&options.SourcePlanPath, "source-plan", "", "plan file created by the host Plan mode")
	flags.StringVar(&options.SpecPath, "spec", "", "approved spec")
	flags.StringVar(&options.PlanPath, "plan", "", "approved structured plan")
	flags.StringVar(&options.TasksPath, "tasks", "", "compiled task graph")
	flags.StringVar(&options.ApprovedBy, "approved-by", "", "human approver")
	flags.StringVar(&options.ApprovedAt, "approved-at", "", "approval timestamp")
	flags.StringVar(&options.SourceCommit, "source-commit", "", "source Git commit")
	flags.StringVar(&options.OutputPath, "output", "", "plan lock path")
	check := flags.Bool("check", false, "verify an existing plan lock")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if options.SourcePlanPath == "" || options.SpecPath == "" || options.PlanPath == "" || options.TasksPath == "" || options.OutputPath == "" {
		return fail(fmt.Errorf("approve-plan requires --source-plan, --spec, --plan, --tasks, and --output"))
	}
	if *check {
		if err := boatstack.CheckApprovalLock(options); err != nil {
			return fail(err)
		}
		fmt.Println("PASS: approved plan lock matches the current artifacts")
		return 0
	}
	if err := boatstack.CreateApprovalLock(options); err != nil {
		return fail(err)
	}
	fmt.Printf("PASS: wrote approved plan lock: %s\n", options.OutputPath)
	return 0
}

func run() int {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: boatstack-helper <init|export|check-source-plan|compile-plan|approve-plan|version>")
		return 2
	}
	switch os.Args[1] {
	case "init":
		return initCommand(os.Args[2:])
	case "export":
		return exportCommand(os.Args[2:])
	case "check-source-plan":
		return checkSourcePlanCommand(os.Args[2:])
	case "compile-plan":
		return compileCommand(os.Args[2:])
	case "approve-plan":
		return approveCommand(os.Args[2:])
	case "version":
		fmt.Printf("Boatstack %s (%s)\n", boatstack.Version, boatstack.SourceCommit)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		return 2
	}
}

func main() { os.Exit(run()) }
