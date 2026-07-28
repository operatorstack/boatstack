package retromine

// control-law: retro-derivation-is-offline-and-deterministic
//
// The recurrence miner is a pure sensor: no network, no subprocesses, no
// filesystem, no clocks, no randomness — inputs arrive as bytes and readers,
// and identical inputs in ANY order produce byte-identical output. The
// import-purity test enforces the capability boundary structurally, the
// determinism test enforces the output contract, and the adapter tests pin
// the lossy-projection rule: skip what you do not understand, error on what
// fails to parse.
//
// Test classes: positive (a planted instruction repeated 3× across 2
// sessions clusters; both host adapters project equivalent content to
// equivalent events), negative (2 occurrences or 1 session never clusters;
// acknowledgements below the token floor never enter the pool), relation
// (input order does not change the report), failure-state (malformed JSONL
// is a typed error naming the line, never a silent partial parse), bypass
// (the package imports grant no I/O capability at all).

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func operatorEvent(session string, text string) Event {
	return Event{Source: "test", SessionID: session, Role: RoleOperator, Text: text}
}

func plantedEvents() []Event {
	return []Event{
		operatorEvent("session-a", "make the pr, monitor the checks, and merge when green"),
		operatorEvent("session-a", "add a logout button to the settings page"),
		{Source: "test", SessionID: "session-a", Role: RoleAgent, Text: "make the pr, monitor the checks, and merge when green"},
		operatorEvent("session-b", "please make the PR, monitor the checks and merge when green"),
		operatorEvent("session-b", "ok"),
		operatorEvent("session-c", "make the pr monitor the checks and merge when it is green"),
	}
}

// Positive: three occurrences across three sessions cluster; the agent's
// identical text and the sub-floor acknowledgement never enter the pool.
func TestRecurringInstructionClusters(t *testing.T) {
	clusters := DetectRecurrence(plantedEvents())
	if len(clusters) != 1 {
		t.Fatalf("clusters = %#v, want exactly one", clusters)
	}
	cluster := clusters[0]
	if cluster.Occurrences != 3 {
		t.Fatalf("occurrences = %d, want 3", cluster.Occurrences)
	}
	if !reflect.DeepEqual(cluster.Sessions, []string{"session-a", "session-b", "session-c"}) {
		t.Fatalf("sessions = %v", cluster.Sessions)
	}
	if !strings.Contains(cluster.Exemplar, "monitor the checks") {
		t.Fatalf("exemplar lost the instruction: %q", cluster.Exemplar)
	}
}

// Negative: two occurrences, or three inside one session, are not recurrence.
func TestBelowThresholdNeverClusters(t *testing.T) {
	twoOccurrences := []Event{
		operatorEvent("session-a", "make the pr, monitor the checks, and merge when green"),
		operatorEvent("session-b", "make the pr, monitor the checks, and merge when green"),
	}
	if clusters := DetectRecurrence(twoOccurrences); len(clusters) != 0 {
		t.Fatalf("two occurrences clustered: %#v", clusters)
	}
	oneSession := []Event{
		operatorEvent("session-a", "make the pr, monitor the checks, and merge when green"),
		operatorEvent("session-a", "make the pr, monitor the checks, and merge when green"),
		operatorEvent("session-a", "make the pr, monitor the checks, and merge when green"),
	}
	if clusters := DetectRecurrence(oneSession); len(clusters) != 0 {
		t.Fatalf("single-session repetition clustered: %#v", clusters)
	}
}

// Relation: input order does not change the report.
func TestDetectionIsOrderIndependent(t *testing.T) {
	events := plantedEvents()
	reversed := make([]Event, 0, len(events))
	for i := len(events) - 1; i >= 0; i-- {
		reversed = append(reversed, events[i])
	}
	forward := DetectRecurrence(events)
	backward := DetectRecurrence(reversed)
	if !reflect.DeepEqual(forward, backward) {
		t.Fatalf("order changed the report:\n%#v\n---\n%#v", forward, backward)
	}
}

// Positive: normalization strips pasted logs, so the same instruction with a
// different fenced payload is the same instruction shape.
func TestFencedPayloadDoesNotSplitClusters(t *testing.T) {
	events := []Event{
		operatorEvent("s1", "fix the failing windows shard\n```\nlog A\n```"),
		operatorEvent("s2", "fix the failing windows shard\n```\ncompletely different log B\n```"),
		operatorEvent("s3", "fix the failing windows shard please"),
	}
	if clusters := DetectRecurrence(events); len(clusters) != 1 {
		t.Fatalf("payload variance split the cluster: %#v", clusters)
	}
}

