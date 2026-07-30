package boatstack

import (
	"fmt"
	"os"
	"strings"
)

// Boatstack denials describe a guardrail, not a crash. A denial is captured once
// as a structured value and rendered four ways so every surface — each coding host
// and the raw terminal — shows the calmest treatment it can render, while the flat
// text stays a complete fallback everywhere:
//
//   - Plain    : multi-line text (default hook reason; safe on every host)
//   - Markdown : light inline markup for hosts that render it
//   - ANSI     : soft-coral pill + reassurance for a real terminal (CLI, guards)
//   - Structured: a machine object for a host that adopts rich denial rendering
//
// Nothing here carries secrets: a Denial holds only category slugs, fixed guidance,
// and bounded finding fields, mirroring SafetyFinding's secret-free contract.

// Severity selects the calm color family. Protected = a guardrail stopped a
// protected effect (coral); Advisory = recoverable / retry / input problem (amber).
type Severity int

const (
	SeverityProtected Severity = iota
	SeverityAdvisory
	SeverityInfo
)

func (s Severity) slug() string {
	switch s {
	case SeverityAdvisory:
		return "advisory"
	case SeverityInfo:
		return "info"
	default:
		return "protected"
	}
}

// RenderMode chooses how a Denial becomes text.
type RenderMode int

const (
	RenderPlain RenderMode = iota
	RenderMarkdown
	RenderANSI
)

// Denial is the single structured description of a blocked action.
type Denial struct {
	Category    string   // machine slug, e.g. "workflow-state-tamper"
	Badge       string   // "Blocked by Boatstack"
	Qualifier   string   // "protected path" | "managed runtime authority" | ""
	Severity    Severity //
	Detail      string   // guidance; may contain `code` spans
	Reassurance string   // "Nothing was written; your files are untouched." (empty if an effect occurred)
	Hint        string   // recovery command, e.g. "boatstack-helper diagnose-hook"
	// Options is the denial's computed solution set: the admissible commands
	// from exactly the position the finding describes, so a weaker model picks
	// a legal move instead of retrying the blocked one. Derived from the same
	// declarations the guard enforces; renders as a short "You can:" list and
	// rides in full on the structured payload.
	// control-law: solution-set-derives-from-guard-declarations
	Options          []PrescribedCommand
	OptionsTruncated bool
	// OwnerVerbs names the verbs that own a protected path (state-tamper
	// denials), derived from the state-ownership map. Named, never compiled
	// into runnable commands — their full arguments are not derivable here.
	OwnerVerbs []string
	// Escalated marks a denial that has repeated past the ledger threshold:
	// the rendering lifts the pick-list cap to the full set and prescribes a
	// fresh diagnostic. More corrective information, never more severity —
	// what is denied never changes.
	// control-law: repeated-denials-escalate-to-solutions
	Escalated   bool
	RepeatCount int
}

// optionTextLimit is the pick-list cap for the text renderings: compact by
// default, the full structured cap once the denial has escalated.
func (d Denial) optionTextLimit() int {
	if d.Escalated {
		return solutionSetCap
	}
	return solutionSetTextCap
}

// escalationLine is the repeat notice with the fresh-probe prescription; empty
// until the denial escalates.
func (d Denial) escalationLine() string {
	if !d.Escalated {
		return ""
	}
	return fmt.Sprintf("This denial repeated %d times. Run: boatstack-helper doctor --repo .", d.RepeatCount)
}

// --- ANSI palette (truecolor; matches the approved mockup) -------------------

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"

	// soft coral #e59280 / amber #e6b566 / calm gray #8b8f98 / pill ink #17181c
	fgCoral = "\x1b[38;2;229;146;128m"
	bgCoral = "\x1b[48;2;229;146;128m"
	fgAmber = "\x1b[38;2;230;181;102m"
	bgAmber = "\x1b[48;2;230;181;102m"
	fgGray  = "\x1b[38;2;139;143;152m"
	fgInk   = "\x1b[38;2;23;24;28m"
	fgCode  = "\x1b[38;2;199;205;215m"
)

