package surfaces

import (
	"fmt"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/supervisor"
)

const SchemaVersion = 2

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
	FlowID              string                   `json:"flow_id,omitempty"`
	Goal                model.Goal               `json:"goal,omitempty"`
	TransitionID        catalog.TransitionID     `json:"transition_id,omitempty"`
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
	PrimaryFlowID            string   `json:"primary_flow_id"`
	PrimaryFlowVersion       string   `json:"primary_flow_version"`
	PrimaryFlowFingerprint   string   `json:"primary_flow_fingerprint"`
	CoreTransitionCount      int      `json:"core_transition_count"`
	FlowTransitionCount      int      `json:"flow_transition_count"`
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
	Goal          model.Goal                  `json:"goal,omitempty"`
	Snapshot      *model.Snapshot             `json:"snapshot,omitempty"`
	Decision      *supervisor.Decision        `json:"decision,omitempty"`
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
