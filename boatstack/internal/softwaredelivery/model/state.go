package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const SnapshotSchemaVersion = 5

const foregroundWorkContextIdentity = "foreground-work-context"

type ProtocolPhase string

const (
	PhaseDormant           ProtocolPhase = "DORMANT"
	PhaseObserved          ProtocolPhase = "OBSERVED"
	PhasePrescribed        ProtocolPhase = "PRESCRIBED"
	PhaseAdmitted          ProtocolPhase = "ADMITTED"
	PhaseExecutingLocal    ProtocolPhase = "EXECUTING_LOCAL"
	PhaseExecutingExternal ProtocolPhase = "EXECUTING_EXTERNAL"
	PhaseVerifying         ProtocolPhase = "VERIFYING"
	PhaseActive            ProtocolPhase = "ACTIVE"
	PhaseRecovery          ProtocolPhase = "RECOVERY"
	PhaseUnresolved        ProtocolPhase = "UNRESOLVED"
	PhaseFrontier          ProtocolPhase = "FRONTIER"
	PhaseTerminal          ProtocolPhase = "TERMINAL"
	PhaseAbandoned         ProtocolPhase = "ABANDONED"
)

var orderedProtocolPhases = []ProtocolPhase{
	PhaseDormant, PhaseObserved, PhasePrescribed, PhaseAdmitted,
	PhaseExecutingLocal, PhaseExecutingExternal, PhaseVerifying,
	PhaseActive, PhaseRecovery, PhaseUnresolved, PhaseFrontier,
	PhaseTerminal, PhaseAbandoned,
}

var protocolPhases = func() map[ProtocolPhase]struct{} {
	result := make(map[ProtocolPhase]struct{}, len(orderedProtocolPhases))
	for _, phase := range orderedProtocolPhases {
		result[phase] = struct{}{}
	}
	return result
}()

func (p ProtocolPhase) Valid() bool { _, ok := protocolPhases[p]; return ok }

// ProtocolPhases returns the kernel-owned canonical protocol order. Surfaces
// consume this projection instead of defining their own lifecycle ordering.
func ProtocolPhases() []ProtocolPhase {
	return append([]ProtocolPhase(nil), orderedProtocolPhases...)
}

// IsCompletionTarget identifies phases that close nonblockingness without
// pretending that frontier or abandonment is successful delivery.
func (p ProtocolPhase) IsCompletionTarget() bool {
	return p == PhaseFrontier || p == PhaseTerminal || p == PhaseAbandoned
}

type EngagementState string

const (
	EngagementDormant     EngagementState = "dormant"
	EngagementCommand     EngagementState = "command"
	EngagementActive      EngagementState = "active"
	EngagementStale       EngagementState = "stale"
	EngagementConflicting EngagementState = "conflicting"
	EngagementInvalid     EngagementState = "invalid"
)

func (s EngagementState) Valid() bool {
	switch s {
	case EngagementDormant, EngagementCommand, EngagementActive, EngagementStale, EngagementConflicting, EngagementInvalid:
		return true
	default:
		return false
	}
}

type DeliveryState string

const (
	DeliveryUninitialized DeliveryState = "uninitialized"
	DeliveryPlanning      DeliveryState = "planning"
	DeliveryApproved      DeliveryState = "approved"
	DeliveryActive        DeliveryState = "active"
	DeliveryGatesPassed   DeliveryState = "gates-passed"
	DeliveryPublished     DeliveryState = "published"
	DeliveryAmendment     DeliveryState = "amendment"
	DeliveryInvalid       DeliveryState = "invalid"
	DeliveryRecovery      DeliveryState = "recovery"
	DeliveryDiscarded     DeliveryState = "discarded"
	DeliveryTerminal      DeliveryState = "terminal"
)

func (s DeliveryState) Valid() bool {
	switch s {
	case DeliveryUninitialized, DeliveryPlanning, DeliveryApproved, DeliveryActive, DeliveryGatesPassed, DeliveryPublished, DeliveryAmendment, DeliveryInvalid, DeliveryRecovery, DeliveryDiscarded, DeliveryTerminal:
		return true
	default:
		return false
	}
}