func (d Denial) sevFG() string {
	if d.Severity == SeverityAdvisory {
		return fgAmber
	}
	return fgCoral
}

func (d Denial) sevBG() string {
	if d.Severity == SeverityAdvisory {
		return bgAmber
	}
	return bgCoral
}

// Render turns a Denial into text for the given mode.
func (d Denial) Render(mode RenderMode) string {
	badge := d.Badge
	if badge == "" {
		badge = "Blocked by Boatstack"
	}
	switch mode {
	case RenderMarkdown:
		return d.renderMarkdown(badge)
	case RenderANSI:
		return d.renderANSI(badge)
	default:
		return d.renderPlain(badge)
	}
}

// optionLines renders the solution set as at most `limit` numbered command
// lines, plus an overflow note. Shared by the three text renderers so every
// surface shows the same picks.
// control-law: solution-set-derives-from-guard-declarations
func (d Denial) optionLines(limit int) []string {
	if len(d.Options) == 0 {
		return nil
	}
	shown := d.Options
	if len(shown) > limit {
		shown = shown[:limit]
	}
	lines := make([]string, 0, len(shown)+1)
	for i, option := range shown {
		lines = append(lines, fmt.Sprintf("  %d) %s", i+1, option.CommandLine()))
	}
	hidden := len(d.Options) - len(shown)
	if d.OptionsTruncated || hidden > 0 {
		lines = append(lines, "  (more legal moves: run boatstack-helper next-status)")
	}
	return lines
}

func (d Denial) renderPlain(badge string) string {
	var b strings.Builder
	head := badge
	if d.Qualifier != "" {
		head += " · " + d.Qualifier
	}
	b.WriteString(head)
	if d.Detail != "" {
		b.WriteString("\n\n")
		b.WriteString(d.Detail)
	}
	if d.Reassurance != "" {
		b.WriteString("\n\n↳ ")
		b.WriteString(d.Reassurance)
	}
	if len(d.OwnerVerbs) > 0 {
		b.WriteString("\n\nThis path is owned by: " + strings.Join(d.OwnerVerbs, ", ") + ".")
	}
	if lines := d.optionLines(d.optionTextLimit()); len(lines) > 0 {
		b.WriteString("\n\nYou can:\n")
		b.WriteString(strings.Join(lines, "\n"))
	}
	if escalation := d.escalationLine(); escalation != "" {
		b.WriteString("\n\n" + escalation)
	}
	if d.Hint != "" {
		b.WriteString("\n\nFalse positive? run: ")
		b.WriteString(d.Hint)
	}
	return b.String()
}

func (d Denial) renderMarkdown(badge string) string {
	var b strings.Builder
	b.WriteString("**" + badge + "**")
	if d.Qualifier != "" {
		b.WriteString(" · " + d.Qualifier)
	}
	if d.Detail != "" {
		b.WriteString("\n\n" + d.Detail)
	}
	if d.Reassurance != "" {
		b.WriteString("\n\n↳ _" + d.Reassurance + "_")
	}
	if len(d.OwnerVerbs) > 0 {
		b.WriteString("\n\nThis path is owned by: `" + strings.Join(d.OwnerVerbs, "`, `") + "`.")
	}
	if lines := d.optionLines(d.optionTextLimit()); len(lines) > 0 {
		b.WriteString("\n\nYou can:\n")
		for _, line := range lines {
			b.WriteString("\n" + line)
		}
	}
	if escalation := d.escalationLine(); escalation != "" {
		b.WriteString("\n\n" + escalation)
	}
	if d.Hint != "" {
		b.WriteString("\n\nFalse positive? run `" + d.Hint + "`")
	}
	return b.String()
}

