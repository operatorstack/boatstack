package boatstack

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDenialRenderModesCarryTheSameInformation(t *testing.T) {
	d := denialFor("claude", SafetyFinding{Category: "workflow-state-tamper"})

	plain := d.Render(RenderPlain)
	if !strings.Contains(plain, "Blocked by Boatstack") || !strings.Contains(plain, "managed runtime authority") {
		t.Fatalf("plain missing badge/qualifier: %q", plain)
	}
	if !strings.Contains(plain, ".git/boatstack/") || !strings.Contains(plain, "publish-update-pr") {
		t.Fatalf("plain dropped guidance detail: %q", plain)
	}
	if !strings.Contains(plain, "Nothing was written") {
		t.Fatalf("plain missing reassurance: %q", plain)
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain must not contain ANSI: %q", plain)
	}

	md := d.Render(RenderMarkdown)
	if !strings.Contains(md, "**Blocked by Boatstack**") {
		t.Fatalf("markdown missing bold badge: %q", md)
	}

	ansi := d.Render(RenderANSI)
	if !strings.Contains(ansi, "\x1b[") || !strings.Contains(ansi, "Blocked by Boatstack") {
		t.Fatalf("ansi missing escape/badge: %q", ansi)
	}
}

// control-law: prescriptive-closure-every-stage-names-a-runnable-command — a
// planning-state plan-gate denial names the owned authoring channel
// (planning-write), not just the cleanup verb, in every render mode.
func TestPlanningPhaseBypassDenialNamesOwnedChannel(t *testing.T) {
	finding := SafetyFinding{
		Category: "workflow-phase-bypass", Source: "planning-state",
		WorkflowStage: "INVALID_STATE", NextOperation: "repair-state",
		BlockingFeature: "sample-feature",
	}
	d := denialFor("claude", finding)
	for mode, name := range map[RenderMode]string{RenderPlain: "plain", RenderMarkdown: "markdown", RenderANSI: "ansi"} {
		out := d.Render(mode)
		if !strings.Contains(out, "repair-state") {
			t.Fatalf("%s denial dropped the recovery verb: %q", name, out)
		}
		if !strings.Contains(out, "planning-write --repo . --feature sample-feature --artifact <name>") {
			t.Fatalf("%s denial must name the owned planning-write channel: %q", name, out)
		}
	}
	// A non-planning finding must not gain the planning guidance.
	other := denialFor("claude", SafetyFinding{Category: "workflow-phase-bypass", Source: "delivery-state", WorkflowStage: "BUILD", NextOperation: "plan-gate"}).Render(RenderPlain)
	if strings.Contains(other, "planning-write") {
		t.Fatalf("non-planning denial must not mention planning-write: %q", other)
	}
}

func TestDenialReassuranceIsCategoryAware(t *testing.T) {
	// A blocked-before-effect denial reassures that nothing was written.
	tamper := denialFor("claude", SafetyFinding{Category: "workflow-state-tamper"}).Render(RenderPlain)
	if !strings.Contains(tamper, "Nothing was written") {
		t.Fatalf("protected denial should reassure nothing was written: %q", tamper)
	}
	// An already-succeeded operation did have an effect — it must NOT claim nothing happened.
	done := denialFor("claude", SafetyFinding{Category: "operation-already-succeeded", OperationID: "op_1"}).Render(RenderPlain)
	if strings.Contains(done, "Nothing was written") {
		t.Fatalf("already-succeeded must not claim nothing was written: %q", done)
	}
	if !strings.Contains(done, "earlier run's result stands") {
		t.Fatalf("already-succeeded should note the result stands: %q", done)
	}
}

func TestDenialPreservesMachineTokens(t *testing.T) {
	d := denialFor("cursor", SafetyFinding{Category: "malformed-tool-input", Reason: "empty-command"})
	plain := d.Render(RenderPlain)
	if !strings.Contains(plain, "HOST_PAYLOAD_MALFORMED:empty-command") {
		t.Fatalf("malformed denial dropped machine token: %q", plain)
	}
	if d.Severity != SeverityAdvisory {
		t.Fatalf("malformed input should be advisory, got %v", d.Severity)
	}
}

