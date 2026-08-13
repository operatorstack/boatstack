package durable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

const StateSchemaVersion = 5

type GateEvidence struct {
	Gate        string `json:"gate"`
	Revision    string `json:"revision"`
	Fingerprint string `json:"fingerprint"`
}

type State struct {
	SchemaVersion         int                      `json:"schema_version"`
	RepositoryID          string                   `json:"repository_id"`
	GitCommonID           string                   `json:"git_common_id"`
	WorktreeID            string                   `json:"worktree_id"`
	ProgramFingerprint    string                   `json:"program_fingerprint,omitempty"`
	Revision              uint64                   `json:"revision"`
	Phase                 model.ProtocolPhase      `json:"phase"`
	Engagement            model.EngagementState    `json:"engagement"`
	Delivery              model.DeliveryState      `json:"delivery"`
	Workspace             model.WorkspaceState     `json:"workspace"`
	Plan                  model.PlanState          `json:"plan"`
	Configuration         model.ConfigurationState `json:"configuration"`
	Runtime               model.RuntimeState       `json:"runtime"`
	Publication           model.PublicationState   `json:"publication"`
	Verification          model.VerificationState  `json:"verification"`
	Recovery              model.RecoveryState      `json:"recovery"`
	Transaction           model.TransactionState   `json:"transaction"`
	Terminal              model.TerminalStatus     `json:"terminal"`
	Objective             model.Objective          `json:"objective"`
	SourceRevision        string                   `json:"source_revision,omitempty"`
	WorktreeFingerprint   string                   `json:"worktree_fingerprint,omitempty"`
	ConfigFingerprint     string                   `json:"config_fingerprint,omitempty"`
	PlanApprovalPolicy    string                   `json:"plan_approval_policy,omitempty"`
	VisualEvidencePolicy  string                   `json:"visual_evidence_policy,omitempty"`
	ExternalEffectPolicy  string                   `json:"external_effect_policy,omitempty"`
	IndependentReview     bool                     `json:"independent_review_for_high_risk,omitempty"`
	EnabledHosts          []string                 `json:"enabled_hosts,omitempty"`
	RuntimeVersion        string                   `json:"runtime_version,omitempty"`
	RuntimeFingerprint    string                   `json:"runtime_fingerprint,omitempty"`
	RuntimeSource         string                   `json:"runtime_source_revision,omitempty"`
	PlanFingerprint       string                   `json:"plan_fingerprint,omitempty"`
	ApprovalFingerprint   string                   `json:"approval_fingerprint,omitempty"`
	WorkspaceBranch       string                   `json:"workspace_branch,omitempty"`
	WorkspacePath         string                   `json:"workspace_path,omitempty"`
	WorkspaceBaseRef      string                   `json:"workspace_base_ref,omitempty"`
	WorkspaceSourcePath   string                   `json:"workspace_source_path,omitempty"`
	WorkspaceSourceID     string                   `json:"workspace_source_worktree_id,omitempty"`
	WorkspaceSourceRef    string                   `json:"workspace_source_ref,omitempty"`
	PublicationID         string                   `json:"publication_id,omitempty"`
	PublicationURL        string                   `json:"publication_url,omitempty"`
	PreviewFingerprint    string                   `json:"preview_fingerprint,omitempty"`
	TransactionID         string                   `json:"transaction_id,omitempty"`
	TransactionTransition string                   `json:"transaction_transition,omitempty"`
	RecoveryCause         string                   `json:"recovery_cause,omitempty"`
	RecoverySourcePhase   model.ProtocolPhase      `json:"recovery_source_phase,omitempty"`
	RecoveryResumption    model.ProtocolPhase      `json:"recovery_resumption,omitempty"`
	RecoveryBudget        int                      `json:"recovery_budget_remaining,omitempty"`
	LastTransition        catalog.TransitionID     `json:"last_transition,omitempty"`
	Gates                 []GateEvidence           `json:"gates,omitempty"`
	UpdatedAt             time.Time                `json:"updated_at"`
}

func Default(invocation model.InvocationContext, now time.Time) State {
	return State{
		SchemaVersion: StateSchemaVersion, RepositoryID: invocation.RepositoryID, GitCommonID: invocation.GitCommonID, WorktreeID: invocation.WorktreeID,
		Revision: 1, Phase: model.PhaseDormant, Engagement: model.EngagementDormant, Delivery: model.DeliveryUninitialized,
		Workspace: model.WorkspaceAbsent, Plan: model.PlanAbsent, Configuration: model.ConfigurationUnsupported,
		Runtime: model.RuntimeAbsent, Publication: model.PublicationNone, Verification: model.VerificationUnverified,
		Recovery: model.RecoveryNone, Transaction: model.TransactionNone, Terminal: model.TerminalNonterminal, UpdatedAt: now.UTC(),
	}
}

func NextRevision(current uint64) (uint64, error) {
	if current == 0 {
		return 0, fmt.Errorf("durable state revision is absent")
	}
	if current == ^uint64(0) {
		return 0, fmt.Errorf("durable state revision overflow")
	}
	return current + 1, nil
}