func (d Denial) renderANSI(badge string) string {
	var b strings.Builder
	// pill: severity background + dark ink, self-contained contrast on any terminal
	b.WriteString(d.sevBG() + fgInk + ansiBold + " ⊘ " + badge + " " + ansiReset)
	if d.Qualifier != "" {
		b.WriteString(" " + d.sevFG() + ansiDim + d.Qualifier + ansiReset)
	}
	if d.Detail != "" {
		b.WriteString("\n\n" + ansiCode(d.Detail))
	}
	if d.Reassurance != "" {
		b.WriteString("\n" + fgGray + "↳ " + d.Reassurance + ansiReset)
	}
	if len(d.OwnerVerbs) > 0 {
		b.WriteString("\n" + fgGray + "this path is owned by: " + ansiReset + fgCode + strings.Join(d.OwnerVerbs, ", ") + ansiReset)
	}
	if lines := d.optionLines(d.optionTextLimit()); len(lines) > 0 {
		b.WriteString("\n" + fgGray + "you can:" + ansiReset)
		for _, line := range lines {
			b.WriteString("\n" + fgCode + line + ansiReset)
		}
	}
	if escalation := d.escalationLine(); escalation != "" {
		b.WriteString("\n" + fgGray + escalation + ansiReset)
	}
	if d.Hint != "" {
		b.WriteString("\n" + fgGray + ansiDim + "false positive? run " + ansiReset + fgCode + d.Hint + ansiReset)
	}
	return b.String()
}

// ansiCode dims the surrounding text and brightens inline `code` spans.
func ansiCode(s string) string {
	parts := strings.Split(s, "`")
	var b strings.Builder
	for i, part := range parts {
		if i%2 == 1 {
			b.WriteString(fgCode + part + ansiReset)
		} else {
			b.WriteString(part)
		}
	}
	return b.String()
}

// Structured is the machine object for a host that adopts rich denial rendering.
// It is emitted only when denialRichEnabled() is set, and the flat reason string
// is always populated alongside it as the fallback.
func (d Denial) Structured() map[string]any {
	badge := d.Badge
	if badge == "" {
		badge = "Blocked by Boatstack"
	}
	out := map[string]any{
		"schema_version": 1,
		"category":       d.Category,
		"badge":          badge,
		"severity":       d.Severity.slug(),
	}
	if d.Qualifier != "" {
		out["qualifier"] = d.Qualifier
	}
	if d.Detail != "" {
		out["detail"] = d.Detail
	}
	if d.Reassurance != "" {
		out["reassurance"] = d.Reassurance
	}
	if d.Hint != "" {
		out["hint"] = d.Hint
	}
	// Additive keys only — schema_version stays 1; a consumer that ignores them
	// loses nothing (the flat reason string already carries the capped picks).
	// control-law: solution-set-derives-from-guard-declarations
	if len(d.Options) > 0 {
		options := make([]map[string]any, 0, len(d.Options))
		for _, option := range d.Options {
			row := map[string]any{
				"verb":         option.Verb,
				"command_line": option.CommandLine(),
				"transition":   string(option.Transition),
			}
			if len(option.Args) > 0 {
				row["args"] = option.Args
			}
			if len(option.RequiresHumanInput) > 0 {
				row["requires_human_input"] = option.RequiresHumanInput
			}
			options = append(options, row)
		}
		out["options"] = options
		if d.OptionsTruncated {
			out["options_truncated"] = true
		}
	}
	if len(d.OwnerVerbs) > 0 {
		out["owner_verbs"] = d.OwnerVerbs
	}
	if d.Escalated {
		out["escalated"] = true
		out["repeat_count"] = d.RepeatCount
	}
	return out
}

// --- environment gating ------------------------------------------------------

// colorEnabled reports whether ANSI styling should be emitted to f. Honors
// BOATSTACK_COLOR=always|never|auto (default auto), NO_COLOR, TERM=dumb, and
// otherwise requires f to be a character device (a real terminal). stdlib only.
func colorEnabled(f *os.File) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BOATSTACK_COLOR"))) {
	case "always", "1", "true", "yes", "on":
		return true
	case "never", "0", "false", "no", "off":
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// denialRichEnabled reports whether the structured denial object should be added
// to a host's hook decision. Default off: no host documents tolerating unknown
// keys, so we opt in explicitly (the flat reason string is always the fallback).
func denialRichEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BOATSTACK_DENIAL_RICH"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// renderModeForFile picks ANSI when color is enabled for f, else Plain. Used by
// the terminal surfaces (CLI errors, guard fallbacks routed through Go).
func renderModeForFile(f *os.File) RenderMode {
	if colorEnabled(f) {
		return RenderANSI
	}
	return RenderPlain
}

