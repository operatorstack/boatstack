package boatstack

import (
	"fmt"
	"strings"

	"github.com/operatorstack/boatstack/boatstack/internal/retromine"
)

// RetroInput is one transcript handed to the retro derivation: a name (for
// evidence references and per-file session identity) and its raw content.
// The derivation layer takes bytes, never paths — every capability the miner
// lacks (filesystem, network, subprocess, clock) stays lacking here; only
// the CLI boundary reads files, from operator-supplied paths only.
// control-law: retro-derivation-is-offline-and-deterministic
type RetroInput struct {
	Name    string
	Content []byte
}

// RetroDerive parses every input with the named adapter format ("" sniffs
// per file: events | claudecode | plaintext) and mines the combined events
// for recurring operator instructions, classified into typed-gap proposals.
// It proposes only: no file is written, no state is touched, no command is
// run, and nothing is enforced — promotion is always a reviewed change made
// by hand. control-law: retro-proposes-never-enforces
func RetroDerive(format string, inputs []RetroInput) (retromine.Report, error) {
	events := []retromine.Event{}
	for _, input := range inputs {
		parsed, err := retromine.ParseTranscript(format, input.Name, input.Content)
		if err != nil {
			return retromine.Report{}, err
		}
		events = append(events, parsed...)
	}
	return retromine.BuildReport(events), nil
}

// FormatRetroReport renders the derivation for a human reviewer.
func FormatRetroReport(report retromine.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Retro derivation: %d event(s) scanned, %d from the operator.\n",
		report.EventsScanned, report.OperatorEvents)
	if len(report.Proposals) == 0 && len(report.Unclassified) == 0 {
		b.WriteString("No recurring operator instruction found across sessions. Nothing to promote.\n")
		return b.String()
	}
	for i, proposal := range report.Proposals {
		fmt.Fprintf(&b, "\n%d. [%s] seen %d time(s) across %d session(s)\n", i+1,
			proposal.GapType, proposal.Occurrences, len(proposal.Sessions))
		fmt.Fprintf(&b, "   Instruction: %q\n", proposal.Exemplar)
		fmt.Fprintf(&b, "   Promote it: %s\n", proposal.SuggestedShape)
	}
	for _, cluster := range report.Unclassified {
		fmt.Fprintf(&b, "\n?. [unclassified] seen %d time(s) across %d session(s): %q\n",
			cluster.Occurrences, len(cluster.Sessions), cluster.Exemplar)
		b.WriteString("   Recurs, but no gap type matched; review it by hand. No proposal is generated.\n")
	}
	b.WriteString("\nDerivation proposes; it never enforces. Promote a proposal by hand through the normal reviewed delivery flow.\n")
	return b.String()
}
