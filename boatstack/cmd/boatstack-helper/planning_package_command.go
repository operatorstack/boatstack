package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
	"github.com/operatorstack/boatstack/boatstack/distribution"
	softwareflow "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery"
	planningpackage "github.com/operatorstack/boatstack/boatstack/flow/softwaredelivery/planningpackage"
)

func runFlowPlanningPackage(arguments []string) error {
	if len(arguments) == 0 || arguments[0] != "verify" {
		return fmt.Errorf("unknown planning-package action")
	}
	flags := flag.NewFlagSet("flow planning-package verify", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var repository, deliveryID, packageFingerprint, format string
	var all, requireApproval, requireCurrent bool
	flags.StringVar(&repository, "repo", ".", "repository root")
	flags.StringVar(&deliveryID, "delivery", "", "delivery identity")
	flags.StringVar(&packageFingerprint, "package", "", "full package fingerprint")
	flags.StringVar(&format, "format", "text", "text or json")
	flags.BoolVar(&all, "all", false, "verify all canonical packages")
	flags.BoolVar(&requireApproval, "require-approval", false, "require an exact approval")
	flags.BoolVar(&requireCurrent, "require-current-program", false, "require the current checked Flow")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("unsupported format %q", format)
	}
	repository, err := filepath.Abs(repository)
	if err != nil {
		return err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return err
	}
	if all == (deliveryID != "" || packageFingerprint != "") {
		return fmt.Errorf("select either --all or both --delivery and --package")
	}
	if !all && (deliveryID == "" || packageFingerprint == "") {
		return fmt.Errorf("--delivery and --package are required together")
	}
	var current *planningpackage.CurrentProgram
	if value, currentErr := loadCurrentPlanningProgram(context.Background(), repository); currentErr == nil {
		current = &value
	} else if requireCurrent {
		return currentErr
	}
	var identities [][2]string
	if all {
		identities, err = planningpackage.Enumerate(repository)
	} else {
		identities = [][2]string{{deliveryID, packageFingerprint}}
	}
	if err != nil {
		return err
	}
	results := make([]planningpackage.Result, 0, len(identities))
	failed := false
	for _, identity := range identities {
		result := planningpackage.Verify(repository, identity[0], identity[1], current)
		results = append(results, result)
		if result.Integrity != planningpackage.Valid || result.Contract != planningpackage.Valid || requireApproval && result.Approval != planningpackage.Valid || requireCurrent && result.CurrentProgram != planningpackage.Match {
			failed = true
		}
	}
	if format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		var payload any = results
		if !all {
			payload = results[0]
		}
		if err := encoder.Encode(payload); err != nil {
			return err
		}
	} else {
		for _, result := range results {
			fmt.Fprintf(os.Stdout, "%s/%s integrity=%s contract=%s approval=%s current_program=%s semantic_correctness=%s origin_authenticity=%s\n", result.DeliveryID, result.PackageFingerprint, result.Integrity, result.Contract, result.Approval, result.CurrentProgram, result.SemanticCorrectness, result.OriginAuthenticity)
		}
	}
	if failed {
		return fmt.Errorf("one or more planning packages failed required verification")
	}
	return nil
}

func loadCurrentPlanningProgram(ctx context.Context, repository string) (planningpackage.CurrentProgram, error) {
	path, err := resolveCheckArtifact(repository, "")
	if err != nil {
		return planningpackage.CurrentProgram{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return planningpackage.CurrentProgram{}, err
	}
	artifact, err := controlprogram.LoadArtifact(bytes.NewReader(raw))
	if err != nil {
		return planningpackage.CurrentProgram{}, err
	}
	resolver, err := softwareflow.NewResolver(ctx)
	if err != nil {
		return planningpackage.CurrentProgram{}, err
	}
	compiled, err := checkArtifactForCurrentProject(repository, artifact, resolver)
	if err != nil {
		return planningpackage.CurrentProgram{}, err
	}
	definition, err := softwareflow.NewDefinition(compiled, resolver)
	if err != nil {
		return planningpackage.CurrentProgram{}, err
	}
	program, err := distribution.ProgramForRepository(ctx, distribution.RepositoryProgramRequest{Repository: repository}, definition)
	if err != nil {
		return planningpackage.CurrentProgram{}, err
	}
	workByID := map[string]controlprogram.WorkContract{}
	for _, work := range compiled.Document.Work {
		workByID[work.ID] = work
	}
	for _, transition := range compiled.Document.Transitions {
		if transition.ID != softwareflow.PlanningPackageAdmit {
			continue
		}
		work, ok := workByID[transition.Work]
		if !ok {
			return planningpackage.CurrentProgram{}, fmt.Errorf("current planning-package work is missing")
		}
		runtime, err := softwareflow.RuntimeWorkContract(work)
		if err != nil {
			return planningpackage.CurrentProgram{}, err
		}
		planOutput := ""
		for _, binding := range transition.Parameters {
			if binding.Parameter == "plan_output" && binding.Producer.Binding != nil {
				planOutput = strings.TrimPrefix(binding.Producer.Binding.Reference, "software-delivery/planning-package-plan-output/")
			}
		}
		if planOutput == "" {
			return planningpackage.CurrentProgram{}, fmt.Errorf("current planning-package plan output is missing")
		}
		return planningpackage.CurrentProgram{ProgramFingerprint: program.Fingerprint(), WorkContractFingerprint: runtime.Fingerprint, PlanOutput: planOutput}, nil
	}
	return planningpackage.CurrentProgram{}, fmt.Errorf("current Flow has no planning-package admission")
}