// FormatBlocked renders a CLI "BLOCKED" error line for the given stream. On a
// color-capable terminal it is a soft-coral pill; otherwise it is the literal
// "BLOCKED: <msg>" plain form, so scripts and tests that match that prefix are
// unaffected when output is piped or captured.
func FormatBlocked(f *os.File, msg string) string {
	if colorEnabled(f) {
		return bgCoral + fgInk + ansiBold + " ⊘ Blocked " + ansiReset + " " + msg
	}
	return "BLOCKED: " + msg
}

// ParseRenderMode maps a flag value to a RenderMode ("ansi"|"plain"|"markdown").
func ParseRenderMode(value string) RenderMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ansi", "color", "terminal":
		return RenderANSI
	case "markdown", "md":
		return RenderMarkdown
	default:
		return RenderPlain
	}
}

// DenialDemo renders a representative set of denials for the render-denial --demo
// command, so an operator can see the plain/markdown/ANSI treatments directly.
func DenialDemo(host string, mode RenderMode) string {
	samples := []SafetyFinding{
		{Category: "workflow-state-tamper"},
		{Category: "filesystem-destruction"},
		{Category: "workflow-phase-bypass", BlockingFeature: "checkout-flow", WorkflowStage: "PLAN_PENDING", NextOperation: "plan-gate"},
		{Category: "malformed-tool-input", Reason: "empty-command"},
		{Category: "operation-already-succeeded", OperationID: "op_9f2c", OperationState: "SUCCEEDED", AttemptNumber: 1},
	}
	var b strings.Builder
	for i, finding := range samples {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(denialWithOptions(".", host, finding).Render(mode))
	}
	return b.String()
}

// denialWithOptions composes the pure finding→Denial mapping with the
// enumerated solution set for the finding's position. denialFor stays pure
// (DenialDemo and tests use it directly); the hook deny paths call this so
// every real denial carries its picks.
// control-law: solution-set-derives-from-guard-declarations
func denialWithOptions(repo, host string, finding SafetyFinding) Denial {
	d := denialFor(host, finding)
	set := enumerateDenialSolutions(repo, host, finding)
	d.Options = set.Options
	d.OptionsTruncated = set.Truncated
	if finding.Category == "workflow-state-tamper" {
		d.OwnerVerbs = tamperOwnerVerbs(repo, finding.AttemptedPath)
	}
	if finding.RepeatCount >= denialEscalationThreshold {
		d.Escalated = true
		d.RepeatCount = finding.RepeatCount
	}
	return d
}

const reassureUntouched = "Nothing was written; your files are untouched."

