package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	boatstack "github.com/operatorstack/boatstack/boatstack"
)

func readInsightInput(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func printInsightView(view boatstack.InsightView, jsonOutput bool) int {
	if jsonOutput {
		return emitJSON(view)
	}
	fmt.Printf("Insight %s: %s\n", view.Capture.ID, view.Evaluation.State)
	fmt.Println(view.Evaluation.Reason)
	fmt.Printf("Repository diff: %s\n", view.RepositoryPath)
	return 0
}

func insightCommand(arguments []string) int {
	if len(arguments) == 0 {
		fmt.Fprintln(os.Stderr, "usage: boatstack-helper insight <check|save|list|show|associate|bind|evaluate|frontier|disposition>")
		return 2
	}
	switch arguments[0] {
	case "check":
		flags := flag.NewFlagSet("insight check", flag.ContinueOnError)
		repo := flags.String("repo", ".", "repository whose insight inbox should be checked")
		input := flags.String("input", "-", "capture JSON file, or - for stdin")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		value, err := readInsightInput(*input)
		if err != nil {
			return fail(err)
		}
		result, err := boatstack.CheckInsightCapture(*repo, value)
		if err != nil {
			return fail(err)
		}
		return emitJSON(result)
	case "save":
		flags := flag.NewFlagSet("insight save", flag.ContinueOnError)
		repo := flags.String("repo", ".", "repository whose tracked insight inbox should receive the capture")
		input := flags.String("input", "-", "capture JSON file, or - for stdin")
		nonce := flags.String("preview-nonce", "", "nonce returned by insight check")
		fingerprint := flags.String("preview-fingerprint", "", "fingerprint returned by insight check")
		jsonOutput := flags.Bool("json", false, "print the structured capture")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		value, err := readInsightInput(*input)
		if err != nil {
			return fail(err)
		}
		view, err := boatstack.SaveInsightCapture(*repo, value, *nonce, *fingerprint)
		if err != nil {
			return fail(err)
		}
		return printInsightView(view, *jsonOutput)
	case "list":
		flags := flag.NewFlagSet("insight list", flag.ContinueOnError)
		repo := flags.String("repo", ".", "repository whose captures should be listed")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		views, err := boatstack.ListInsights(*repo)
		if err != nil {
			return fail(err)
		}
		return emitJSON(views)
	case "show":
		flags := flag.NewFlagSet("insight show", flag.ContinueOnError)
		repo := flags.String("repo", ".", "repository whose capture should be shown")
		id := flags.String("id", "", "insight capture id")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		view, err := boatstack.ShowInsight(*repo, *id)
		if err != nil {
			return fail(err)
		}
		return emitJSON(view)
	case "associate":
		flags := flag.NewFlagSet("insight associate", flag.ContinueOnError)
		repo := flags.String("repo", ".", "repository whose capture should be associated")
		id := flags.String("id", "", "insight capture id")
		primary := flags.String("primary-topic", "", "human-confirmed primary feature topic")
		var related stringList
		flags.Var(&related, "related-topic", "related feature topic (repeatable)")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		view, err := boatstack.AssociateInsight(*repo, *id, *primary, related)
		if err != nil {
			return fail(err)
		}
		return emitJSON(view)
	case "bind":
		flags := flag.NewFlagSet("insight bind", flag.ContinueOnError)
		repo := flags.String("repo", ".", "repository whose capture should be bound")
		id := flags.String("id", "", "insight capture id")
		feature := flags.String("feature", "", "managed feature id")
		var criteria stringList
		flags.Var(&criteria, "criterion", "mapped acceptance criterion id (repeatable)")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		view, err := boatstack.BindInsight(*repo, *id, *feature, criteria)
		if err != nil {
			return fail(err)
		}
		return emitJSON(view)
	case "evaluate":
		flags := flag.NewFlagSet("insight evaluate", flag.ContinueOnError)
		repo := flags.String("repo", ".", "repository whose capture should be evaluated")
		id := flags.String("id", "", "insight capture id")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		result, err := boatstack.EvaluateInsight(*repo, *id)
		if err != nil {
			return fail(err)
		}
		return emitJSON(result)
	case "frontier":
		flags := flag.NewFlagSet("insight frontier", flag.ContinueOnError)
		repo := flags.String("repo", ".", "repository whose pending insight frontier should be shown")
		jsonOutput := flags.Bool("json", false, "print the structured frontier")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		report, err := boatstack.InsightFrontier(*repo)
		if err != nil {
			return fail(err)
		}
		if *jsonOutput {
			return emitJSON(report)
		}
		fmt.Print(boatstack.FormatInsightFrontier(report))
		return 0
	case "disposition":
		flags := flag.NewFlagSet("insight disposition", flag.ContinueOnError)
		repo := flags.String("repo", ".", "repository whose capture should be dispositioned")
		id := flags.String("id", "", "insight capture id")
		outcome := flags.String("outcome", "", "completed, deferred, rejected, or duplicate")
		reason := flags.String("reason", "", "human reason, required for non-ready completion and non-complete outcomes")
		duplicateOf := flags.String("duplicate-of", "", "original capture id for duplicate outcomes")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		view, err := boatstack.DisposeInsight(*repo, *id, *outcome, *reason, *duplicateOf)
		if err != nil {
			return fail(err)
		}
		return emitJSON(view)
	default:
		fmt.Fprintln(os.Stderr, "unknown insight subcommand:", arguments[0])
		return 2
	}
}