func TestDenialGenericFallbackNamesCategory(t *testing.T) {
	d := denialFor("claude", SafetyFinding{Category: "database-destruction"})
	plain := d.Render(RenderPlain)
	if !strings.Contains(plain, "(database-destruction)") {
		t.Fatalf("generic denial should name its category: %q", plain)
	}
	if !strings.Contains(plain, "Nothing was written") {
		t.Fatalf("generic protected denial should reassure: %q", plain)
	}
}

func TestColorEnabledHonorsEnvAndDevice(t *testing.T) {
	// A regular file is not a character device → auto = no color.
	f, err := os.CreateTemp(t.TempDir(), "notty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	t.Setenv("NO_COLOR", "")
	t.Setenv("BOATSTACK_COLOR", "")
	if colorEnabled(f) {
		t.Fatal("auto mode on a regular file must not enable color")
	}
	t.Setenv("BOATSTACK_COLOR", "always")
	if !colorEnabled(f) {
		t.Fatal("BOATSTACK_COLOR=always must force color")
	}
	t.Setenv("BOATSTACK_COLOR", "never")
	if colorEnabled(f) {
		t.Fatal("BOATSTACK_COLOR=never must disable color")
	}
	t.Setenv("BOATSTACK_COLOR", "")
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(f) {
		t.Fatal("NO_COLOR must disable color")
	}
}

func TestFormatBlockedPreservesPlainPrefix(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "notty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	t.Setenv("NO_COLOR", "")
	t.Setenv("BOATSTACK_COLOR", "")
	if got := FormatBlocked(f, "check-plan requires --plan"); got != "BLOCKED: check-plan requires --plan" {
		t.Fatalf("non-terminal must keep the literal BLOCKED: prefix, got %q", got)
	}
	t.Setenv("BOATSTACK_COLOR", "always")
	got := FormatBlocked(f, "boom")
	if !strings.Contains(got, "\x1b[") || !strings.Contains(got, "Blocked") || !strings.Contains(got, "boom") {
		t.Fatalf("terminal form should be an ANSI pill carrying the message, got %q", got)
	}
}

func TestStructuredDenialObjectAndRichGate(t *testing.T) {
	finding := SafetyFinding{Category: "workflow-state-tamper"}
	obj := denialFor("claude", finding).Structured()
	if obj["category"] != "workflow-state-tamper" || obj["severity"] != "protected" || obj["badge"] == "" {
		t.Fatalf("structured object missing fields: %+v", obj)
	}

	// Rich object is gated off by default; the flat reason stays complete.
	t.Setenv("BOATSTACK_DENIAL_RICH", "")
	out, err := structuredHookDeny(".", "claude", finding)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("deny output is not valid JSON: %v", err)
	}
	hook, _ := decoded["hookSpecificOutput"].(map[string]any)
	if hook["permissionDecisionReason"] == "" {
		t.Fatal("flat reason must always be present")
	}
	if _, present := hook["boatstackDenial"]; present {
		t.Fatal("structured object must be OFF by default")
	}

	// Opt-in adds the object while keeping the flat reason.
	t.Setenv("BOATSTACK_DENIAL_RICH", "1")
	out, err = structuredHookDeny(".", "claude", finding)
	if err != nil {
		t.Fatal(err)
	}
	decoded = map[string]any{}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	hook, _ = decoded["hookSpecificOutput"].(map[string]any)
	if _, present := hook["boatstackDenial"]; !present {
		t.Fatal("BOATSTACK_DENIAL_RICH=1 must add the structured object")
	}
	if hook["permissionDecisionReason"] == "" {
		t.Fatal("flat reason must remain complete alongside the structured object")
	}
}

func TestDenialDemoRendersAllSamples(t *testing.T) {
	out := DenialDemo("claude", RenderPlain)
	for _, want := range []string{"managed runtime authority", "plan gate", "HOST_PAYLOAD_MALFORMED", "already completed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("demo missing %q in:\n%s", want, out)
		}
	}
}

// Guard-script generation stays syntactically emittable and the plain fallback
// keeps the exact human message (asserts the ANSI helper did not alter wording).
func TestGuardScriptPlainMessagesUnchanged(t *testing.T) {
	script := string(guardShellScript())
	for _, want := range []string{
		"could not resolve the repository; denying tool execution.",
		"shared runtime checksum is invalid; rerun the verified tagged installer.",
		"bs_deny ", "bs_color",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("guard script missing %q", want)
		}
	}
}
