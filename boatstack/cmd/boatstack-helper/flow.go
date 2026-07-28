package main

import (
	"flag"
	"fmt"
	"os"

	boatstack "github.com/operatorstack/boatstack/boatstack"
)

// flowCommand is the read-only entry point for delivery-flow navigation:
// `flow check` gates the owned model, `flow next` advises the lowest-cost next
// move. Both are additive and side-effect free; they change no existing command,
// gate, authority, or exit code.
func flowCommand(arguments []string) int {
	if len(arguments) == 0 {
		fmt.Fprintln(os.Stderr, "usage: boatstack-helper flow <check|next|tasks|frontier|report>")
		return 2
	}
	switch arguments[0] {
	case "check":
		return flowCheckCommand(arguments[1:])
	case "next":
		return flowNextCommand(arguments[1:])
	case "tasks":
		return flowTasksCommand(arguments[1:])
	case "frontier":
		return flowFrontierCommand(arguments[1:])
	case "report":
		return flowReportCommand(arguments[1:])
	default:
		fmt.Fprintln(os.Stderr, "unknown flow subcommand:", arguments[0])
		return 2
	}
}

// flowCheckCommand runs the static conformance + liveness gate over the delivery
// model and exits non-zero on drift. It reads no repository state.
func flowCheckCommand(arguments []string) int {
	flags := flag.NewFlagSet("flow check", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "print the structured check result")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	result := boatstack.FlowCheck()
	if *jsonOutput {
		value, err := boatstack.MarshalJSON(result)
		if err != nil {
			return fail(err)
		}
		fmt.Print(string(value))
	} else {
		fmt.Print(boatstack.FormatFlowCheck(result))
	}
	if !result.OK {
		return 1
	}
	return 0
}

// flowNextCommand advises the lowest-cost next move toward a published delivery.
// It is purely advisory and never fails on flow position — an unresolved flow
// still prints the authoritative recommendation. With --execute (opt-in,
// default-off), it additionally runs any move the driver proves safe and fully
// state-derivable, and prescribes-and-stops at the first human/evidence gate.
func flowNextCommand(arguments []string) int {
	flags := flag.NewFlagSet("flow next", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose delivery flow should be advised")
	feature := flags.String("feature", "", "optional specific managed feature to advise")
	jsonOutput := flags.Bool("json", false, "print the structured advisory")
	execute := flags.Bool("execute", false, "opt in to auto-running safe, state-derivable moves (default off; stops at human/evidence gates)")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	next, err := boatstack.NextControl(*repo, *feature)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		value, marshalErr := boatstack.MarshalJSON(next)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		fmt.Print(string(value))
	} else {
		fmt.Print(boatstack.FormatFlowNext(next))
	}
	if *execute {
		return driveExecute(*repo, *feature, next)
	}
	return 0
}

// driveExecute runs the opt-in execute driver: it takes the oracle's lowest-cost
// edge and runs it only when the driver proves the move is safe and fully
// state-derivable, re-resolving after each executed step. At the first move that
// owes human input, is off the auto-drive allowlist, or when the kill switch is
// set, it prescribes-and-stops. It never fabricates evidence, a gate status, a
// preview fingerprint, or reviewer identity. The loop is bounded as a backstop
// against any cycle in the model.
func driveExecute(repo, feature string, next boatstack.FlowNext) int {
	const maxDriveSteps = 32
	for step := 0; step < maxDriveSteps; step++ {
		decision := boatstack.DecideDrive(next, true, boatstack.FlowDriveKilled())
		switch decision.Action {
		case boatstack.DriveNone:
			fmt.Println("drive: nothing to run —", decision.Reason)
			return 0
		case boatstack.DrivePrescribe:
			fmt.Println("drive: stopping —", decision.Reason)
			if decision.Command != nil {
				fmt.Println("drive: run by hand:", decision.Command.CommandLine())
			}
			return 0
		case boatstack.DriveExecute:
			fmt.Println("drive: running", decision.Command.CommandLine())
			if err := executePrescribed(decision.Command); err != nil {
				return fail(err)
			}
			// Re-resolve from ground truth so the next decision reflects the mutation
			// the executed command committed, never an assumed position.
			resolved, err := boatstack.NextControl(repo, feature)
			if err != nil {
				return fail(err)
			}
			next = resolved
		default:
			return fail(fmt.Errorf("drive: unknown decision %q", decision.Action))
		}
	}
	fmt.Println("drive: step budget exhausted; stopping")
	return 0
}

// executePrescribed is the driver's second, independent gate: even a move the pure
// decision blessed as auto-drivable runs only if an executor is explicitly
// registered here for its verb. Nothing is registered today — every real forward
// move owes human input and is refused by the decision before reaching here — so
// this defends against a future allowlist entry landing without a deliberate,
// reviewed executor. It never synthesizes arguments; it would only ever invoke the
// same verb dispatch a human would run.
func executePrescribed(cmd *boatstack.PrescribedCommand) error {
	switch cmd.Verb {
	// No verbs are registered for auto-execution. Add a case here only together with
	// an allowlist entry in flow_drive.go, and only for a verb whose arguments are
	// fully state-derivable with nothing to fabricate.
	default:
		return fmt.Errorf("no registered auto-executor for verb %q; run it by hand: %s", cmd.Verb, cmd.CommandLine())
	}
}

// flowFrontierCommand renders the cross-delivery frontier dashboard: one row
// per managed delivery slice with its observed position and the actor who owes
// the next step. Strictly read-only — it performs zero writes, including the
// terminal PR-state cache that next/recovery maintain.
// control-law: frontier-reports-never-mutates
func flowFrontierCommand(arguments []string) int {
	flags := flag.NewFlagSet("flow frontier", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose delivery frontier should be reported")
	jsonOutput := flags.Bool("json", false, "print the structured frontier report")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	frontier, err := boatstack.ResolveFrontier(*repo)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		value, marshalErr := boatstack.MarshalJSON(frontier)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		fmt.Print(string(value))
	} else {
		fmt.Print(boatstack.FormatFlowFrontier(frontier))
	}
	return 0
}

// flowTasksCommand renders the active delivery slice's sub-actions from the
// compiled plan task DAG, in dependency order, with the one to start pointed at.
// It is read-only and never fails on flow position — an unresolved slice or an
// unreadable task graph prints its reason rather than a guessed sub-action.
func flowTasksCommand(arguments []string) int {
	flags := flag.NewFlagSet("flow tasks", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose active-slice sub-actions should be listed")
	feature := flags.String("feature", "", "managed Boatstack feature slug")
	jsonOutput := flags.Bool("json", false, "print the structured task ordering")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	tasks, err := boatstack.FlowTasksForActiveSlice(*repo, *feature)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		value, marshalErr := boatstack.MarshalJSON(tasks)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		fmt.Print(string(value))
	} else {
		fmt.Print(boatstack.FormatFlowTasks(tasks))
	}
	return 0
}

// flowReportCommand renders the session's flow-navigation regret and coding-effort
// telemetry from the shadow logs. It is read-only and never fails on an empty
// session — it simply reports zero steps.
func flowReportCommand(arguments []string) int {
	flags := flag.NewFlagSet("flow report", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose flow session should be reported")
	jsonOutput := flags.Bool("json", false, "print the structured report")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	report, err := boatstack.FlowReport(*repo)
	if err != nil {
		return fail(err)
	}
	if *jsonOutput {
		value, marshalErr := boatstack.MarshalJSON(report)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		fmt.Print(string(value))
	} else {
		fmt.Print(boatstack.FormatFlowReport(report))
	}
	return 0
}