type WorkspaceState string

const (
	WorkspaceAbsent            WorkspaceState = "absent"
	WorkspaceCut               WorkspaceState = "cut"
	WorkspaceActive            WorkspaceState = "active"
	WorkspacePublished         WorkspaceState = "published"
	WorkspaceLanded            WorkspaceState = "landed"
	WorkspaceAbandoned         WorkspaceState = "abandoned"
	WorkspaceAttentionRequired WorkspaceState = "attention-required"
)

func (s WorkspaceState) Valid() bool {
	switch s {
	case WorkspaceAbsent, WorkspaceCut, WorkspaceActive, WorkspacePublished, WorkspaceLanded, WorkspaceAbandoned, WorkspaceAttentionRequired:
		return true
	default:
		return false
	}
}

type PlanState string

const (
	PlanAbsent            PlanState = "absent"
	PlanDraft             PlanState = "draft"
	PlanValid             PlanState = "valid"
	PlanPackageApproved   PlanState = "package-approved"
	PlanApproved          PlanState = "approved"
	PlanLocked            PlanState = "locked"
	PlanStale             PlanState = "stale"
	PlanInvalid           PlanState = "invalid"
	PlanAmendmentRequired PlanState = "amendment-required"
)

func (s PlanState) Valid() bool {
	switch s {
	case PlanAbsent, PlanDraft, PlanValid, PlanPackageApproved, PlanApproved, PlanLocked, PlanStale, PlanInvalid, PlanAmendmentRequired:
		return true
	default:
		return false
	}
}

type ConfigurationState string

const (
	ConfigurationVerified    ConfigurationState = "verified"
	ConfigurationStale       ConfigurationState = "stale"
	ConfigurationDivergent   ConfigurationState = "divergent"
	ConfigurationConflicting ConfigurationState = "conflicting"
	ConfigurationUnsupported ConfigurationState = "unsupported"
)

func (s ConfigurationState) Valid() bool {
	switch s {
	case ConfigurationVerified, ConfigurationStale, ConfigurationDivergent, ConfigurationConflicting, ConfigurationUnsupported:
		return true
	default:
		return false
	}
}

// ConfigurationPolicy is the control-relevant projection of the strict Boatstack
// project document. Keeping it in the canonical snapshot prevents policy bytes
// from being validated but then ignored by admission or terminal logic.
type ConfigurationPolicy struct {
	PlanApproval                 string   `json:"plan_approval"`
	IndependentReviewForHighRisk bool     `json:"independent_review_for_high_risk,omitempty"`
	HighRiskChange               bool     `json:"high_risk_change,omitempty"`
	VisualEvidence               string   `json:"visual_evidence"`
	ExternalEffectAuthority      string   `json:"external_effect_authority"`
	Hosts                        []string `json:"hosts"`
}

func (p ConfigurationPolicy) Canonical() ConfigurationPolicy {
	p.Hosts = append([]string(nil), p.Hosts...)
	sort.Strings(p.Hosts)
	return p
}

func (p ConfigurationPolicy) Validate() error {
	if p.PlanApproval != "human" && p.PlanApproval != "human-or-autonomy" {
		return fmt.Errorf("configuration policy has invalid plan approval %q", p.PlanApproval)
	}
	if p.VisualEvidence != "off" && p.VisualEvidence != "optional" && p.VisualEvidence != "required" {
		return fmt.Errorf("configuration policy has invalid visual evidence %q", p.VisualEvidence)
	}
	if p.ExternalEffectAuthority != "human-or-autonomy-plus-provider" {
		return fmt.Errorf("configuration policy has invalid external authority %q", p.ExternalEffectAuthority)
	}
	if len(p.Hosts) == 0 {
		return fmt.Errorf("configuration policy requires enabled hosts")
	}
	seen := map[string]bool{}
	for _, host := range p.Hosts {
		if host == "" || seen[host] {
			return fmt.Errorf("configuration policy has empty or duplicate host %q", host)
		}
		seen[host] = true
	}
	if !seen["cli"] {
		return fmt.Errorf("configuration policy requires cli host")
	}
	return nil
}

