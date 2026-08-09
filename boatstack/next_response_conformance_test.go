package boatstack

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// control-law: response-contract-is-helper-rendered
// (cross-reference: banner-hides-internal-machinery)
//
// The "one next action" decision was always deterministic — ResolveNext plus
// the prescription layer — but its user-facing rendering lived only as prose
// in the exported operation instructions, re-derived by every agent on every
// turn. RenderNextStatusResponse compiles that rendering into the helper:
// one friendly outcome line and exactly one "### Next step" block carrying
// the identical command line `flow next` prescribes. These tests hold the
// renderer to the phrase vocabulary, the single-next-step contract, parity
// with the prescription layer, and the banner law; and they pin the exported
// skill prose to POINTING at the renderer rather than restating a state table.

// machineTokens must never appear as status prose in a rendered response.
// Lines that carry the runnable command (`.product-loop/boatstack …`) are the one
// legitimate exception — the verb IS the next step there.
var machineTokens = regexp.MustCompile(`DRAFT_PLAN|APPROVED|POLICY_READY|NOT_INITIALIZED|INVALID_STATE|AMBIGUOUS|NOT_STARTED|TEST_PASSED|REVIEW_PASSED|PR_PREVIEW|FEATURE_COMPLETE|repair-state|discard-delivery|plan-gate|ship-gate|review-gate|auto-plan`)

func renderedResponse(t *testing.T, repo string) (NextStatus, string) {
	t.Helper()
	status, err := ResolveNext(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	output, err := RenderNextStatusResponse(repo, status)
	if err != nil {
		t.Fatal(err)
	}
	return status, output
}

// Positive: per-stage fixtures render the friendly phrase and exactly one
// "### Next step"; when a command is prescribable the block carries it.
func TestResponseContractPerStage(t *testing.T) {
	t.Run("not_started", func(t *testing.T) {
		repo := nextTestRepo(t)
		status, output := renderedResponse(t, repo)
		assertResponseShape(t, status, output)
		if !strings.Contains(output, "Run: .product-loop/boatstack check-source-plan") {
			t.Fatalf("NOT_STARTED must carry the prescribed command: %q", output)
		}
	})

	t.Run("draft_plan", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "demo")
		status, output := renderedResponse(t, repo)
		assertResponseShape(t, status, output)
		if !strings.Contains(output, "Run: .product-loop/boatstack check-plan") {
			t.Fatalf("DRAFT_PLAN must carry the prescribed command: %q", output)
		}
		if !strings.Contains(output, "Then: ") {
			t.Fatalf("DRAFT_PLAN must carry the follow-up: %q", output)
		}
	})

	t.Run("approved", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "demo")
		if err := os.WriteFile(filepath.Join(repo, ".product-loop", "features", "demo", "approval.md"), []byte("approved\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		status, output := renderedResponse(t, repo)
		assertResponseShape(t, status, output)
		if !strings.Contains(output, "Run: .product-loop/boatstack activate-plan") {
			t.Fatalf("APPROVED must carry the prescribed command: %q", output)
		}
	})

	t.Run("build", func(t *testing.T) {
		repo, feature := activateTwoSliceDelivery(t)
		status, err := ResolveNext(repo, feature)
		if err != nil {
			t.Fatal(err)
		}
		output, err := RenderNextStatusResponse(repo, status)
		if err != nil {
			t.Fatal(err)
		}
		assertResponseShape(t, status, output)
		if !strings.Contains(output, "Run: .product-loop/boatstack record-delivery-gate") {
			t.Fatalf("BUILD must carry the oracle-prescribed command: %q", output)
		}
	})

	t.Run("ambiguous_lists_candidates", func(t *testing.T) {
		repo := nextTestRepo(t)
		writeSavedFeaturePlan(t, repo, "plan-one")
		writeSavedFeaturePlan(t, repo, "plan-two")
		status, output := renderedResponse(t, repo)
		assertResponseShape(t, status, output)
		if strings.Contains(output, "Run: ") {
			t.Fatalf("AMBIGUOUS must not fabricate a command: %q", output)
		}
		for _, candidate := range []string{"plan-one", "plan-two"} {
			if !strings.Contains(output, "- "+candidate) {
				t.Fatalf("candidates must be listed: %q", output)
			}
		}
	})
}

