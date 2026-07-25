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
		fmt.Fprintln(os.Stderr, "usage: boatstack-helper flow <check|next|report>")
		return 2
	}
	switch arguments[0] {
	case "check":
		return flowCheckCommand(arguments[1:])
	case "next":
		return flowNextCommand(arguments[1:])
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
// still prints the authoritative recommendation.
func flowNextCommand(arguments []string) int {
	flags := flag.NewFlagSet("flow next", flag.ContinueOnError)
	repo := flags.String("repo", ".", "repository whose delivery flow should be advised")
	feature := flags.String("feature", "", "optional specific managed feature to advise")
	jsonOutput := flags.Bool("json", false, "print the structured advisory")
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