func (s State) Validate() error {
	if s.SchemaVersion != StateSchemaVersion {
		return fmt.Errorf("durable state schema %d, want %d", s.SchemaVersion, StateSchemaVersion)
	}
	if s.RepositoryID == "" || s.GitCommonID == "" || s.WorktreeID == "" || s.Revision == 0 || s.UpdatedAt.IsZero() {
		return fmt.Errorf("durable state identity, revision, and update time are required")
	}
	if s.ProgramFingerprint != "" && len(s.ProgramFingerprint) != 64 {
		return fmt.Errorf("durable state has invalid program fingerprint")
	}
	if !s.Phase.Valid() || !s.Engagement.Valid() || !s.Delivery.Valid() || !s.Workspace.Valid() || !s.Plan.Valid() ||
		!s.Configuration.Valid() || !s.Runtime.Valid() || !s.Publication.Valid() || !s.Verification.Valid() ||
		!s.Recovery.Valid() || !s.Transaction.Valid() || !s.Terminal.Valid() {
		return fmt.Errorf("durable state contains an invalid controlling value")
	}
	if s.Phase == model.PhaseRecovery && s.Recovery == model.RecoveryNone {
		return fmt.Errorf("durable recovery phase has no recovery classification")
	}
	if s.Recovery != model.RecoveryNone && (s.TransactionID == "" || s.RecoveryCause == "" || !s.RecoverySourcePhase.Valid() || !s.RecoveryResumption.Valid() || s.RecoveryBudget < 0) {
		return fmt.Errorf("durable recovery state has incomplete recovery context")
	}
	if s.Transaction != model.TransactionNone && (s.TransactionID == "" || s.TransactionTransition == "") {
		return fmt.Errorf("durable transaction state has incomplete transaction context")
	}
	if s.Runtime == model.RuntimeVerified && (s.RuntimeVersion == "" || s.RuntimeFingerprint == "" || s.RuntimeSource == "") {
		return fmt.Errorf("verified runtime requires version, source revision, and fingerprint")
	}
	if s.Configuration == model.ConfigurationVerified && s.ConfigFingerprint == "" {
		return fmt.Errorf("verified configuration requires a fingerprint")
	}
	if s.Configuration == model.ConfigurationVerified {
		if err := s.ConfigurationPolicy().Validate(); err != nil {
			return fmt.Errorf("verified configuration policy: %w", err)
		}
	}
	if (s.Plan == model.PlanApproved || s.Plan == model.PlanLocked) && (s.PlanFingerprint == "" || s.ApprovalFingerprint == "") {
		return fmt.Errorf("approved or locked plan requires plan and approval fingerprints")
	}
	switch s.Workspace {
	case model.WorkspaceCut, model.WorkspaceActive, model.WorkspacePublished, model.WorkspaceLanded, model.WorkspaceAttentionRequired, model.WorkspaceAbandoned:
		if s.WorkspacePath == "" || s.WorkspaceBranch == "" || s.WorkspaceSourcePath == "" || s.WorkspaceSourceID == "" || s.WorkspaceSourceRef == "" {
			return fmt.Errorf("managed workspace state requires destination and source identity")
		}
	}
	if s.Terminal == model.TerminalEstablished && s.Phase != model.PhaseTerminal && s.Phase != model.PhaseAbandoned {
		return fmt.Errorf("durable terminal evidence has a nonterminal phase")
	}
	seenGates := map[string]bool{}
	for _, gate := range s.Gates {
		if gate.Gate == "" || gate.Revision == "" || gate.Fingerprint == "" || seenGates[gate.Gate] {
			return fmt.Errorf("durable gate evidence must be complete and unique")
		}
		seenGates[gate.Gate] = true
	}
	return nil
}

func (s State) Canonical() State {
	result := s
	result.Gates = append([]GateEvidence(nil), s.Gates...)
	result.EnabledHosts = append([]string(nil), s.EnabledHosts...)
	sort.Strings(result.EnabledHosts)
	sort.Slice(result.Gates, func(i, j int) bool { return result.Gates[i].Gate < result.Gates[j].Gate })
	return result
}

func (s State) ConfigurationPolicy() model.ConfigurationPolicy {
	return model.ConfigurationPolicy{
		PlanApproval: s.PlanApprovalPolicy, IndependentReviewForHighRisk: s.IndependentReview,
		VisualEvidence: s.VisualEvidencePolicy, ExternalEffectAuthority: s.ExternalEffectPolicy,
		Hosts: append([]string(nil), s.EnabledHosts...),
	}.Canonical()
}

func (s State) ActiveObjective() (model.Objective, bool) {
	return s.Objective, s.Objective.ID != "" && s.Terminal == model.TerminalNonterminal
}

func EncodeState(state State) ([]byte, error) {
	state = state.Canonical()
	if err := state.Validate(); err != nil {
		return nil, err
	}
	value, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(value, '\n'), nil
}

func DecodeState(value []byte) (State, error) {
	var state State
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return State{}, fmt.Errorf("durable state contains trailing JSON")
	}
	if err := state.Validate(); err != nil {
		return State{}, err
	}
	return state.Canonical(), nil
}