type RuntimeState string

const (
	RuntimeAbsent             RuntimeState = "absent"
	RuntimeHydrating          RuntimeState = "hydrating"
	RuntimeVerified           RuntimeState = "verified"
	RuntimeStale              RuntimeState = "stale"
	RuntimeInvalid            RuntimeState = "invalid"
	RuntimeConflicting        RuntimeState = "conflicting"
	RuntimeWrongSource        RuntimeState = "wrong-source"
	RuntimePartiallyPublished RuntimeState = "partially-published"
)

func (s RuntimeState) Valid() bool {
	switch s {
	case RuntimeAbsent, RuntimeHydrating, RuntimeVerified, RuntimeStale, RuntimeInvalid, RuntimeConflicting, RuntimeWrongSource, RuntimePartiallyPublished:
		return true
	default:
		return false
	}
}

type PublicationState string

const (
	PublicationNone               PublicationState = "none"
	PublicationCandidate          PublicationState = "candidate"
	PublicationOpen               PublicationState = "open"
	PublicationClosedUnmerged     PublicationState = "closed-unmerged"
	PublicationMerged             PublicationState = "merged"
	PublicationUnavailable        PublicationState = "unavailable"
	PublicationConflicting        PublicationState = "conflicting"
	PublicationPublishedNotLanded PublicationState = "published-not-landed"
)

func (s PublicationState) Valid() bool {
	switch s {
	case PublicationNone, PublicationCandidate, PublicationOpen, PublicationClosedUnmerged, PublicationMerged, PublicationUnavailable, PublicationConflicting, PublicationPublishedNotLanded:
		return true
	default:
		return false
	}
}

type VerificationState string

const (
	VerificationUnverified VerificationState = "unverified"
	VerificationCurrent    VerificationState = "current"
	VerificationStale      VerificationState = "stale"
	VerificationFailed     VerificationState = "failed"
	VerificationUnresolved VerificationState = "unresolved"
)

func (s VerificationState) Valid() bool {
	switch s {
	case VerificationUnverified, VerificationCurrent, VerificationStale, VerificationFailed, VerificationUnresolved:
		return true
	default:
		return false
	}
}

type RecoveryState string

const (
	RecoveryNone         RecoveryState = "none"
	RecoveryResumable    RecoveryState = "resumable"
	RecoveryRollback     RecoveryState = "rollback"
	RecoveryCompensation RecoveryState = "compensation"
	RecoveryReconcile    RecoveryState = "reconcile"
	RecoveryEscalated    RecoveryState = "escalated"
)

func (s RecoveryState) Valid() bool {
	switch s {
	case RecoveryNone, RecoveryResumable, RecoveryRollback, RecoveryCompensation, RecoveryReconcile, RecoveryEscalated:
		return true
	default:
		return false
	}
}

type TransactionState string

const (
	TransactionNone              TransactionState = "none"
	TransactionStaged            TransactionState = "staged"
	TransactionLocalApplied      TransactionState = "local-applied"
	TransactionExternalUncertain TransactionState = "external-uncertain"
	TransactionVerifying         TransactionState = "verifying"
	TransactionCommitted         TransactionState = "committed"
	TransactionCompensating      TransactionState = "compensating"
)

func (s TransactionState) Valid() bool {
	switch s {
	case TransactionNone, TransactionStaged, TransactionLocalApplied, TransactionExternalUncertain, TransactionVerifying, TransactionCommitted, TransactionCompensating:
		return true
	default:
		return false
	}
}

// RecoveryContext records why the controller entered recovery and the bounded
// exits that remain legal. It is separate from RecoveryState so a coarse label
// can never erase transaction identity or the resumption target.
type RecoveryContext struct {
	TransactionID   string        `json:"transaction_id"`
	Cause           string        `json:"cause"`
	SourcePhase     ProtocolPhase `json:"source_phase"`
	Permitted       []string      `json:"permitted"`
	BudgetRemaining int           `json:"budget_remaining"`
	Resumption      ProtocolPhase `json:"resumption"`
}

