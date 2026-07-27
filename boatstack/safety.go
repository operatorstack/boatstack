package boatstack

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SafetyFinding is intentionally small and secret-free. The guard reports the
// class and a stable explanation, never the full command or tool arguments.
type SafetyFinding struct {
	Category               string `json:"category"`
	Reason                 string `json:"reason"`
	Source                 string `json:"source,omitempty"`
	BlockingFeature        string `json:"blocking_feature,omitempty"`
	BlockingSlice          string `json:"blocking_slice,omitempty"`
	BranchRelation         string `json:"branch_relation,omitempty"`
	NextOperation          string `json:"next_operation,omitempty"`
	ParentDelivery         string `json:"parent_delivery,omitempty"`
	WorkflowStage          string `json:"workflow_stage,omitempty"`
	AttemptedPath          string `json:"attempted_path,omitempty"`
	OperationID            string `json:"operation_id,omitempty"`
	OperationState         string `json:"operation_state,omitempty"`
	AttemptNumber          int    `json:"attempt_number,omitempty"`
	ReconciliationRequired bool   `json:"reconciliation_required,omitempty"`
}

type SafetyReport struct {
	Status   string          `json:"status"`
	Findings []SafetyFinding `json:"findings"`
}

type SafetyHookOptions struct {
	Host  string
	Repo  string
	Input []byte
}

type hookDecodeError struct {
	code string
}

func (err hookDecodeError) Error() string { return err.code }

func malformedHookInput(code string) error {
	return hookDecodeError{code: code}
}

// readOnlyStage recognizes one stage of a pipeline that is read-only by EFFECT: a
// reader (rg/grep/git-read/cat/…), a read-only Boatstack status helper, or a pure
// stdin→stdout inspection filter (wc/awk/sort/…). Effect-Typed Allowlist: a
// pipeline is admitted iff EVERY stage is effect-read-only, so ordinary inspection
// idioms — recovery-status | jq, git diff | wc -l, … | sort | uniq -c — compose
// freely. Effect-CHANGING syntax (redirection > <, command substitution $()) is
// still banned in isPureReadOnlyCommand, so no filter can be turned into a writer.
var readOnlyStage = regexp.MustCompile(`(?i)^\s*(?:env\s+[^ ]+\s+)*(?:rg|grep|git\s+(?:grep|diff|status|show|log)|cat|sed|head|tail|less|wc|awk|sort|uniq|cut|tr|jq|column|nl|comm|rev|fold|find\s+[^\n]*-(?:print|ls)|psql\s+[^\n]*\s-c\s+["']?\s*select\b|(?:[^\s]*/)?boatstack-helper(?:[_.-][a-z0-9._-]+)?\s+(?:recovery-status|mutation-status|operation-status|delivery-status|next-status|workspace-status|repair-status|check-plan|check-source-plan|check-safety|diagnose-hook|doctor|version)\b)`)

// Constitutional/Optimization split. These destruction rules are CONSTITUTIONAL:
// they define the real boundary (destroying a live resource) and are never traded
// for convenience — no project config knob disables them; config may only ADD
// scope (HighRiskPaths). The executor-gating around them (the scanSQL argument to
// classifySafetyText; the executed-vs-data file distinction; the artifact exemption
// on committed diffs) is the OPTIMIZATION surface: it narrows WHEN a rule is
// observed to cut false positives, but it may never disable the boundary — when the
// executor is live, a constitutional rule must still fire. That floor is enforced by
// TestExecutorGatingNeverDisablesTheBoundary.
//
// irreversiblePatterns classify destruction by text. Rules whose regex names its
// own EXECUTOR (rm, git, terraform, supabase db reset, …) are self-executing:
// matching the text is sound because the text IS the command. Rules marked
// sqlEffect match bare SQL grammar (DROP TABLE, TRUNCATE) that is inert as data
// and destructive only when a live database client runs it — so they are applied
// only in an executor context (see classifySafetyText's scanSQL argument). This
// is what stops `git add staging.sql` or a committed migration from being read as
// a live drop while `psql -c "DROP TABLE"` is still denied.
var irreversiblePatterns = []struct {
	category  string
	reason    string
	pattern   *regexp.Regexp
	sqlEffect bool
}{
	{"database-destruction", "database or schema destruction is operator-only", regexp.MustCompile(`(?is)\bdrop\s+(?:database|schema|table)\b|\balter\s+table\b[^;\n]*\bdrop\s+(?:column|constraint)\b|\btruncate(?:\s+table)?\b|\bdrop\s+schema\b[^;\n]*\bcascade\b`), true},
	{"database-reset", "database reset, flush, or destructive downgrade is operator-only", regexp.MustCompile(`(?i)(?:--reset-public\b|\b(?:supabase\s+db\s+reset|prisma\s+migrate\s+reset|rails\s+db:(?:drop|reset)|django-admin\s+flush|manage\.py\s+flush|alembic\s+downgrade\s+base|pg_restore\b[^\n]*\s--clean\b))`), false},
	{"filesystem-destruction", "recursive deletion of a broad or protected path is denied", regexp.MustCompile(`(?i)\b(?:rm\s+-[^\n;]*(?:r[^\n;]*f|f[^\n;]*r)|remove-item\s+[^\n;]*-recurse[^\n;]*-force)\s+(?:["']?(?:/|~|\$home|\$HOME|\.|\.\.)["']?\s*(?:;|&&|\|\||$)|[^\s;]*\*[^\s;]*)`), false},
	{"git-history-destruction", "destructive Git cleanup or history replacement is denied", regexp.MustCompile(`(?i)\bgit\s+(?:reset\s+--hard\b|clean\s+-[^\s]*(?:f[^\s]*d|d[^\s]*f|x)[^\s]*|push\b[^\n]*(?:--force(?:-with-lease)?|-f\b))`), false},
	{"infrastructure-destruction", "cloud or infrastructure destruction is operator-only", regexp.MustCompile(`(?i)\b(?:terraform|tofu|pulumi)\s+destroy\b|\bkubectl\s+delete\s+(?:namespace|cluster|persistentvolume|persistentvolumeclaim|pvc)\b|\bdocker\s+volume\s+(?:rm|prune)\b|\bgcloud\s+(?:projects|sql\s+instances|compute\s+(?:instances|disks))\s+delete\b|\baws\s+[^\n]*(?:delete-cluster|delete-db-instance|terminate-instances|delete-volume|delete-bucket)\b`), false},
	{"recovery-destruction", "backup deletion or recovery disablement is operator-only", regexp.MustCompile(`(?i)\b(?:delete|remove|disable)\b[^\n;]*(?:backup|snapshot|point-in-time|pitr|recovery)\b`), false},
}

// liveSQLClientPattern matches the executable of a command that runs SQL against a
// live database connection. Reading, committing, or diffing a file that CONTAINS
// SQL is not such a command; only these executors actually apply DDL/DML.
var liveSQLClientPattern = regexp.MustCompile(`(?i)^(?:psql|mysql|mariadb|mongo|mongosh|cockroach|sqlcmd|usql|clickhouse-client)$`)

// fileRunnerPattern matches an executable that EXECUTES a file argument, so that
// file's contents are a live capability — unlike git/cp/cat, which treat a named
// file as data. SQL clients are added at the use site (they run a file via -f/<).
var fileRunnerPattern = regexp.MustCompile(`(?i)^(?:python[0-9.]*|sh|bash|zsh|ksh|dash|ruby|node|deno|bun|perl|php|pwsh|powershell)$`)

var operationalPathPattern = regexp.MustCompile(`(?i)(?:^|/)(?:scripts?|migrations?|schema|database|db|deploy|infra|ops|terraform|k8s)(?:/|$)|\.(?:sql|ps1|sh|bash|py)$`)

