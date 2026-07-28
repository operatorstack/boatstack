package main

import (
	"flag"
	"fmt"
	"os"

	boatstack "github.com/operatorstack/boatstack/boatstack"
)

// retroCommand is the derive-only entry point for transcript mining. The CLI
// boundary owns the ONLY I/O in the pipeline: it reads the operator-supplied
// paths and prints the report to stdout. Below this boundary the derivation
// is capability-free (no filesystem, network, subprocess, or clock), and
// nothing anywhere in the pipeline writes, mutates state, or runs a command.
// control-law: retro-proposes-never-enforces
func retroCommand(arguments []string) int {
	if len(arguments) == 0 || arguments[0] != "derive" {
		fmt.Fprintln(os.Stderr, "usage: boatstack-helper retro derive --input <transcript> [--input <transcript> ...] [--format events|claudecode|plaintext] [--json]")
		return 2
	}
	flags := flag.NewFlagSet("retro derive", flag.ContinueOnError)
	var inputs stringList
	flags.Var(&inputs, "input", "transcript file to mine (repeatable)")
	format := flags.String("format", "", "transcript format: events, claudecode, or plaintext (default: sniff per file)")
	jsonOutput := flags.Bool("json", false, "print the structured derivation report")
	if err := flags.Parse(arguments[1:]); err != nil {
		return 2
	}
	inputs = append(inputs, flags.Args()...)
	if len(inputs) == 0 {
		fmt.Fprintln(os.Stderr, "retro derive requires at least one --input transcript; Boatstack never scans for transcripts on its own")
		return 2
	}
	loaded := make([]boatstack.RetroInput, 0, len(inputs))
	for _, path := range inputs {
		content, err := os.ReadFile(path)
		if err != nil {
			return fail(err)
		}
		loaded = append(loaded, boatstack.RetroInput{Name: path, Content: content})
	}
	report, err := boatstack.RetroDerive(*format, loaded)
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
		fmt.Print(boatstack.FormatRetroReport(report))
	}
	return 0
}

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string { return fmt.Sprint([]string(*s)) }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}