func (c RecoveryContext) Validate() error {
	if c.TransactionID == "" || c.Cause == "" || !c.SourcePhase.Valid() || !c.Resumption.Valid() || len(c.Permitted) == 0 || c.BudgetRemaining < 0 {
		return fmt.Errorf("recovery context requires transaction, cause, source, permitted exits, budget, and resumption target")
	}
	return nil
}

// TransactionContext identifies the exact interrupted transition and resources
// without embedding their bytes in the canonical snapshot.
type TransactionContext struct {
	ID               string   `json:"id"`
	TransitionID     string   `json:"transition_id"`
	Status           string   `json:"status"`
	ResourceDigests  []string `json:"resource_digests,omitempty"`
	ExternalPossible bool     `json:"external_possible,omitempty"`
}

func (c TransactionContext) Validate() error {
	if c.ID == "" || c.TransitionID == "" || c.Status == "" {
		return fmt.Errorf("transaction context requires id, transition, and status")
	}
	return nil
}

type TerminalStatus string

const (
	TerminalNonterminal TerminalStatus = "nonterminal"
	TerminalEstablished TerminalStatus = "established"
	TerminalStale       TerminalStatus = "stale"
	TerminalUnknown     TerminalStatus = "unknown"
	TerminalConflicting TerminalStatus = "conflicting"
)

func (s TerminalStatus) Valid() bool {
	switch s {
	case TerminalNonterminal, TerminalEstablished, TerminalStale, TerminalUnknown, TerminalConflicting:
		return true
	default:
		return false
	}
}

type ProgramState string

const (
	ProgramUnbound ProgramState = "unbound"
	ProgramCurrent ProgramState = "current"
	ProgramDrift   ProgramState = "drift"
)

func (s ProgramState) Valid() bool {
	switch s {
	case ProgramUnbound, ProgramCurrent, ProgramDrift:
		return true
	default:
		return false
	}
}

// Observation is the read-only plant result before canonical validation and
// fingerprinting.
type Observation struct {
	SchemaVersion              int                       `json:"schema_version"`
	StateRevision              uint64                    `json:"state_revision"`
	ProgramFingerprint         string                    `json:"program_fingerprint,omitempty"`
	RecordedProgramFingerprint string                    `json:"recorded_program_fingerprint,omitempty"`
	Invocation                 InvocationContext         `json:"invocation"`
	Program                    Fact[ProgramState]        `json:"program"`
	Phase                      Fact[ProtocolPhase]       `json:"phase"`
	Engagement                 Fact[EngagementState]     `json:"engagement"`
	Delivery                   Fact[DeliveryState]       `json:"delivery"`
	Workspace                  Fact[WorkspaceState]      `json:"workspace"`
	Plan                       Fact[PlanState]           `json:"plan"`
	Configuration              Fact[ConfigurationState]  `json:"configuration"`
	ConfigurationPolicy        Fact[ConfigurationPolicy] `json:"configuration_policy"`
	Runtime                    Fact[RuntimeState]        `json:"runtime"`
	Publication                Fact[PublicationState]    `json:"publication"`
	Verification               Fact[VerificationState]   `json:"verification"`
	Recovery                   Fact[RecoveryState]       `json:"recovery"`
	Transaction                Fact[TransactionState]    `json:"transaction"`
	RecoveryInfo               Fact[RecoveryContext]     `json:"recovery_info"`
	TransactionInfo            Fact[TransactionContext]  `json:"transaction_info"`
	Terminal                   Fact[TerminalStatus]      `json:"terminal"`
	Objective                  Fact[Objective]           `json:"objective"`
	ProgramFacts               map[string]Fact[string]   `json:"flow_facts,omitempty"`
	ExtensionFacts             map[string]Fact[string]   `json:"extension_facts,omitempty"`
	ObservedAt                 time.Time                 `json:"observed_at"`
}

// ExecutableRuntimeAdmitted reports whether repository-selected executable
// observers may run for this exact compiled program.
func (o Observation) ExecutableRuntimeAdmitted(programFingerprint string) bool {
	return o.RecordedProgramFingerprint == programFingerprint &&
		o.Configuration.Status == FactKnown && o.Configuration.Value == ConfigurationVerified
}