// Match SQL mutation grammar rather than isolated English or command tokens.
// Requiring DELETE FROM or UPDATE <target> SET keeps executable SQL visible
// without treating names such as check-update or API method labels as queries.
var mutationStatementPattern = regexp.MustCompile(`(?is)\b(?:delete\s+from\s+(?:[a-z_][a-z0-9_$.-]*|"[^"]+")|update\s+(?:[a-z_][a-z0-9_$.-]*|"[^"]+")\s+set\b)[^;]*`)
var directPublicationPattern = regexp.MustCompile(`(?i)(?:\bgit\b[^\n;&|]*\bpush\b|\bgh\s+pr\s+(?:create|edit|ready|merge)\b|\bgh\s+api\b[^\n;&|]*(?:/pulls\b|/pull-requests\b)|\bhub\s+pull-request\b|\bcurl\b[^\n;&|]*(?:api\.github\.com|/pulls\b)[^\n;&|]*(?:\s-X\s*(?:POST|PATCH)|--request\s+(?:POST|PATCH)))`)
var approvedPublisherPattern = regexp.MustCompile(`(?i)^\s*(?:[^\s]*/)?boatstack-helper\s+publish-pr\b[^\n;&|]*$`)

// approvedUpdatePublisherPattern recognizes the sanctioned Boatstack version-update
// publisher. That command must be passed the update preview path, which lives under
// <git-common-dir>/boatstack/updates/<version>/pr-preview.json, so the command line
// always names a path inside the .git/boatstack/ subtree. Without this exemption the
// deliveryStatePathPattern check below denies the publish as workflow-state-tamper,
// even though the path is a read argument to the trusted helper, not a direct edit.
// Like approvedPublisherPattern it is anchored end to end so no second command can be
// chained after it, and it tolerates the platform-suffixed helper binary (for example
// boatstack-helper_darwin_arm64) that a running update may invoke after the installed
// helper is swapped or removed.
var approvedUpdatePublisherPattern = regexp.MustCompile(`(?i)^\s*(?:[^\s]*/)?boatstack-helper(?:[_.-][a-z0-9._-]+)?\s+publish-update-pr\b[^\n;&|]*$`)

// deliveryStatePathPattern matches Boatstack's managed runtime/control state so
// the guard denies direct model mutation of it. It covers the embedded homes
// (boatstack/deliveries and any .git/.../boatstack subtree) and the Detached
// Supervision external control root (boatstack/{repositories,registry.json} and
// the version-namespaced boatstack/runtimes). Only Boatstack transitions and the
// sanctioned publisher (approvedUpdatePublisherPattern) may name these paths.
var deliveryStatePathPattern = regexp.MustCompile(`(?i)(?:boatstack[/\\](?:deliveries|operations|flow|repositories|runtimes|registry\.json)|\.git[/\\](?:worktrees[/\\][^/\\]+[/\\])?boatstack(?:[/\\]|$))`)
var mutationToolPattern = regexp.MustCompile(`(?i)(?:write|edit|apply[_-]?patch|create|delete|remove|move|rename|update|insert|upload|install)`)
var planningMutationToolPattern = regexp.MustCompile(`(?i)(?:write|edit|apply[_-]?patch|create)`)
var externalReadOnlyToolPattern = regexp.MustCompile(`(?i)(?:^|[_-])(?:get|list|read|search|find|status|inspect|query|fetch|open)(?:[_-]|$)`)

// featuresCommandPathPattern extracts a .product-loop/features/… operand from a
// shell command so the first-write latch can see raw shell writes (cp, tee, >)
// the same way ClassifyTool sees a Write tool's file_path. Mirrors the
// deliveryStatePathPattern law for .git/boatstack: only owned channels may name
// managed planning paths in a mutating command.
// control-law: first-planning-write-uses-the-owned-channel
var featuresCommandPathPattern = regexp.MustCompile(`(?i)(?:^|[\s"'=(])((?:\./)?\.product-loop[/\\]features[/\\][^\s"';&|)]+)`)

func controlledPhaseTransition(command, stage string) bool {
	if strings.ContainsAny(command, "\n`><;&|") || strings.Contains(command, "$(") {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) < 2 {
		return false
	}
	executable := strings.TrimSuffix(strings.ToLower(filepath.Base(fields[0])), ".exe")
	if executable != "boatstack-helper" {
		return false
	}
	readOnlyHelpers := map[string]bool{
		"check-plan": true, "check-source-plan": true, "next-status": true, "delivery-status": true,
		"recovery-status": true, "repair-status": true, "operation-status": true, "check-safety": true, "workspace-status": true, "diagnose-hook": true,
		"doctor": true, "version": true, "mutation-status": true,
	}
	if readOnlyHelpers[fields[1]] {
		return true
	}
	// repair-state is the guard-prescribed recovery for a workflow stuck at
	// INVALID_STATE because of an unregistered malformed draft. Those findings
	// carry an empty stage, so allow it independent of stage. It mutates (it
	// quarantines the draft), so it is not a read-only helper; RepairState
	// self-guards, refusing any registered, published, or tracked directory.
	if fields[1] == "repair-state" {
		return true
	}
	// undo is the bounded actuator that reverses a Boatstack-generated managed
	// artifact by re-applying its receipt's inverse through the same transactional
	// mutation boundary. Like repair-state it is a stage-independent recovery verb;
	// it mutates but self-guards (UndoManagedMutation refuses to strand delivery
	// state and the boundary's stale-base precondition refuses to clobber later
	// work), so it is not a read-only helper.
	if fields[1] == "undo" {
		return true
	}
	// workspace-reap and workspace-cleanup are the sanctioned actuators that
	// reclaim finished managed worktrees and branches. They mutate only
	// Boatstack-owned workspace bookkeeping — never product source or delivery
	// state — and self-guard (refusing the base branch, the current worktree, and
	// unmerged or dirty work without an explicit force). They are stage-independent
	// like undo: without this the pre-activation interlock would deny post-merge
	// cleanup and force the operator to reclaim worktrees with raw, denied Git.
	if fields[1] == "workspace-reap" || fields[1] == "workspace-cleanup" {
		return true
	}
	// discard-delivery is the bounded recovery that clears stuck or unverifiable
	// managed delivery state (and orphaned feature artifacts). It is the verb the
	// resolver prescribes for those causes, so it must be admitted wherever it is
	// prescribed — the Coreachability invariant: the states that prescribe a
	// recovery verb must be a subset of the states that verb accepts, and the verb
	// must be reachable in-tool. It mutates but self-guards (DiscardDelivery archives
	// rather than deletes and refuses published state without --force). Without this
	// admission the resolver could name discard-delivery while the guard denied it —
	// a fail-closed state with no reachable exit.
	if fields[1] == "discard-delivery" {
		return true
	}
	switch stage {
	case "DRAFT_PLAN":
		return fields[1] == "planning-write" || fields[1] == "record-approval"
	case "INVALID_STATE":
		return fields[1] == "planning-write" || fields[1] == "record-approval"
	case "APPROVED", "POLICY_READY":
		return fields[1] == "activate-plan" || fields[1] == "workspace-cut"
	case "NOT_STARTED":
		// The first-write latch denies raw writes into .product-loop/features/
		// before any candidate exists and prescribes planning-write; Coreachability
		// requires the guard to admit that verb at the very stage that names it.
		// record-approval is NOT admitted here — there is no plan to approve yet.
		return fields[1] == "planning-write"
	default:
		return false
	}
}

