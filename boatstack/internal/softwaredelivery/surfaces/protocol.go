package surfaces

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
)

const SchemaVersion = 6

var flowContextIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Operation string

const (
	OperationResolve Operation = "resolve"
	OperationApply   Operation = "apply"
	OperationRecover Operation = "recover"
	OperationDoctor  Operation = "doctor"
	OperationCatalog Operation = "catalog"
	OperationEvents  Operation = "events"
	OperationGuard   Operation = "guard"
)

func (o Operation) Valid() bool {
	switch o {
	case OperationResolve, OperationApply, OperationRecover, OperationDoctor, OperationCatalog, OperationEvents, OperationGuard:
		return true
	default:
		return false
	}
}

type Request struct {
	SchemaVersion       int                      `json:"schema_version"`
	Operation           Operation                `json:"operation"`
	Repository          string                   `json:"repository"`
	Host                string                   `json:"host"`
	CorrelationID       string                   `json:"correlation_id"`
	ProgramID           string                   `json:"program_id,omitempty"`
	ProgramFingerprint  string                   `json:"program_fingerprint,omitempty"`
	EntryID             string                   `json:"entry_id,omitempty"`
	FlowID              string                   `json:"flow_id,omitempty"`
	Objective           model.Objective          `json:"objective,omitempty"`
	TransitionID        catalog.TransitionID     `json:"transition_id,omitempty"`
	Prescription        protocol.Prescription    `json:"prescription,omitempty"`
	Authority           protocol.AuthorityBundle `json:"authority,omitempty"`
	RepositoryAuthority bool                     `json:"repository_authority,omitempty"`
	Parameters          protocol.Parameters      `json:"parameters,omitempty"`
	IdempotencyKey      string                   `json:"idempotency_key,omitempty"`
	Command             string                   `json:"command,omitempty"`
}

func (r Request) Validate(now time.Time) error {
	if r.SchemaVersion != SchemaVersion || !r.Operation.Valid() {
		return fmt.Errorf("surface request has invalid schema or operation")
	}
	if r.Operation != OperationCatalog && (r.Repository == "" || r.Host == "" || r.CorrelationID == "") {
		return fmt.Errorf("surface request requires repository, host, and correlation identity")
	}
	if (r.ProgramID == "") != (r.EntryID == "") {
		return fmt.Errorf("surface request requires both program and entry identity")
	}
	if r.ProgramID != "" && (!flowContextIdentity.MatchString(r.ProgramID) || len(r.ProgramFingerprint) != 64 || !flowContextIdentity.MatchString(r.EntryID) || r.FlowID == "") {
		return fmt.Errorf("surface Flow entry requires semantic program, entry, and run identity")
	}
	if r.ProgramID == "" && r.ProgramFingerprint != "" {
		return fmt.Errorf("surface request cannot carry a program fingerprint without a program")
	}
	if r.Operation != OperationCatalog {
		knownHost := false
		for _, host := range CanonicalHostNames() {
			if r.Host == host {
				knownHost = true
				break
			}
		}
		if !knownHost {
			return fmt.Errorf("surface request has unsupported host %q", r.Host)
		}
	}
	if r.Operation == OperationApply || r.Operation == OperationRecover {
		if r.FlowID == "" || r.TransitionID == "" {
			return fmt.Errorf("apply/recover request requires flow and transition identity")
		}
		if err := r.Prescription.Validate(); err != nil {
			return fmt.Errorf("apply/recover request requires an exact resolution prescription: %w", err)
		}
		if r.Prescription.TransitionID != r.TransitionID {
			return fmt.Errorf("apply/recover transition does not match prescription")
		}
	}
	if r.Operation == OperationGuard && (strings.TrimSpace(r.Command) == "" || len(r.Command) > 1<<20) {
		return fmt.Errorf("guard operation requires a bounded command")
	}
	if r.IdempotencyKey != "" && !strings.HasPrefix(r.IdempotencyKey, "idem-") {
		return fmt.Errorf("surface idempotency key has invalid identity")
	}
	if err := r.Authority.Validate(now); err != nil {
		return err
	}
	return nil
}