// Positive + relation: the two host adapters project equivalent content into
// equivalent instruction streams — the miner is agent-agnostic by contract.
func TestAdaptersProjectEquivalentContent(t *testing.T) {
	claudeLines := strings.Join([]string{
		`{"type":"user","sessionId":"cc-1","message":{"role":"user","content":"make the pr, monitor the checks, and merge when green"}}`,
		`{"type":"assistant","sessionId":"cc-1","message":{"role":"assistant","content":[{"type":"text","text":"On it."}]}}`,
		`{"type":"user","sessionId":"cc-1","message":{"role":"user","content":[{"type":"tool_result","content":"exit 0"}]}}`,
		`{"type":"summary","summary":"irrelevant"}`,
	}, "\n")
	fromClaude, err := ParseTranscript("", "cc-1.jsonl", []byte(claudeLines))
	if err != nil {
		t.Fatal(err)
	}
	plain := "User: make the pr, monitor the checks, and merge when green\nAgent: On it.\n"
	fromPlain, err := ParseTranscript("", "plain.txt", []byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	pick := func(events []Event) []string {
		out := []string{}
		for _, e := range events {
			if e.Role == RoleOperator {
				out = append(out, normalizeInstruction(e.Text))
			}
		}
		return out
	}
	if !reflect.DeepEqual(pick(fromClaude), pick(fromPlain)) {
		t.Fatalf("adapters disagree:\n%v\n---\n%v", pick(fromClaude), pick(fromPlain))
	}
	// The tool result projected as tool, never operator.
	for _, e := range fromClaude {
		if e.Role == RoleOperator && strings.Contains(e.Text, "exit 0") {
			t.Fatalf("tool payload classified as operator: %#v", e)
		}
	}
}

// Failure-state: malformed JSONL is a typed error naming the line — never a
// silent partial parse.
func TestMalformedLinesAreTypedErrors(t *testing.T) {
	if _, err := ParseTranscript(FormatNeutral, "bad.jsonl", []byte(`{"role":"operator","session_id":"s","text":"x"}`+"\nnot json\n")); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("malformed neutral line not surfaced: %v", err)
	}
	if _, err := ParseTranscript(FormatClaudeCode, "bad.jsonl", []byte("{broken\n")); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("malformed claudecode line not surfaced: %v", err)
	}
	if _, err := ParseTranscript(FormatNeutral, "bad.jsonl", []byte(`{"role":"wizard","session_id":"s","text":"x"}`+"\n")); err == nil || !strings.Contains(err.Error(), "unknown role") {
		t.Fatalf("unknown role accepted: %v", err)
	}
}

// Bypass: the package's non-test imports grant no I/O capability — no
// network, no subprocesses, no filesystem, no clocks, no randomness. The
// boundary is structural, not behavioral.
func TestPackageImportsGrantNoCapabilities(t *testing.T) {
	disallowed := []string{"net", "os", "syscall", "time", "math/rand", "crypto/rand", "path/filepath", "io/ioutil"}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			for _, banned := range disallowed {
				if path == banned || strings.HasPrefix(path, banned+"/") {
					t.Fatalf("%s imports %s — the miner must stay capability-free", name, path)
				}
			}
		}
	}
}

// Golden determinism over the synthetic fixtures: parse both fixture
// transcripts, mine them together, and pin the whole report.
func TestFixtureGoldenReport(t *testing.T) {
	events := []Event{}
	for _, fixture := range []string{"session-alpha.jsonl", "session-beta.txt"} {
		content, err := os.ReadFile(filepath.Join("testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseTranscript("", fixture, content)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, parsed...)
	}
	clusters := DetectRecurrence(events)
	if len(clusters) != 1 {
		t.Fatalf("fixture clusters = %#v", clusters)
	}
	got := fmt.Sprintf("%dx across %v: %s", clusters[0].Occurrences, clusters[0].Sessions, clusters[0].Normalized)
	want := "3x across [cc-alpha session-beta.txt]: open the pr watch ci until every check passes then merge it"
	if got != want {
		t.Fatalf("golden drift:\n got %q\nwant %q", got, want)
	}
}