func controlledWorkspaceSync(repo, command string) bool {
	if strings.ContainsAny(command, "\n`><;&|") || strings.Contains(command, "$(") {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) < 4 || fields[1] != "workspace-sync" {
		return false
	}
	executable := fields[0]
	if !filepath.IsAbs(executable) {
		executable = filepath.Join(repo, filepath.FromSlash(executable))
	}
	executable, err := filepath.Abs(executable)
	if err != nil {
		return false
	}
	expected := filepath.Join(repo, ".product-loop", "bin", helperName())
	expected, err = filepath.Abs(expected)
	if err != nil || filepath.Clean(executable) != filepath.Clean(expected) {
		return false
	}
	info, err := os.Lstat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	seenSource := false
	for index := 2; index < len(fields); index += 2 {
		if index+1 >= len(fields) {
			return false
		}
		switch fields[index] {
		case "--source":
			seenSource = strings.TrimSpace(fields[index+1]) != ""
		case "--branch":
			if strings.TrimSpace(fields[index+1]) == "" {
				return false
			}
		case "--repo":
			candidate := fields[index+1]
			if !filepath.IsAbs(candidate) {
				candidate = filepath.Join(repo, filepath.FromSlash(candidate))
			}
			absolute, absErr := filepath.Abs(candidate)
			if absErr != nil || filepath.Clean(absolute) != filepath.Clean(repo) {
				return false
			}
		default:
			return false
		}
	}
	return seenSource
}