type DoctorReport struct {
	Healthy                  bool     `json:"healthy"`
	KernelVersion            string   `json:"kernel_version"`
	CoreSystemID             string   `json:"core_system_id"`
	CoreSystemVersion        string   `json:"core_system_version"`
	ProgramID                string   `json:"program_id"`
	ProgramVersion           string   `json:"program_version"`
	CoreTransitionCount      int      `json:"core_transition_count"`
	RuntimeTransitionCount   int      `json:"runtime_transition_count"`
	ExtensionTransitionCount int      `json:"extension_transition_count"`
	TransitionCount          int      `json:"transition_count"`
	EnabledExtensions        []string `json:"enabled_extensions,omitempty"`
	ProgramFingerprint       string   `json:"program_fingerprint"`
	UnresolvedProgramDrift   bool     `json:"unresolved_program_drift"`
	RuntimeHealthy           bool     `json:"runtime_healthy"`
	UpdateReady              bool     `json:"update_ready"`
	RecoveryRequired         bool     `json:"recovery_required"`
	Snapshot                 string   `json:"snapshot,omitempty"`
	Detail                   string   `json:"detail"`
}

type ProgramChange struct {
	PriorProgramFingerprint     string               `json:"prior_program_fingerprint"`
	CandidateProgramFingerprint string               `json:"candidate_program_fingerprint"`
	ProgramDeltaFingerprint     string               `json:"program_delta_fingerprint"`
	RequiredTransition          catalog.TransitionID `json:"required_transition"`
	AcceptanceFlag              string               `json:"acceptance_flag"`
}

type Response struct {
	SchemaVersion int                         `json:"schema_version"`
	Operation     Operation                   `json:"operation"`
	ProgramID     string                      `json:"program_id,omitempty"`
	EntryID       string                      `json:"entry_id,omitempty"`
	RunID         string                      `json:"run_id,omitempty"`
	Objective     model.Objective             `json:"objective,omitempty"`
	Snapshot      *model.Snapshot             `json:"snapshot,omitempty"`
	Decision      *supervisor.Decision        `json:"decision,omitempty"`
	Question      *Question                   `json:"question,omitempty"`
	Prescription  *protocol.Prescription      `json:"prescription,omitempty"`
	Admission     *protocol.Admission         `json:"admission,omitempty"`
	Receipt       *protocol.TransitionReceipt `json:"receipt,omitempty"`
	Replayed      bool                        `json:"replayed,omitempty"`
	Catalog       []catalog.Transition        `json:"catalog,omitempty"`
	Events        []map[string]any            `json:"events,omitempty"`
	Doctor        *DoctorReport               `json:"doctor,omitempty"`
	ProgramChange *ProgramChange              `json:"program_change,omitempty"`
	Guard         *supervisor.GuardDecision   `json:"guard,omitempty"`
	Error         string                      `json:"error,omitempty"`
}

// Question is a typed suspension, not a background task. Supplying its
// required evidence and resolving again with the same run identity resumes the
// existing command context.
type Question struct {
	ID           string                   `json:"id"`
	RunID        string                   `json:"run_id"`
	TransitionID catalog.TransitionID     `json:"transition_id"`
	Prompt       string                   `json:"prompt,omitempty"`
	Parameters   []catalog.ParameterSpec  `json:"parameters,omitempty"`
	Authority    []catalog.AuthorityClass `json:"authority,omitempty"`
	AuthorityAll []catalog.AuthorityClass `json:"authority_all,omitempty"`
}

func QuestionFor(runID, snapshotFingerprint string, decision supervisor.Decision) *Question {
	if runID == "" || decision.Kind != supervisor.DecisionCandidate || decision.Transition == nil {
		return nil
	}
	transition := decision.Transition
	if transition.Prescription.AuthorityPrompt == "" && len(transition.Parameters) == 0 && len(transition.Authority) == 0 && len(transition.AuthorityAll) == 0 {
		return nil
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{runID, snapshotFingerprint, string(transition.ID)}, "\x00")))
	return &Question{
		ID: "question-" + hex.EncodeToString(digest[:12]), RunID: runID, TransitionID: transition.ID,
		Prompt: transition.Prescription.AuthorityPrompt, Parameters: append([]catalog.ParameterSpec(nil), transition.Parameters...),
		Authority: append([]catalog.AuthorityClass(nil), transition.Authority...), AuthorityAll: append([]catalog.AuthorityClass(nil), transition.AuthorityAll...),
	}
}
