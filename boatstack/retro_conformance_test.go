package boatstack

// control-law: retro-proposes-never-enforces
//
// `retro derive` closes the loop the whole program serves: a recurring
// operator instruction is steady-state error, and the remedy is a TYPED
// promotion — an observation, verb, setpoint, or guard — never a saved
// prompt and never an automatic change. The derivation therefore only ever
// produces a report: it writes no file, mutates no state, runs no command,
// and an unclassifiable recurrence is surfaced without a proposal
// (fail-closed). Below the CLI's read-only file loading, the pipeline is
// capability-free (pinned structurally in the retromine conformance suite).
//
// Test classes: positive (each gap type classifies from planted recurring
// phrasing, with a suggested typed shape), negative (an unmatched recurrence
// lands in unclassified with zero proposals), bypass (derivation leaves the
// filesystem byte-identical), failure-state (empty input → empty report;
// a malformed transcript is a typed error, not a partial report).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func neutralTranscript(instruction string) []byte {
	var b strings.Builder
	for _, session := range []string{"s1", "s2", "s3"} {
		fmt.Fprintf(&b, `{"session_id":%q,"role":"operator","text":%q}`+"\n", session, instruction)
		fmt.Fprintf(&b, `{"session_id":%q,"role":"agent","text":"done"}`+"\n", session)
	}
	return []byte(b.String())
}

// Positive: each gap type classifies from its phrasing and carries a
// suggested typed shape.
func TestRetroDeriveClassifiesEachGapType(t *testing.T) {
	for _, test := range []struct {
		instruction string
		wantGap     string
	}{
		{"never force push to the main branch", "missing_guard"},
		{"watch the checks until every one passes then merge", "missing_setpoint"},
		{"check the status of the deployment pipeline", "missing_observation"},
		{"run the full test suite again please", "missing_verb"},
	} {
		t.Run(test.wantGap, func(t *testing.T) {
			report, err := RetroDerive("events", []RetroInput{{Name: "t.jsonl", Content: neutralTranscript(test.instruction)}})
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Proposals) != 1 {
				t.Fatalf("proposals = %#v, want exactly one", report.Proposals)
			}
			proposal := report.Proposals[0]
			if proposal.GapType != test.wantGap {
				t.Fatalf("gap = %q, want %q", proposal.GapType, test.wantGap)
			}
			if proposal.Occurrences != 3 || len(proposal.Sessions) != 3 {
				t.Fatalf("unexpected recurrence evidence: %#v", proposal)
			}
			if proposal.SuggestedShape == "" {
				t.Fatal("proposal carries no suggested typed shape")
			}
			rendered := FormatRetroReport(report)
			if !strings.Contains(rendered, test.wantGap) || !strings.Contains(rendered, "never enforces") {
				t.Fatalf("rendering incomplete:\n%s", rendered)
			}
		})
	}
}

// Negative: a recurrence the lexicon cannot place is surfaced as
// unclassified and generates zero proposals.
func TestUnclassifiedRecurrenceGeneratesNoProposal(t *testing.T) {
	report, err := RetroDerive("events", []RetroInput{{Name: "t.jsonl", Content: neutralTranscript("the quarterly numbers look pretty good overall")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Proposals) != 0 {
		t.Fatalf("unclassified recurrence produced proposals: %#v", report.Proposals)
	}
	if len(report.Unclassified) != 1 {
		t.Fatalf("unclassified recurrence not surfaced: %#v", report)
	}
	if rendered := FormatRetroReport(report); !strings.Contains(rendered, "No proposal is generated") {
		t.Fatalf("unclassified recurrence not explained:\n%s", rendered)
	}
}

// Bypass: derivation leaves the filesystem byte-identical — it consumes
// bytes and produces a report, nothing else.
func TestRetroDeriveWritesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, neutralTranscript("never force push to the main branch"), 0o644); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RetroDerive("", []RetroInput{{Name: path, Content: content}}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("derivation changed the directory: %v", entries)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(content) {
		t.Fatal("derivation modified its input")
	}
}

// Failure-state: empty input yields an empty report; a malformed transcript
// is a typed error, never a partial report.
func TestRetroDeriveFailureStates(t *testing.T) {
	report, err := RetroDerive("events", nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.EventsScanned != 0 || len(report.Proposals) != 0 {
		t.Fatalf("empty input produced content: %#v", report)
	}
	if rendered := FormatRetroReport(report); !strings.Contains(rendered, "Nothing to promote") {
		t.Fatalf("empty report not explained:\n%s", rendered)
	}
	if _, err := RetroDerive("events", []RetroInput{{Name: "bad.jsonl", Content: []byte("not json\n")}}); err == nil {
		t.Fatal("malformed transcript accepted")
	}
}