func attemptedRepositoryPath(repo string, input any) string {
	keys := map[string]bool{"path": true, "file_path": true, "filepath": true, "target_path": true, "destination": true}
	var visit func(any) string
	visit = func(value any) string {
		switch typed := value.(type) {
		case map[string]any:
			for key, child := range typed {
				if keys[strings.ToLower(key)] {
					if candidate, ok := child.(string); ok && strings.TrimSpace(candidate) != "" {
						path := candidate
						if !filepath.IsAbs(path) {
							path = filepath.Join(repo, filepath.FromSlash(path))
						}
						absolute, err := filepath.Abs(path)
						if err != nil {
							return "<invalid-path>"
						}
						relative, err := filepath.Rel(repo, absolute)
						if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
							return "<outside-repository>"
						}
						if err := rejectSymlinkComponents(repo, absolute); err != nil {
							return "<invalid-path>"
						}
						return filepath.ToSlash(relative)
					}
				}
			}
			for _, child := range typed {
				if found := visit(child); found != "" {
					return found
				}
			}
		case []any:
			for _, child := range typed {
				if found := visit(child); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return visit(input)
}

// featureScopedPath reports whether a repo-relative path lands anywhere under
// the managed planning tree. Broader than planningMarkdownPath on purpose: the
// first-write latch covers every depth and name, while planningMarkdownPath
// stays the exact allowlist for the bounded DRAFT_PLAN carve-out.
func featureScopedPath(path string) bool {
	return strings.HasPrefix(filepath.ToSlash(path), ".product-loop/features/")
}

// featuresPathInCommand extracts the first .product-loop/features/… operand a
// shell command names, normalized to a slash-form repo-relative path, or ""
// when none is named. It is the ClassifyCommand analogue of a Write tool's
// extracted file_path, feeding the same first-write latch.
func featuresPathInCommand(command string) string {
	match := featuresCommandPathPattern.FindStringSubmatch(command)
	if match == nil {
		return ""
	}
	path := filepath.ToSlash(match[1])
	return strings.TrimPrefix(path, "./")
}

func planningMarkdownPath(path string) bool {
	if !featureScopedPath(path) {
		return false
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	return len(parts) == 4 && featureSlugPattern.MatchString(parts[2]) && planningArtifacts[parts[3]]
}

// preActivationFinding decides whether a product mutation is denied before a plan
// reaches its activation boundary, and — per the Coreachability invariant — names
// a recovery verb that actually CLEARS the cause it reports. A "cannot verify /
// cannot resolve" error is observation loss (a channel fault), not a plant defect:
// no mutation verb repairs it, so it is classified distinctly and routed to the
// read-only doctor to diagnose the channel — never to repair-state, which acts on a
// malformed draft and would refuse. A genuinely malformed draft routes to
// repair-state; every other stage carries ResolveNext's own (already Coreachable)
// next operation.
func preActivationFinding(repo, attemptedPath string) (SafetyFinding, bool) {
	active, err := ActiveManagedDeliveries(repo)
	if err != nil {
		// Same-Relation-Same-Law: the mutation boundary FAILS CLOSED on invalid
		// delivery state — ignoring a delivery quiets status but never launders corrupt
		// state into a mutation (TestActiveManagedDeliveriesStaysFailClosedOnInvalid).
		// But the block must be Coreachable: distinguish a corrupt-delivery *plant*
		// fault (a verb clears it — discard-delivery, named) from an *observation*
		// (channel) fault (doctor to diagnose). Naming discard-delivery is what tells
		// the operator how to actually unblock mutation, since ignoring will not.
		if _, invalid, scanErr := scanManagedDeliveries(repo); scanErr == nil && len(invalid) > 0 {
			return SafetyFinding{
				Category: "workflow-state-invalid", Source: "delivery-state",
				Reason:        "invalid managed delivery state blocks all mutation; ignoring it only quiets status — clear it with discard-delivery to continue",
				NextOperation: "discard-delivery", BlockingFeature: invalid[0], AttemptedPath: attemptedPath,
			}, true
		}
		return SafetyFinding{Category: "workflow-observation-fault", Reason: "managed delivery state cannot be verified; diagnose the channel with doctor", Source: "delivery-state", NextOperation: "doctor"}, true
	}
	if len(active) > 0 {
		return SafetyFinding{}, false
	}
	candidates, err := featurePlanCandidates(repo)
	if err != nil {
		return SafetyFinding{Category: "workflow-observation-fault", Reason: "saved feature plans cannot be verified; diagnose the channel with doctor", Source: "planning-state", NextOperation: "doctor"}, true
	}
	if len(candidates) == 0 {
		// First-write latch: even before any plan candidate exists, the managed
		// planning tree is authored only through the owned channel. Without this,
		// the very first raw host write of plan.md registers a malformed draft and
		// the agent discovers planning-write only by failing into INVALID_STATE.
		// The deny is path-scoped — ordinary product writes stay unlatched at zero
		// candidates. Stage NOT_STARTED is what ResolveNext reports here, and
		// controlledPhaseTransition admits planning-write at that stage, so the
		// denial names a verb the guard accepts (Coreachability).
		// control-law: first-planning-write-uses-the-owned-channel
		if featureScopedPath(attemptedPath) {
			finding := SafetyFinding{
				Category: "workflow-phase-bypass", Reason: "planning Markdown is created through the owned channel; a raw first write into .product-loop/features/ is denied", Source: "planning-state",
				WorkflowStage: "NOT_STARTED", AttemptedPath: attemptedPath, NextOperation: "planning-write",
			}
			if parts := strings.Split(filepath.ToSlash(attemptedPath), "/"); len(parts) > 2 && featureSlugPattern.MatchString(parts[2]) {
				finding.BlockingFeature = parts[2]
			}
			return finding, true
		}
		return SafetyFinding{}, false
	}
	status, err := ResolveNext(repo, "")
	if err != nil {
		return SafetyFinding{Category: "workflow-observation-fault", Reason: "workflow state cannot be resolved; diagnose the channel with doctor", Source: "planning-state", NextOperation: "doctor"}, true
	}
	if status.ObservedStage != "DRAFT_PLAN" && status.ObservedStage != "APPROVED" && status.ObservedStage != "POLICY_READY" && status.ObservedStage != "AMBIGUOUS" && status.ObservedStage != "INVALID_STATE" {
		return SafetyFinding{}, false
	}
	if len(candidates) == 1 && status.ObservedStage != "AMBIGUOUS" {
		planPath := filepath.Join(repo, ".product-loop", "features", candidates[0], "plan.md")
		check, checkErr := CheckPlan(planPath)
		if checkErr != nil {
			return SafetyFinding{
				Category: "workflow-phase-bypass", Reason: "saved plan state is invalid", Source: "planning-state",
				BlockingFeature: candidates[0], WorkflowStage: "INVALID_STATE", AttemptedPath: attemptedPath, NextOperation: "repair-state",
			}, true
		}
		if status.ObservedStage == "APPROVED" {
			approvalPath := filepath.Join(filepath.Dir(planPath), "approval.md")
			if _, approvalErr := CheckApprovalReceipt(approvalPath, check); approvalErr != nil {
				return SafetyFinding{
					Category: "workflow-phase-bypass", Reason: "approval or product baseline is stale", Source: "planning-state",
					BlockingFeature: candidates[0], WorkflowStage: "INVALID_STATE", AttemptedPath: attemptedPath, NextOperation: "plan-gate",
				}, true
			}
		}
	}
	return SafetyFinding{
		Category: "workflow-phase-bypass", Reason: "product mutation is denied until the saved plan reaches its controlled activation boundary", Source: "planning-state",
		BlockingFeature: status.Feature, WorkflowStage: status.ObservedStage, AttemptedPath: attemptedPath, NextOperation: status.NextOperation,
	}, true
}

func publicationBypassFinding(repo, reason, source string) (SafetyFinding, bool) {
	active, err := ActiveManagedDeliveries(repo)
	if err != nil {
		return SafetyFinding{Category: "workflow-state-invalid", Reason: "publication is denied because managed delivery state cannot be verified", Source: "delivery-state"}, true
	}
	// Scope publication-authority resolution to un-ignored deliveries so it
	// matches ResolveNext and the run coordinator. Without this, a stale-but-
	// active ignored delivery (e.g. an APPROVED lock whose code shipped out of
	// band) poisons authority for every other delivery with relation=ambiguous.
	// A config that fails to load leaves active unfiltered, preserving the prior
	// behavior.
	if config, _, configErr := LoadConfig(WorkspaceFor(repo).ProjectConfigPath()); configErr == nil {
		active = withoutIgnoredDeliveries(active, config.Workflow.IgnoredDeliveries)
	}
	if len(active) == 0 {
		return SafetyFinding{}, false
	}
	branch, _ := gitCommand(repo, "branch", "--show-current")
	selected := ""
	relation := "unrelated"
	for _, feature := range active {
		state, loadErr := LoadDeliveryState(repo, feature)
		if loadErr == nil && stateMatchesBranch(state, strings.TrimSpace(branch)) {
			if selected != "" {
				selected = strings.Join(active, ",")
				relation = "ambiguous"
				break
			}
			selected = feature
			relation = "current_branch"
		}
	}
	if selected == "" {
		if len(active) == 1 {
			selected = active[0]
		} else {
			selected = strings.Join(active, ",")
			relation = "ambiguous"
		}
	}
	finding := SafetyFinding{
		Category: "workflow-publication-bypass", Reason: reason, Source: source,
		BlockingFeature: selected, BranchRelation: relation, NextOperation: "recovery-status",
	}
	if !strings.Contains(selected, ",") {
		if state, loadErr := LoadDeliveryState(repo, selected); loadErr == nil {
			// Report the addressable slice the current branch actually owns — the
			// active slice or a published-but-open earlier slice — rather than always
			// the active slice, which named the wrong slice for a published-slice fix.
			if _, addressable, ok := resolveAddressableSliceByBranch(state, strings.TrimSpace(branch)); ok {
				finding.BlockingSlice = addressable.ID
			} else {
				_, finding.BlockingSlice, _ = deliveryBranchAndSlice(state)
			}
			finding.ParentDelivery = state.ParentDelivery
		}
	}
	return finding, true
}

// classifySafetyText matches destructive-operation text. scanSQL gates the rules
// whose grammar is inert as data and destructive only when a live database client
// runs it — bare DDL (DROP TABLE) and unbounded DML (DELETE FROM … with no WHERE).
// Callers pass scanSQL=true only in an executor context (a command that invokes a
// SQL client, a file the command executes, or a live SQL tool); they pass false
// for a committed artifact, a git operand, or a document edit, so declarative SQL
// is treated as data. Self-executing rules (rm, git, terraform, supabase db reset)
// name their own executor and always apply.
func classifySafetyText(value, source string, scanSQL bool) []SafetyFinding {
	if isPureReadOnlyCommand(value) {
		return nil
	}
	findings := []SafetyFinding{}
	seen := map[string]bool{}
	for _, rule := range irreversiblePatterns {
		if rule.sqlEffect && !scanSQL {
			continue
		}
		if rule.pattern.MatchString(value) && !seen[rule.category] {
			seen[rule.category] = true
			findings = append(findings, SafetyFinding{Category: rule.category, Reason: rule.reason, Source: source})
		}
	}
	if scanSQL {
		for _, statement := range mutationStatementPattern.FindAllString(strings.ToLower(value), -1) {
			normalized := " " + strings.Join(strings.Fields(statement), " ") + " "
			if !strings.Contains(normalized, " where ") {
				findings = append(findings, SafetyFinding{Category: "unbounded-data-mutation", Reason: "unbounded data deletion or update is denied", Source: source})
				break
			}
		}
	}
	return findings
}

// isPureReadOnlyCommand recognizes a deliberately narrow diagnostic surface.
// Every pipeline stage must itself be read-only, and compound shell syntax is
// rejected. Quoted search patterns may name dangerous operations without
// turning the diagnostic search into an executable capability.
func isPureReadOnlyCommand(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.ContainsAny(trimmed, "\n`><") || strings.Contains(trimmed, "$(") ||
		strings.Contains(trimmed, ";") || strings.Contains(trimmed, "&&") || strings.Contains(trimmed, "||") || strings.Contains(trimmed, "<<") {
		return false
	}
	stages, ok := shellPipelineStages(trimmed)
	if !ok {
		return false
	}
	for _, stage := range stages {
		if !readOnlyStage.MatchString(strings.TrimSpace(stage)) {
			return false
		}
	}
	return true
}

func shellPipelineStages(value string) ([]string, bool) {
	stages := []string{}
	start := 0
	var quote rune
	escaped := false
	for index, char := range value {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char == '|' {
			stages = append(stages, value[start:index])
			start = index + 1
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	stages = append(stages, value[start:])
	return stages, true
}

// shellSegments splits a command into simple-command segments on unquoted shell
// operators (; & | and newline), so each segment's first word is its executor.
// && and || reduce to their operator characters, which still segments correctly.
func shellSegments(value string) []string {
	segments := []string{}
	start := 0
	var quote rune
	escaped := false
	for index, char := range value {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char == ';' || char == '&' || char == '|' || char == '\n' {
			segments = append(segments, value[start:index])
			start = index + 1
		}
	}
	return append(segments, value[start:])
}

// segmentExecutor returns the executable basename of a simple command, skipping
// leading VAR=value assignments and benign wrappers (env, sudo, time, …). It
// returns "" when the segment has no command word.
func segmentExecutor(segment string) string {
	fields := strings.Fields(segment)
	for len(fields) > 0 {
		field := fields[0]
		if strings.HasPrefix(field, "-") {
			return ""
		}
		if eq := strings.IndexByte(field, '='); eq > 0 && !strings.ContainsAny(field[:eq], "/\\") {
			fields = fields[1:]
			continue
		}
		base := strings.TrimSuffix(strings.ToLower(filepath.Base(field)), ".exe")
		switch base {
		case "env", "sudo", "time", "nohup", "xargs", "command", "doas", "stdbuf":
			fields = fields[1:]
			continue
		}
		return base
	}
	return ""
}

// shellDashCScript returns the script passed to a shell's -c flag, so an executor
// hidden inside `bash -c "…"` is analyzed at the same fidelity as a top-level one.
func shellDashCScript(executor, segment string) (string, bool) {
	switch executor {
	case "sh", "bash", "zsh", "ksh", "dash":
	default:
		return "", false
	}
	fields := strings.Fields(segment)
	for index, field := range fields {
		if field == "-c" && index+1 < len(fields) {
			return strings.Trim(strings.Join(fields[index+1:], " "), "\"'"), true
		}
	}
	return "", false
}

// commandExecutesLiveSQL reports whether any segment of the command invokes a live
// database client. Quoted prose in an unrelated command (git commit -m "DROP
// TABLE …") is not an executor and returns false; a nested shell -c script is
// unwrapped so `bash -c "psql … DROP …"` returns true.
func commandExecutesLiveSQL(command string) bool {
	for _, segment := range shellSegments(command) {
		executor := segmentExecutor(segment)
		if liveSQLClientPattern.MatchString(executor) {
			return true
		}
		if inline, ok := shellDashCScript(executor, segment); ok && commandExecutesLiveSQL(inline) {
			return true
		}
	}
	return false
}

// executedRepositoryFiles returns repository files the command actually EXECUTES,
// split into regular files (whose contents are inspected) and symlinked
// entrypoints (reported, never followed). A file merely named as data — git add
// x.sql, cp, cat — is returned by neither, because only runner segments (an
// interpreter or a SQL client that runs a file) are considered.
func executedRepositoryFiles(repo, command string) (content []string, symlinks []string) {
	seen := map[string]bool{}
	for _, segment := range shellSegments(command) {
		executor := segmentExecutor(segment)
		if !fileRunnerPattern.MatchString(executor) && !liveSQLClientPattern.MatchString(executor) {
			continue
		}
		for _, token := range strings.Fields(segment) {
			candidate := strings.Trim(token, "\"'`;,()[]{}")
			if candidate == "" || strings.HasPrefix(candidate, "-") {
				continue
			}
			ext := strings.ToLower(filepath.Ext(candidate))
			if ext != ".py" && ext != ".sh" && ext != ".bash" && ext != ".ps1" && ext != ".sql" {
				continue
			}
			path := candidate
			if !filepath.IsAbs(path) {
				path = filepath.Join(repo, filepath.FromSlash(candidate))
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(repo, abs)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if seen[abs] {
				continue
			}
			info, err := os.Lstat(abs)
			if err != nil {
				continue
			}
			seen[abs] = true
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				symlinks = append(symlinks, abs)
			case info.Mode().IsRegular():
				content = append(content, abs)
			}
		}
	}
	return content, symlinks
}

// sqlExecutorToolPattern matches a tool name that denotes a database client
// executing SQL against a live connection (an MCP execute_sql / db query tool),
// so its arguments are a live capability rather than inert text.
var sqlExecutorToolPattern = regexp.MustCompile(`(?i)(?:execute|run|exec)[_-]?sql|sql[_-]?(?:exec|execute|query|statement)|db[_-]?(?:execute|exec|query)`)

// toolExecutesLiveSQL reports whether the tool runs SQL against a live database.
func toolExecutesLiveSQL(name string) bool {
	return sqlExecutorToolPattern.MatchString(strings.ToLower(name))
}

func ClassifyCommand(repo, command string) []SafetyFinding {
	if strings.TrimSpace(command) == "" {
		return []SafetyFinding{{Category: "malformed-tool-input", Reason: "empty-command", Source: "tool-input"}}
	}
	if deliveryStatePathPattern.MatchString(command) && !isPureReadOnlyCommand(command) && !approvedUpdatePublisherPattern.MatchString(command) {
		return []SafetyFinding{{Category: "workflow-state-tamper", Reason: "managed delivery state may be changed only by Boatstack transitions", Source: "delivery-state"}}
	}
	if directPublicationPattern.MatchString(command) && !approvedPublisherPattern.MatchString(command) {
		if finding, blocked := publicationBypassFinding(repo, "direct push or PR mutation is denied while a managed delivery slice is active", "tool-input"); blocked {
			return []SafetyFinding{finding}
		}
	}
	if strings.Contains(command, "workspace-sync") && !isPureReadOnlyCommand(command) && !controlledWorkspaceSync(repo, command) {
		return []SafetyFinding{{Category: "workspace-sync-bypass", Reason: "recoverable branch alignment must use the exact project-local Boatstack helper", Source: "command"}}
	}
	findings := classifySafetyText(command, "command", commandExecutesLiveSQL(command))
	if len(findings) > 0 {
		return dedupeFindings(findings)
	}
	if !isPureReadOnlyCommand(command) {
		// Feed any named .product-loop/features/ operand so the first-write latch
		// sees raw shell writes (cp/tee/redirect) the same way it sees a Write tool.
		if finding, blocked := preActivationFinding(repo, featuresPathInCommand(command)); blocked && !controlledPhaseTransition(command, finding.WorkflowStage) && !controlledWorkspaceSync(repo, command) {
			return []SafetyFinding{finding}
		}
	}
	if regexp.MustCompile(`(?i)\b(?:rm\s+-[^\n;]*(?:r[^\n;]*f|f[^\n;]*r)|remove-item\s+[^\n;]*-recurse[^\n;]*-force)\b`).MatchString(command) && strings.Contains(command, repo) {
		findings = append(findings, SafetyFinding{Category: "filesystem-destruction", Reason: "recursive deletion of the repository is denied", Source: "command"})
	}
	if len(findings) > 0 || isPureReadOnlyCommand(command) {
		return dedupeFindings(findings)
	}
	// Only inspect files the command actually EXECUTES (interpreter / SQL-client
	// segments). A file merely named as data (git add x.sql, cp, cat) is not an
	// executed capability, so its SQL content is never classified. An executed file
	// IS a live capability, so its contents are scanned with scanSQL=true.
	contentFiles, symlinkFiles := executedRepositoryFiles(repo, command)
	if len(symlinkFiles) > 0 {
		return []SafetyFinding{{Category: "symlink-entrypoint", Reason: "an invoked repository entrypoint is a symlink and cannot be inspected safely", Source: filepath.Base(symlinkFiles[0])}}
	}
	for _, path := range contentFiles {
		value, err := os.ReadFile(path)
		if err != nil {
			return []SafetyFinding{{Category: "unreadable-entrypoint", Reason: "an invoked repository entrypoint could not be inspected", Source: filepath.Base(path)}}
		}
		relative, relErr := filepath.Rel(repo, path)
		if relErr != nil {
			relative = filepath.Base(path)
		}
		findings = append(findings, classifySafetyText(string(value), filepath.ToSlash(relative), true)...)
	}
	return dedupeFindings(findings)
}

func ClassifyTool(repo, name string, input any) []SafetyFinding {
	if strings.EqualFold(name, "Bash") || strings.EqualFold(name, "Shell") || strings.EqualFold(name, "beforeShellExecution") || strings.EqualFold(name, "run_shell_command") {
		if object, ok := input.(map[string]any); ok {
			return ClassifyCommand(repo, stringValue(object["command"]))
		}
	}
	value, err := json.Marshal(input)
	if err != nil {
		return []SafetyFinding{{Category: "malformed-tool-input", Reason: "invalid-tool-input", Source: "tool-input"}}
	}
	combined := name + " " + string(value)
	// Bare SQL grammar in a tool's arguments is a live capability only when the tool
	// itself executes SQL (an MCP execute_sql / db query tool). A Write/Edit/Read
	// whose content merely contains DDL is a document, not an execution.
	findings := classifySafetyText(combined, "tool-input", toolExecutesLiveSQL(name))
	nameLower := strings.ToLower(name)
	attemptedPath := attemptedRepositoryPath(repo, input)
	mutationCapable := mutationToolPattern.MatchString(nameLower) || (strings.HasPrefix(nameLower, "mcp__") && !externalReadOnlyToolPattern.MatchString(nameLower))
	if mutationCapable {
		if finding, blocked := preActivationFinding(repo, attemptedPath); blocked {
			if finding.WorkflowStage != "DRAFT_PLAN" || attemptedPath == "" || !planningMarkdownPath(attemptedPath) || !planningMutationToolPattern.MatchString(nameLower) {
				findings = append(findings, finding)
			}
		}
	}
	publicationText := strings.ToLower(combined)
	if deliveryStatePathPattern.MatchString(combined) && regexp.MustCompile(`(?:write|edit|delete|remove|move|rename|create|update)`).MatchString(nameLower) {
		findings = append(findings, SafetyFinding{Category: "workflow-state-tamper", Reason: "managed delivery state may be changed only by Boatstack transitions", Source: "delivery-state"})
	}
	if (strings.Contains(publicationText, "pull_request") || strings.Contains(publicationText, "pull request")) &&
		regexp.MustCompile(`(?:create|update|edit|merge|publish)`).MatchString(publicationText) {
		if finding, blocked := publicationBypassFinding(repo, "direct PR mutation is denied while a managed delivery slice is active", "tool-input"); blocked {
			findings = append(findings, finding)
		}
	}
	if regexp.MustCompile(`(?:delete|destroy|reset|drop|truncate|terminate)`).MatchString(nameLower) && regexp.MustCompile(`(?:database|schema|project|cluster|namespace|volume|bucket|backup|snapshot|instance)`).MatchString(strings.ToLower(combined)) {
		findings = append(findings, SafetyFinding{Category: "external-resource-destruction", Reason: "destructive external-resource tools are operator-only", Source: "tool-input"})
	}
	return dedupeFindings(findings)
}

func mutationCapableTool(name string, input any) bool {
	if strings.EqualFold(name, "Bash") || strings.EqualFold(name, "Shell") || strings.EqualFold(name, "beforeShellExecution") || strings.EqualFold(name, "run_shell_command") {
		object, ok := input.(map[string]any)
		return !ok || !isPureReadOnlyCommand(stringValue(object["command"]))
	}
	lower := strings.ToLower(name)
	return mutationToolPattern.MatchString(lower) || (strings.HasPrefix(lower, "mcp__") && !externalReadOnlyToolPattern.MatchString(lower))
}

func supervisedToolIdentity(name string, input any) (string, string) {
	value, _ := json.Marshal(input)
	fingerprint := SHA256Bytes(append([]byte(strings.ToLower(strings.TrimSpace(name))+"\x00"), value...))
	return "tool:" + strings.ToLower(strings.TrimSpace(name)), fingerprint
}

func activeManagedOperationScope(repo string) (OperationScope, string, bool) {
	active, err := ActiveManagedDeliveries(repo)
	if err != nil || len(active) == 0 {
		return OperationScope{}, "", false
	}
	branch := strings.TrimSpace(gitOutput(repo, "branch", "--show-current"))
	for _, feature := range active {
		state, loadErr := LoadDeliveryState(repo, feature)
		if loadErr != nil || !stateMatchesBranch(state, branch) || state.ActiveIndex >= len(state.Slices) {
			continue
		}
		slice := state.Slices[state.ActiveIndex]
		return OperationScope{Feature: feature, Slice: slice.ID, Worktree: filepath.Base(repo), HeadBranch: branch}, state.PlanLockHash, true
	}
	return OperationScope{}, "", false
}

func operationRetryClassForTool(name string) string {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "mcp__") || strings.Contains(lower, "upload") || strings.Contains(lower, "browser") {
		return "RECONCILE_FIRST"
	}
	if strings.Contains(lower, "write") || strings.Contains(lower, "edit") || strings.Contains(lower, "patch") || strings.Contains(lower, "create") {
		return "ATOMIC_LOCAL"
	}
	return "IDEMPOTENT_EXTERNAL"
}

func hookAttemptKey(host, fingerprint string, eventValue []byte) string {
	var event map[string]any
	if json.Unmarshal(eventValue, &event) == nil {
		for _, key := range []string{"tool_call_id", "tool_use_id", "call_id"} {
			if identity := strings.TrimSpace(stringValue(event[key])); identity != "" {
				return SHA256Bytes([]byte(strings.ToLower(strings.TrimSpace(host)) + "\x00" + identity + "\x00" + fingerprint))
			}
		}
	}
	return SHA256Bytes([]byte(strings.ToLower(strings.TrimSpace(host)) + "\x00" + fingerprint))
}

func superviseToolAttempt(repo, host, name string, input any, eventValue []byte) *SafetyFinding {
	if !mutationCapableTool(name, input) {
		return nil
	}
	scope, authority, managed := activeManagedOperationScope(repo)
	if !managed {
		return nil
	}
	kind, fingerprint := supervisedToolIdentity(name, input)
	target := attemptedRepositoryPath(repo, input)
	if target == "" {
		target = kind
	}
	receipt, err := PrepareOperation(OperationPrepareOptions{
		Repo: repo, Kind: kind, Scope: scope, Target: target, PackageFingerprint: fingerprint,
		AuthorizationFingerprint: authority, RetryClass: operationRetryClassForTool(name), MaxAttempts: 3,
		ExpectedPostcondition: "the supervised tool reports completion and its target can be reconciled",
	})
	if err != nil {
		return &SafetyFinding{Category: "operation-state-invalid", Reason: "the managed operation receipt could not be prepared", Source: "operation-controller", NextOperation: "operation-status"}
	}
	if receipt.State == OperationSucceeded {
		return &SafetyFinding{Category: "operation-already-succeeded", Reason: "the identical fingerprinted operation already succeeded", Source: "operation-controller", OperationID: receipt.OperationID, OperationState: string(receipt.State), AttemptNumber: receipt.Attempt, NextOperation: "none"}
	}
	attemptKey := hookAttemptKey(host, fingerprint, eventValue)
	begin, beginErr := BeginOperation(repo, receipt.OperationID, attemptKey, name)
	if beginErr == nil {
		return nil
	}
	finding := &SafetyFinding{
		Category: "operation-state-invalid", Reason: beginErr.Error(), Source: "operation-controller",
		OperationID: receipt.OperationID, OperationState: string(begin.Receipt.State), AttemptNumber: begin.Receipt.Attempt, NextOperation: "operation-status",
	}
	switch {
	case errors.Is(beginErr, ErrOperationInFlight):
		finding.Category = "operation-in-flight"
		finding.Reason = "the identical authorized operation is already executing"
		finding.NextOperation = "wait"
	case begin.Receipt.State == OperationReconcileRequired:
		finding.Category = "operation-reconciliation-required"
		finding.Reason = "the previous attempt ended without an observable completion"
		finding.ReconciliationRequired = true
		finding.NextOperation = "reconcile"
	case begin.Receipt.State == OperationFailedFinal:
		finding.Category = "operation-retry-exhausted"
		finding.Reason = "the persistent operation retry budget is exhausted"
		finding.NextOperation = "manual_recovery"
	}
	return finding
}

func postToolEvent(host string, value []byte) (string, any, string, bool, bool) {
	var event map[string]any
	if json.Unmarshal(value, &event) != nil {
		return "", nil, "", false, false
	}
	eventName := stringValue(event["hook_event_name"])
	postNames := map[string]bool{"postToolUse": true, "postToolUseFailure": true, "afterShellExecution": true, "afterMCPExecution": true, "PostToolUse": true, "PostToolUseFailure": true, "AfterTool": true}
	if !postNames[eventName] {
		return "", nil, "", false, false
	}
	name := stringValue(event["tool_name"])
	input := event["tool_input"]
	if eventName == "afterShellExecution" {
		name = "Bash"
		input = map[string]any{"command": stringValue(event["command"])}
	}
	if eventName == "afterMCPExecution" {
		var err error
		input, err = cursorMCPInput(input)
		if err != nil {
			return "", nil, "UNKNOWN", true, true
		}
	}
	hasResult := event["tool_response"] != nil || event["tool_result"] != nil || event["tool_output"] != nil || event["result"] != nil || event["output"] != nil || event["error"] != nil || event["tool_error"] != nil || event["exit_code"] != nil || event["exitCode"] != nil
	if strings.Contains(strings.ToLower(eventName), "failure") {
		hasResult = event["error"] != nil || event["tool_error"] != nil
	}
	if !hasResult {
		return "", nil, "UNKNOWN", true, true
	}
	outcome := "SUCCEEDED"
	failed := event["error"] != nil || event["tool_error"] != nil || event["is_error"] == true
	for _, key := range []string{"exit_code", "exitCode"} {
		if code, ok := event[key].(float64); ok && code != 0 {
			failed = true
		}
	}
	if response, ok := event["tool_response"].(map[string]any); ok {
		if response["error"] != nil || response["is_error"] == true || response["success"] == false {
			failed = true
		}
		for _, key := range []string{"exit_code", "exitCode"} {
			if code, ok := response[key].(float64); ok && code != 0 {
				failed = true
			}
		}
	}
	if failed {
		outcome = "UNKNOWN"
	}
	if strings.TrimSpace(name) == "" || input == nil {
		return "", nil, "UNKNOWN", true, true
	}
	return name, input, outcome, true, false
}

func completeSupervisedToolEvent(repo, host string, value []byte) (bool, bool) {
	name, input, outcome, handled, malformed := postToolEvent(host, value)
	if !handled {
		return false, false
	}
	if malformed {
		return true, true
	}
	if name == "" || input == nil || !mutationCapableTool(name, input) {
		return true, false
	}
	kind, fingerprint := supervisedToolIdentity(name, input)
	target := attemptedRepositoryPath(repo, input)
	if target == "" {
		target = kind
	}
	id := operationID(kind, target, fingerprint)
	receipt, err := loadOperation(repo, id)
	if err != nil || receipt.State != OperationExecuting || receipt.Lease == nil {
		return true, false
	}
	attemptKey := hookAttemptKey(host, fingerprint, value)
	if _, err := CompleteOperationAttempt(repo, id, attemptKey, outcome, "host completion event observed", ""); err != nil && outcome != "UNKNOWN" {
		_, _ = CompleteOperationAttempt(repo, id, attemptKey, "UNKNOWN", "completion event could not be correlated", "")
	}
	return true, false
}

func dedupeFindings(values []SafetyFinding) []SafetyFinding {
	seen := map[string]bool{}
	result := []SafetyFinding{}
	for _, value := range values {
		key := value.Category + "\x00" + value.Source
		if !seen[key] {
			seen[key] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Category == result[j].Category {
			return result[i].Source < result[j].Source
		}
		return result[i].Category < result[j].Category
	})
	return result
}

type hookHostContract struct {
	decode func([]byte) (string, any, error)
	allow  func() ([]byte, error)
	deny   func(SafetyFinding) ([]byte, error)
}

func decodeJSONObject(host string, value []byte) (map[string]any, error) {
	if len(strings.TrimSpace(string(value))) == 0 {
		return nil, malformedHookInput("empty-input")
	}
	var event map[string]any
	if err := DecodeJSON("parse "+host+" hook event", "stdin", value, &event); err != nil {
		return nil, malformedHookInput("invalid-json")
	}
	return event, nil
}

func cursorMCPInput(value any) (any, error) {
	if text, ok := value.(string); ok {
		if strings.TrimSpace(text) == "" {
			return nil, malformedHookInput("empty-tool-input")
		}
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return nil, malformedHookInput("invalid-tool-input-json")
		}
		return decoded, nil
	}
	if value == nil {
		return nil, malformedHookInput("missing-tool-input")
	}
	return value, nil
}

func decodeCursorHook(value []byte) (string, any, error) {
	event, err := decodeJSONObject("cursor", value)
	if err != nil {
		return "", nil, err
	}
	eventName := stringValue(event["hook_event_name"])
	command := stringValue(event["command"])
	toolName := stringValue(event["tool_name"])
	toolInput := event["tool_input"]

	switch eventName {
	case "preToolUse":
		if _, present := event["tool_name"]; !present {
			return "", nil, malformedHookInput("missing-tool-name")
		}
		if strings.TrimSpace(toolName) == "" {
			return "", nil, malformedHookInput("empty-tool-name")
		}
		if toolInput == nil {
			return "", nil, malformedHookInput("missing-tool-input")
		}
		return toolName, toolInput, nil
	case "beforeShellExecution":
		if _, present := event["command"]; !present {
			return "", nil, malformedHookInput("missing-command")
		}
		if strings.TrimSpace(command) == "" {
			return "", nil, malformedHookInput("empty-command")
		}
		return "Bash", map[string]any{"command": command}, nil
	case "beforeMCPExecution":
		if _, present := event["tool_name"]; !present {
			return "", nil, malformedHookInput("missing-tool-name")
		}
		if strings.TrimSpace(toolName) == "" {
			return "", nil, malformedHookInput("empty-tool-name")
		}
		input, inputErr := cursorMCPInput(toolInput)
		if inputErr != nil {
			return "", nil, inputErr
		}
		return toolName, input, nil
	case "":
		// Older Cursor builds omitted hook_event_name. Preserve only the two
		// unambiguous shapes; an MCP transport command must never be classified
		// as the requested tool operation.
		if toolName != "" && toolInput != nil {
			if command != "" {
				return "", nil, malformedHookInput("ambiguous-event")
			}
			input, inputErr := cursorMCPInput(toolInput)
			if inputErr != nil {
				return "", nil, inputErr
			}
			return toolName, input, nil
		}
		if command != "" && toolName == "" {
			return "Bash", map[string]any{"command": command}, nil
		}
		return "", nil, malformedHookInput("missing-command-or-tool")
	default:
		return "", nil, malformedHookInput("unsupported-event")
	}
}

func decodePreToolUseHook(host string, value []byte) (string, any, error) {
	event, err := decodeJSONObject(host, value)
	if err != nil {
		return "", nil, err
	}
	eventName := stringValue(event["hook_event_name"])
	if eventName != "" && eventName != "PreToolUse" && !(host == "claude" && eventName == "preToolUse") {
		return "", nil, malformedHookInput("unsupported-event")
	}
	name := stringValue(event["tool_name"])
	input := event["tool_input"]
	if _, present := event["tool_name"]; !present {
		return "", nil, malformedHookInput("missing-tool-name")
	}
	if strings.TrimSpace(name) == "" {
		return "", nil, malformedHookInput("empty-tool-name")
	}
	if input == nil {
		return "", nil, malformedHookInput("missing-tool-input")
	}
	return name, input, nil
}

func decodeGeminiHook(value []byte) (string, any, error) {
	event, err := decodeJSONObject("gemini", value)
	if err != nil {
		return "", nil, err
	}
	if eventName := stringValue(event["hook_event_name"]); eventName != "" && eventName != "BeforeTool" {
		return "", nil, malformedHookInput("unsupported-event")
	}
	name := stringValue(event["tool_name"])
	if strings.TrimSpace(name) == "" {
		return "", nil, malformedHookInput("missing-tool-name")
	}
	input, present := event["tool_input"]
	if !present || input == nil {
		return "", nil, malformedHookInput("missing-tool-input")
	}
	return name, input, nil
}

func structuredHookDeny(host string, finding SafetyFinding) ([]byte, error) {
	message := denialMessage(host, finding)
	hookOutput := map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": message,
	}
	// Opt-in structured object for hosts that adopt rich denial rendering. Nested
	// inside the host's existing container; the flat reason above is always the
	// complete fallback for any host that ignores it. Off by default (no host
	// documents tolerating unknown keys — see references/host-hook-contracts.md).
	if denialRichEnabled() {
		hookOutput["boatstackDenial"] = denialFor(host, finding).Structured()
	}
	value, err := json.Marshal(map[string]any{"hookSpecificOutput": hookOutput})
	return append(value, '\n'), err
}

var hookHostContracts = map[string]hookHostContract{
	"cursor": {
		decode: decodeCursorHook,
		allow: func() ([]byte, error) {
			value, err := json.Marshal(map[string]any{"continue": true, "permission": "allow"})
			return append(value, '\n'), err
		},
		deny: func(finding SafetyFinding) ([]byte, error) {
			message := denialMessage("cursor", finding)
			payload := map[string]any{
				"continue": true, "permission": "deny", "user_message": message, "agent_message": message,
			}
			if denialRichEnabled() {
				payload["boatstackDenial"] = denialFor("cursor", finding).Structured()
			}
			value, err := json.Marshal(payload)
			return append(value, '\n'), err
		},
	},
	"claude": {
		decode: func(value []byte) (string, any, error) { return decodePreToolUseHook("claude", value) },
		allow:  func() ([]byte, error) { return nil, nil },
		deny:   func(finding SafetyFinding) ([]byte, error) { return structuredHookDeny("claude", finding) },
	},
	"codex": {
		decode: func(value []byte) (string, any, error) { return decodePreToolUseHook("codex", value) },
		allow:  func() ([]byte, error) { return nil, nil },
		deny:   func(finding SafetyFinding) ([]byte, error) { return structuredHookDeny("codex", finding) },
	},
	"gemini": {
		decode: decodeGeminiHook,
		allow: func() ([]byte, error) {
			value, err := json.Marshal(map[string]any{"decision": "allow"})
			return append(value, '\n'), err
		},
		deny: func(finding SafetyFinding) ([]byte, error) {
			payload := map[string]any{"decision": "deny", "reason": denialMessage("gemini", finding)}
			if denialRichEnabled() {
				payload["boatstackDenial"] = denialFor("gemini", finding).Structured()
			}
			value, err := json.Marshal(payload)
			return append(value, '\n'), err
		},
	},
}

// denialMessage renders the human-facing reason string embedded in a host's hook
// decision. It delegates to the structured Denial model (denial.go) and renders
// the plain, multi-line form — the safe default that every host displays. Richer
// treatments (markdown, ANSI, the structured object) are produced from the same
// Denial by the CLI/guard surfaces and the opt-in rich path.
func denialMessage(host string, finding SafetyFinding) string {
	return denialFor(host, finding).Render(RenderPlain)
}

// AmbientHookDecision is the entry point for a developer-level (user-scoped) guard
// that runs for every repository the coding agent opens. It enforces Boatstack only
// on managed repositories — those with a detached attachment or an embedded install
// — and returns a plain allow (no Boatstack decision) everywhere else, so a
// user-level hook never controls an unattached repository. On a managed repository
// it delegates to the full HookDecision.
func AmbientHookDecision(options SafetyHookOptions) ([]byte, bool) {
	host := strings.ToLower(strings.TrimSpace(options.Host))
	contract, supported := hookHostContracts[host]
	repo, err := ResolveRepository(options.Repo)
	if err != nil || !RepositoryIsManaged(repo) {
		if supported {
			value, _ := contract.allow()
			return value, false
		}
		return nil, false
	}
	return HookDecision(options)
}

func HookDecision(options SafetyHookOptions) ([]byte, bool) {
	host := strings.ToLower(strings.TrimSpace(options.Host))
	contract, supported := hookHostContracts[host]
	if !supported {
		finding := SafetyFinding{Category: "unsupported-host", Reason: "unknown host is denied by the fail-closed guard", Source: "hook"}
		value, _ := structuredHookDeny("codex", finding)
		return value, true
	}
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		finding := SafetyFinding{Category: "unresolved-repository", Reason: "repository identity could not be established", Source: "hook"}
		value, _ := contract.deny(finding)
		return value, true
	}
	if handled, malformed := completeSupervisedToolEvent(repo, host, options.Input); handled {
		if malformed {
			finding := SafetyFinding{Category: "malformed-tool-input", Reason: "invalid-post-event", Source: "hook"}
			value, _ := contract.deny(finding)
			return value, true
		}
		value, _ := contract.allow()
		return value, false
	}
	name, input, err := contract.decode(options.Input)
	if err != nil {
		reason := "invalid-event"
		var decodeErr hookDecodeError
		if errors.As(err, &decodeErr) {
			reason = decodeErr.code
		}
		finding := SafetyFinding{Category: "malformed-tool-input", Reason: reason, Source: "hook"}
		value, _ := contract.deny(finding)
		return value, true
	}
	findings := ClassifyTool(repo, name, input)
	if len(findings) == 0 {
		if finding := superviseToolAttempt(repo, host, name, input, options.Input); finding != nil {
			value, _ := contract.deny(*finding)
			return value, true
		}
		value, _ := contract.allow()
		return value, false
	}
	value, _ := contract.deny(findings[0])
	return value, true
}

func operationalChangedFiles(repo string, highRisk []string, defaultBranch string) ([]string, error) {
	diffStart := "HEAD"
	if strings.TrimSpace(defaultBranch) != "" {
		if head, headErr := gitCommand(repo, "branch", "--show-current"); headErr == nil && head != defaultBranch {
			if baseCommit, baseErr := resolveBaseCommit(repo, defaultBranch); baseErr == nil {
				if mergeBase, mergeErr := gitCommand(repo, "merge-base", baseCommit, "HEAD"); mergeErr == nil && mergeBase != "" {
					diffStart = mergeBase
				}
			}
		}
	}
	command := exec.Command("git", "-C", repo, "diff", "--name-only", "--diff-filter=ACMR", diffStart)
	value, err := command.Output()
	if err != nil {
		return nil, err
	}
	untrackedCommand := exec.Command("git", "-C", repo, "ls-files", "--others", "--exclude-standard")
	untracked, err := untrackedCommand.Output()
	if err != nil {
		return nil, err
	}
	paths := []string{}
	seen := map[string]bool{}
	all := strings.TrimSpace(string(value)) + "\n" + strings.TrimSpace(string(untracked))
	for _, path := range strings.Split(all, "\n") {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		matched := operationalPathPattern.MatchString(path)
		for _, pattern := range highRisk {
			if ok, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(path)); ok {
				matched = true
			}
			prefix := strings.TrimSuffix(filepath.ToSlash(pattern), "/**")
			if prefix != pattern && strings.HasPrefix(path, prefix+"/") {
				matched = true
			}
		}
		if matched {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func CheckRepositorySafety(repoPath string) (SafetyReport, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return SafetyReport{}, err
	}
	highRisk := []string{}
	defaultBranch := ""
	configPath := WorkspaceFor(repo).ProjectConfigPath()
	if value, readErr := os.ReadFile(configPath); readErr == nil {
		var config ProjectConfig
		if json.Unmarshal(value, &config) == nil {
			highRisk = config.Project.HighRiskPaths
			defaultBranch = config.Project.DefaultBranch
		}
	}
	paths, err := operationalChangedFiles(repo, highRisk, defaultBranch)
	if err != nil {
		return SafetyReport{}, err
	}
	findings := []SafetyFinding{}
	for _, relative := range paths {
		value, readErr := os.ReadFile(filepath.Join(repo, filepath.FromSlash(relative)))
		if readErr != nil {
			return SafetyReport{}, readErr
		}
		// A committed file in the delivery diff is a DATA ARTIFACT, not an execution:
		// a declarative migration .sql or a schema dump is applied later by the
		// controlled deploy pipeline (the operator boundary), so its bare SQL is not a
		// capability the agent is exercising now (scanSQL=false). Self-executing
		// destruction committed into a script (supabase db reset, terraform destroy)
		// still blocks, because those rules name their own executor.
		findings = append(findings, classifySafetyText(string(value), relative, false)...)
	}
	findings = dedupeFindings(findings)
	status := "PASS"
	if len(findings) > 0 {
		status = "BLOCKED"
	}
	return SafetyReport{Status: status, Findings: findings}, nil
}