type Snapshot struct {
	Observation
	Fingerprint string `json:"fingerprint"`
}

func CanonicalizeForProgram(observation Observation, programFingerprint string) (Snapshot, error) {
	if len(programFingerprint) != 64 {
		return Snapshot{}, fmt.Errorf("snapshot: invalid compiled program fingerprint")
	}
	observation.ProgramFingerprint = programFingerprint
	state := ProgramUnbound
	if observation.RecordedProgramFingerprint != "" {
		state = ProgramDrift
		if observation.RecordedProgramFingerprint == programFingerprint {
			state = ProgramCurrent
		}
	}
	observation.Program = Known(state, Evidence{
		Source: "compiled-control-program", Fingerprint: programFingerprint, ObservedAt: observation.ObservedAt,
	})
	return Canonicalize(observation)
}

func Canonicalize(observation Observation) (Snapshot, error) {
	if observation.SchemaVersion != SnapshotSchemaVersion {
		return Snapshot{}, fmt.Errorf("snapshot: schema version %d, want %d", observation.SchemaVersion, SnapshotSchemaVersion)
	}
	if observation.StateRevision == 0 {
		return Snapshot{}, fmt.Errorf("snapshot: durable state revision is required")
	}
	if observation.Program.Status == "" && observation.ProgramFingerprint == "" {
		observation.Program = Known(ProgramUnbound, Evidence{Source: "control-program:unbound", Fingerprint: "unbound", ObservedAt: observation.ObservedAt})
	}
	if observation.ProgramFingerprint != "" && len(observation.ProgramFingerprint) != 64 {
		return Snapshot{}, fmt.Errorf("snapshot: invalid program fingerprint")
	}
	if err := observation.Invocation.Validate(false); err != nil {
		return Snapshot{}, err
	}
	checks := []struct {
		name string
		err  error
	}{
		{"program", observation.Program.Validate("program")},
		{"phase", observation.Phase.Validate("phase")},
		{"engagement", observation.Engagement.Validate("engagement")},
		{"delivery", observation.Delivery.Validate("delivery")},
		{"workspace", observation.Workspace.Validate("workspace")},
		{"plan", observation.Plan.Validate("plan")},
		{"configuration", observation.Configuration.Validate("configuration")},
		{"configuration_policy", observation.ConfigurationPolicy.Validate("configuration_policy")},
		{"runtime", observation.Runtime.Validate("runtime")},
		{"publication", observation.Publication.Validate("publication")},
		{"verification", observation.Verification.Validate("verification")},
		{"recovery", observation.Recovery.Validate("recovery")},
		{"transaction", observation.Transaction.Validate("transaction")},
		{"recovery_info", observation.RecoveryInfo.Validate("recovery_info")},
		{"transaction_info", observation.TransactionInfo.Validate("transaction_info")},
		{"terminal", observation.Terminal.Validate("terminal")},
		{"objective", observation.Objective.Validate("objective")},
	}
	for _, check := range checks {
		if check.err != nil {
			return Snapshot{}, check.err
		}
	}
	if observation.Phase.Status == FactKnown && !observation.Phase.Value.Valid() {
		return Snapshot{}, fmt.Errorf("snapshot: invalid protocol phase %q", observation.Phase.Value)
	}
	valueChecks := []struct {
		name  string
		known bool
		valid bool
	}{
		{"program", observation.Program.Status == FactKnown, observation.Program.Value.Valid()},
		{"engagement", observation.Engagement.Status == FactKnown, observation.Engagement.Value.Valid()},
		{"delivery", observation.Delivery.Status == FactKnown, observation.Delivery.Value.Valid()},
		{"workspace", observation.Workspace.Status == FactKnown, observation.Workspace.Value.Valid()},
		{"plan", observation.Plan.Status == FactKnown, observation.Plan.Value.Valid()},
		{"configuration", observation.Configuration.Status == FactKnown, observation.Configuration.Value.Valid()},
		{"runtime", observation.Runtime.Status == FactKnown, observation.Runtime.Value.Valid()},
		{"publication", observation.Publication.Status == FactKnown, observation.Publication.Value.Valid()},
		{"verification", observation.Verification.Status == FactKnown, observation.Verification.Value.Valid()},
		{"recovery", observation.Recovery.Status == FactKnown, observation.Recovery.Value.Valid()},
		{"transaction", observation.Transaction.Status == FactKnown, observation.Transaction.Value.Valid()},
		{"terminal", observation.Terminal.Status == FactKnown, observation.Terminal.Value.Valid()},
	}
	for _, check := range valueChecks {
		if check.known && !check.valid {
			return Snapshot{}, fmt.Errorf("snapshot: invalid %s value", check.name)
		}
	}
	if observation.ObservedAt.IsZero() {
		return Snapshot{}, fmt.Errorf("snapshot: observed time is required")
	}
	if observation.Phase.Status == FactKnown && observation.Phase.Value == PhaseRecovery && observation.Recovery.Status == FactKnown && observation.Recovery.Value == RecoveryNone {
		return Snapshot{}, fmt.Errorf("snapshot: recovery phase requires a recovery state")
	}
	if observation.Phase.Status == FactKnown && observation.Phase.Value == PhaseRecovery {
		if observation.RecoveryInfo.Status != FactKnown || observation.RecoveryInfo.Value.Validate() != nil {
			return Snapshot{}, fmt.Errorf("snapshot: recovery phase requires complete recovery context")
		}
	}
	if observation.Recovery.Status == FactKnown && observation.Recovery.Value != RecoveryNone {
		if observation.RecoveryInfo.Status != FactKnown || observation.RecoveryInfo.Value.Validate() != nil {
			return Snapshot{}, fmt.Errorf("snapshot: non-empty recovery state requires complete recovery context")
		}
	}
	if observation.Transaction.Status == FactKnown && observation.Transaction.Value != TransactionNone {
		if observation.TransactionInfo.Status != FactKnown || observation.TransactionInfo.Value.Validate() != nil {
			return Snapshot{}, fmt.Errorf("snapshot: active transaction requires complete transaction context")
		}
	}
	if observation.Terminal.Status == FactKnown && observation.Terminal.Value == TerminalEstablished && observation.Phase.Status == FactKnown && observation.Phase.Value != PhaseTerminal && observation.Phase.Value != PhaseAbandoned {
		return Snapshot{}, fmt.Errorf("snapshot: established terminal evidence requires terminal or abandoned phase")
	}
	if observation.Terminal.Status == FactKnown && observation.Terminal.Value == TerminalEstablished {
		if observation.Objective.Status != FactKnown || observation.Objective.Value.Validate() != nil {
			return Snapshot{}, fmt.Errorf("snapshot: established terminal evidence requires an exact configured objective")
		}
		if observation.Delivery.Status == FactKnown && observation.Delivery.Value != DeliveryTerminal && observation.Delivery.Value != DeliveryDiscarded {
			return Snapshot{}, fmt.Errorf("snapshot: established terminal evidence requires terminal or discarded delivery")
		}
	}
	if observation.Workspace.Status == FactKnown && observation.Workspace.Value == WorkspaceLanded && observation.Publication.Status == FactKnown && observation.Publication.Value != PublicationMerged {
		return Snapshot{}, fmt.Errorf("snapshot: landed workspace requires merged publication evidence")
	}
	if observation.Phase.Status == FactKnown && observation.Phase.Value == PhaseActive && observation.Engagement.Status == FactKnown && observation.Engagement.Value == EngagementDormant {
		return Snapshot{}, fmt.Errorf("snapshot: active protocol phase cannot have dormant engagement")
	}
	if observation.Objective.Status == FactKnown {
		if err := observation.Objective.Value.Validate(); err != nil {
			return Snapshot{}, fmt.Errorf("snapshot: invalid objective fact: %w", err)
		}
	}
	for id, fact := range observation.ProgramFacts {
		if !FacetName(id).Valid() || controllingFacet(FacetName(id)) {
			return Snapshot{}, fmt.Errorf("snapshot: invalid control-program fact id %q", id)
		}
		if err := fact.Validate("control-program fact " + id); err != nil {
			return Snapshot{}, err
		}
	}
	for id, fact := range observation.ExtensionFacts {
		if !FacetName(id).Valid() || controllingFacet(FacetName(id)) {
			return Snapshot{}, fmt.Errorf("snapshot: invalid extension fact id %q", id)
		}
		if err := fact.Validate("extension fact " + id); err != nil {
			return Snapshot{}, err
		}
	}
	if observation.ConfigurationPolicy.Status == FactKnown {
		if err := observation.ConfigurationPolicy.Value.Validate(); err != nil {
			return Snapshot{}, fmt.Errorf("snapshot: invalid configuration policy: %w", err)
		}
		observation.ConfigurationPolicy.Value = observation.ConfigurationPolicy.Value.Canonical()
	}
	if observation.RecoveryInfo.Status == FactKnown {
		if err := observation.RecoveryInfo.Value.Validate(); err != nil {
			return Snapshot{}, fmt.Errorf("snapshot: invalid recovery context: %w", err)
		}
	}
	if observation.TransactionInfo.Status == FactKnown {
		if err := observation.TransactionInfo.Value.Validate(); err != nil {
			return Snapshot{}, fmt.Errorf("snapshot: invalid transaction context: %w", err)
		}
	}
	snapshot := Snapshot{Observation: observation}
	fingerprint, err := observationFingerprint(observation)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Fingerprint = fingerprint
	return snapshot, nil
}