func assertResponseShape(t *testing.T, status NextStatus, output string) {
	t.Helper()
	if got := strings.Count(output, "### Next step"); got != 1 {
		t.Fatalf("response must carry exactly one next step, got %d: %q", got, output)
	}
	phrase := friendlyPhrase(status)
	if !strings.Contains(strings.ToLower(output), strings.ToLower(phrase)) {
		t.Fatalf("response must carry the friendly phrase %q: %q", phrase, output)
	}
}

// Negative: machine stage names and operation codes never appear as status
// prose — only command lines may name helper verbs.
func TestResponseHidesMachineTokens(t *testing.T) {
	fixtures := map[string]func(t *testing.T) string{
		"not_started": func(t *testing.T) string { return nextTestRepo(t) },
		"draft_plan": func(t *testing.T) string {
			repo := nextTestRepo(t)
			writeSavedFeaturePlan(t, repo, "demo")
			return repo
		},
		"ambiguous": func(t *testing.T) string {
			repo := nextTestRepo(t)
			writeSavedFeaturePlan(t, repo, "plan-one")
			writeSavedFeaturePlan(t, repo, "plan-two")
			return repo
		},
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			_, output := renderedResponse(t, fixture(t))
			for _, line := range strings.Split(output, "\n") {
				if strings.Contains(line, ".product-loop/boatstack") {
					continue // the runnable command line is the legitimate exception
				}
				if match := machineTokens.FindString(line); match != "" {
					t.Fatalf("machine token %q leaked into status prose: %q", match, line)
				}
			}
		})
	}
}

// Relation: the response's command line is the identical CommandLine() the
// flow prescription layer renders — one decision, two surfaces, no drift.
func TestResponseCommandMatchesFlowPrescription(t *testing.T) {
	repo := nextTestRepo(t)
	writeSavedFeaturePlan(t, repo, "demo")

	status, output := renderedResponse(t, repo)
	next, err := nextControlFromStatus(repo, status)
	if err != nil {
		t.Fatal(err)
	}
	if next.Prescribed == nil {
		t.Fatal("fixture must prescribe a command")
	}
	if !strings.Contains(output, "Run: "+next.Prescribed.CommandLine()) {
		t.Fatalf("response and flow prescription drifted:\nresponse: %q\nprescribed: %q", output, next.Prescribed.CommandLine())
	}
	if !strings.Contains(FormatFlowNext(next), "Run: "+next.Prescribed.CommandLine()) {
		t.Fatal("FormatFlowNext no longer renders the same command line — parity broken")
	}
}

// Bypass: the exported boatstack-next instruction points at the helper-rendered
// contract and no longer restates the per-stage decision table in prose — the
// prose channel cannot silently reintroduce a second decision surface.
func TestExportedNextInstructionDefersToRenderer(t *testing.T) {
	config := testConfig()
	raw, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildExportBundle(".boatstack-project.json", config, raw, "boatstack")
	if err != nil {
		t.Fatal(err)
	}
	inspected := 0
	for path, content := range bundle.Files {
		if !strings.Contains(path, "boatstack-next") {
			continue
		}
		inspected++
		text := string(content)
		if !strings.Contains(text, "--format response") {
			t.Fatalf("%s must point at the helper-rendered response contract", path)
		}
		for _, restated := range []string{"Distinguish NOT_STARTED", "FEATURE_COMPLETE, which is reserved"} {
			if strings.Contains(text, restated) {
				t.Fatalf("%s restates the state table the helper now renders: %q", path, restated)
			}
		}
	}
	if inspected == 0 {
		t.Fatal("no exported boatstack-next instruction found — the bypass guarantee would be vacuous")
	}
}

// Failure-state: rendering is read-only — the repository is byte-identical
// after resolving and rendering the response.
func TestResponseRenderingIsReadOnly(t *testing.T) {
	repo := nextTestRepo(t)
	writeSavedFeaturePlan(t, repo, "demo")
	before, err := exec.Command("git", "-C", repo, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = renderedResponse(t, repo)
	after, err := exec.Command("git", "-C", repo, "status", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("rendering mutated the repository: before=%q after=%q", before, after)
	}
}