// denialFor maps a SafetyFinding to a structured Denial. It preserves every
// piece of information the previous flat messages carried (guidance, machine
// tokens like HOST_PAYLOAD_MALFORMED, and the interpolated recovery context),
// and adds the calm framing: a badge, a qualifier, a severity, and — when the
// action was blocked before any effect — a reassurance line.
func denialFor(host string, finding SafetyFinding) Denial {
	d := Denial{Category: finding.Category, Badge: "Blocked by Boatstack", Severity: SeverityProtected}

	switch finding.Category {
	case "malformed-tool-input":
		name := strings.ToUpper(strings.TrimSpace(host))
		if name == "" {
			name = "HOST"
		}
		d.Severity = SeverityAdvisory
		d.Qualifier = "unreadable tool event"
		d.Detail = "Boatstack could not inspect the " + name + " hook event (HOST_PAYLOAD_MALFORMED:" + finding.Reason +
			"). No unsafe operation was detected; execution is denied because the intended command or tool call is unavailable. Retry once with an explicit non-empty command. If this repeats, stop shell and tool retries and preserve current edits."
		if strings.EqualFold(host, "cursor") {
			d.Detail += " Start a new Cursor task and run `.product-loop/bin/boatstack-helper diagnose-hook --host cursor --repo .` from an external terminal. Do not reinstall Boatstack unless it separately reports a missing, drifted, unsafe, or checksum-invalid runtime."
		} else {
			d.Detail += " Run `.product-loop/bin/boatstack-helper diagnose-hook --host " + strings.ToLower(host) + " --repo .` from an external terminal before changing the installation."
		}
		return d

	case "workflow-state-invalid":
		d.Severity = SeverityAdvisory
		d.Qualifier = "delivery state unverified"
		d.Detail = "Publication is denied because managed delivery state cannot be verified. Re-run the active Boatstack operation or repair the installation before publishing."
		d.Reassurance = "Nothing was published."
		return d

	case "workflow-state-tamper":
		d.Qualifier = "managed runtime authority"
		d.Detail = "Change `.git/boatstack/` only through the command that owns it — a build, test, review, or ship transition for delivery state, `publish-update-pr` for a version update, or `workspace-reap` to reclaim a finished worktree and its runtime state."
		d.Reassurance = "Nothing was written; your runtime state is unchanged."
		d.Hint = "boatstack-helper diagnose-hook"
		return d

	case "workflow-phase-bypass":
		target := "the saved Boatstack plan"
		if finding.BlockingFeature != "" {
			target = fmt.Sprintf("Boatstack feature %q", finding.BlockingFeature)
		}
		next := finding.NextOperation
		if next == "" {
			next = "repair-state"
		}
		attempted := ""
		if finding.AttemptedPath != "" {
			attempted = " Attempted path: " + finding.AttemptedPath + "."
		}
		d.Qualifier = "plan gate"
		d.Detail = fmt.Sprintf("Product mutation is denied because %s is at %s.%s Continue with `%s`; unrelated task completions do not authorize implementation.", target, finding.WorkflowStage, attempted, next)
		// A planning-state denial must name the owned authoring channel, not just
		// the cleanup verb — otherwise the corrective move (planning-write) is
		// discoverable only by failing again.
		// control-law: prescriptive-closure-every-stage-names-a-runnable-command
		if finding.Source == "planning-state" {
			slug := "<feature>"
			if finding.BlockingFeature != "" {
				slug = finding.BlockingFeature
			}
			d.Detail += fmt.Sprintf(" Planning Markdown is authored through the owned channel: `boatstack-helper planning-write --repo . --feature %s --artifact <name>` with the document on stdin — never a raw host write into `.product-loop/features/`.", slug)
		}
		d.Reassurance = reassureUntouched
		return d

	case "workflow-publication-bypass":
		target := "the active managed delivery"
		if finding.BlockingFeature != "" {
			target = fmt.Sprintf("managed delivery %q", finding.BlockingFeature)
		}
		relation := ""
		switch finding.BranchRelation {
		case "unrelated":
			relation = " It is unrelated to the current branch."
		case "ambiguous":
			relation = " More than one delivery may be blocking publication."
		}
		context := publicationRecoveryContext(finding)
		d.Qualifier = "publication authority"
		d.Detail = target + " still owns publication authority." + relation + context + " Resolve the reported change through the managed recovery path; do not repeat this push or PR mutation manually."
		d.Reassurance = "No push or pull request was made."
		return d

	case "workflow-visual-evidence-missing":
		target := "this delivery"
		if finding.BlockingFeature != "" {
			target = fmt.Sprintf("feature %q", finding.BlockingFeature)
		}
		d.Qualifier = "visual evidence is owed"
		d.Detail = "PR publication is blocked until required visual evidence is current for " + target + "."
		if reason := strings.TrimSpace(finding.Reason); reason != "" {
			d.Detail += " Automatic capture reported: " + reason + "."
		}
		d.Detail += " Boatstack captures the plan's approved scenarios itself once a repository command is registered; declare pr_visual_evidence not_relevant (with a reason) only for a genuinely nonvisual change."
		d.Reassurance = "No pull request was created or updated."
		return d

	case "operation-in-flight":
		d.Severity = SeverityAdvisory
		d.Qualifier = "already supervised"
		d.Detail = "Boatstack is already supervising this exact operation." + operationContext(finding) + " Wait for its completion event; do not launch it again."
		d.Reassurance = "The original operation is still running."
		d.Hint = "boatstack-helper operation-status"
		return d

	case "operation-already-succeeded":
		d.Severity = SeverityInfo
		d.Qualifier = "already completed"
		d.Detail = "Boatstack already observed this exact operation succeed." + operationContext(finding) + " Continue from the resulting repository state instead of repeating it."
		d.Reassurance = "The earlier run's result stands."
		return d

	case "operation-reconciliation-required":
		d.Severity = SeverityAdvisory
		d.Qualifier = "needs reconciliation"
		d.Detail = "Boatstack cannot yet distinguish success from an interrupted response." + operationContext(finding) + " Reconcile the expected postcondition with operation-status before any retry."
		d.Hint = "boatstack-helper operation-status"
		return d

	case "operation-retry-exhausted":
		d.Severity = SeverityAdvisory
		d.Qualifier = "retry budget spent"
		d.Detail = "Boatstack exhausted the persistent retry budget for this operation." + operationContext(finding) + " Preserve current state and use the reported manual recovery; do not repeat the tool call."
		return d

	case "git-history-destruction":
		d.Qualifier = "history-destructive git"
		d.Detail = "Use the project-local workspace-sync operation to checkpoint current state and align the exact branch; do not scan delivery artifacts or retry the destructive command."
		d.Reassurance = "Nothing was written; your Git history is intact."
		return d

	case "workspace-sync-bypass":
		d.Qualifier = "unverified workspace sync"
		d.Detail = "Invoke only the exact project-local workspace-sync helper for the current repository."
		d.Reassurance = reassureUntouched
		return d

	case "filesystem-destruction":
		d.Qualifier = "recursive deletion"
		d.Detail = "Recursive deletion of a broad or protected path is denied. To reclaim a finished managed worktree and its branch, use `workspace-reap` (or single-feature `workspace-cleanup`); Boatstack removes them through its own sanctioned actuator. For any other path, preserve current state and use fix-forward recovery — destructive deletion is operator-only outside the agent workflow."
		d.Reassurance = reassureUntouched
		return d
	}

	// operation-* fallthrough (operation-state-invalid and any other operation-*)
	if strings.HasPrefix(finding.Category, "operation-") {
		d.Severity = SeverityAdvisory
		d.Qualifier = "operation state unverified"
		d.Detail = "Boatstack could not verify the durable operation state." + operationContext(finding) + " Inspect operation-status before retrying."
		d.Hint = "boatstack-helper operation-status"
		return d
	}

	// Generic protected-effect denial: database/filesystem/infrastructure/recovery
	// destruction, unbounded mutation, external-resource destruction, entrypoint
	// safety, unsupported host, unresolved repository, and anything new.
	d.Qualifier = "protected effect"
	d.Detail = fmt.Sprintf("Boatstack denied an irreversible operation (%s). Preserve the current state and use read-only diagnosis or fix-forward recovery; destructive recovery is operator-only outside the agent workflow.", finding.Category)
	d.Reassurance = reassureUntouched
	return d
}

func operationContext(finding SafetyFinding) string {
	if finding.OperationID == "" && finding.OperationState == "" && finding.AttemptNumber == 0 {
		return ""
	}
	return fmt.Sprintf(" operation=%s state=%s attempt=%d.", finding.OperationID, finding.OperationState, finding.AttemptNumber)
}

func publicationRecoveryContext(finding SafetyFinding) string {
	var parts []string
	if finding.BlockingSlice != "" {
		parts = append(parts, "slice="+finding.BlockingSlice)
	}
	if finding.BranchRelation != "" {
		parts = append(parts, "relation="+finding.BranchRelation)
	}
	if finding.ParentDelivery != "" {
		parts = append(parts, "parent="+finding.ParentDelivery)
	}
	if finding.NextOperation != "" {
		parts = append(parts, "next="+finding.NextOperation)
	}
	if len(parts) == 0 {
		return ""
	}
	return " Recovery context: " + strings.Join(parts, " ") + "."
}