// ForegroundWorkContextFingerprint binds long-running work to the durable and
// admission-relevant observation while excluding invocation-local driver IDs.
// A restart may change controller, host, correlation, or the executable's
// installation path without changing the repository, runtime, program, or
// control state that the work was authorized to inspect.
func ForegroundWorkContextFingerprint(snapshot Snapshot) (string, error) {
	projection := snapshot.Observation
	projection.Invocation.ControllerID = foregroundWorkContextIdentity
	projection.Invocation.Host = foregroundWorkContextIdentity
	projection.Invocation.Correlation = foregroundWorkContextIdentity
	projection.Invocation.RuntimePath = ""
	return observationFingerprint(projection)
}

func observationFingerprint(observation Observation) (string, error) {
	projection := observation
	projection.ObservedAt = time.Time{}
	zeroEvidenceTimes(&projection.Program)
	zeroEvidenceTimes(&projection.Phase)
	zeroEvidenceTimes(&projection.Engagement)
	zeroEvidenceTimes(&projection.Delivery)
	zeroEvidenceTimes(&projection.Workspace)
	zeroEvidenceTimes(&projection.Plan)
	zeroEvidenceTimes(&projection.Configuration)
	zeroEvidenceTimes(&projection.ConfigurationPolicy)
	zeroEvidenceTimes(&projection.Runtime)
	zeroEvidenceTimes(&projection.Publication)
	zeroEvidenceTimes(&projection.Verification)
	zeroEvidenceTimes(&projection.Recovery)
	zeroEvidenceTimes(&projection.Transaction)
	zeroEvidenceTimes(&projection.RecoveryInfo)
	zeroEvidenceTimes(&projection.TransactionInfo)
	zeroEvidenceTimes(&projection.Terminal)
	zeroEvidenceTimes(&projection.Objective)
	for id, fact := range projection.ProgramFacts {
		zeroEvidenceTimes(&fact)
		projection.ProgramFacts[id] = fact
	}
	for id, fact := range projection.ExtensionFacts {
		zeroEvidenceTimes(&fact)
		projection.ExtensionFacts[id] = fact
	}
	raw, err := json.Marshal(Snapshot{Observation: projection})
	if err != nil {
		return "", fmt.Errorf("snapshot: canonical encoding: %w", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func zeroEvidenceTimes[T any](fact *Fact[T]) {
	fact.Evidence = append([]Evidence(nil), fact.Evidence...)
	for index := range fact.Evidence {
		fact.Evidence[index].ObservedAt = time.Time{}
	}
}
