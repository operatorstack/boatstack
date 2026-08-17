package surfaces

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	boatstackruntime "github.com/operatorstack/boatstack/boatstack/internal/runtime"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/foregroundwork"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/supervisor"
	"github.com/operatorstack/boatstack/boatstack/invocation"
	general "github.com/operatorstack/boatstack/boatstack/kernel"
)

const SchemaVersion = 15

var flowContextIdentity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var gitObjectIdentity = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

type Operation string

const (
	OperationResolve           Operation = "resolve"
	OperationExplain           Operation = "explain"
	OperationApply             Operation = "apply"
	OperationRecover           Operation = "recover"
	OperationDoctor            Operation = "doctor"
	OperationCatalog           Operation = "catalog"
	OperationEvents            Operation = "events"
	OperationGuard             Operation = "guard"
	OperationWorkShow          Operation = "work-show"
	OperationWorkInputRequired Operation = "work-input-required"
	OperationWorkAnswer        Operation = "work-answer"
	OperationWorkComplete      Operation = "work-complete"
	OperationWorkBlock         Operation = "work-block"
)

func (o Operation) Valid() bool {
	switch o {
	case OperationResolve, OperationExplain, OperationApply, OperationRecover, OperationDoctor, OperationCatalog, OperationEvents, OperationGuard,
		OperationWorkShow, OperationWorkInputRequired, OperationWorkAnswer, OperationWorkComplete, OperationWorkBlock:
		return true
	default:
		return false
	}
}

type Request struct {
	SchemaVersion                int                                     `json:"schema_version"`
	Operation                    Operation                               `json:"operation"`
	Repository                   string                                  `json:"repository"`
	Host                         string                                  `json:"host"`
	CorrelationID                string                                  `json:"correlation_id"`
	ProgramID                    string                                  `json:"program_id,omitempty"`
	ProgramFingerprint           string                                  `json:"program_fingerprint,omitempty"`
	EntryID                      string                                  `json:"entry_id,omitempty"`
	FlowID                       string                                  `json:"flow_id,omitempty"`
	Objective                    model.Objective                         `json:"objective,omitempty"`
	TransitionID                 catalog.TransitionID                    `json:"transition_id,omitempty"`
	Prescription                 protocol.Prescription                   `json:"prescription,omitempty"`
	Authority                    protocol.AuthorityBundle                `json:"authority,omitempty"`
	RepositoryAuthority          bool                                    `json:"repository_authority,omitempty"`
	Parameters                   protocol.Parameters                     `json:"parameters,omitempty"`
	IdempotencyKey               string                                  `json:"idempotency_key,omitempty"`
	Command                      string                                  `json:"command,omitempty"`
	DelegationBindingFingerprint string                                  `json:"delegation_binding_fingerprint,omitempty"`
	DelegationRequestFingerprint string                                  `json:"delegation_request_fingerprint,omitempty"`
	DelegatedAuthorities         []catalog.AuthorityClass                `json:"delegated_authorities,omitempty"`
	WorkInputs                   map[string]protocol.WorkInputValue      `json:"work_inputs,omitempty"`
	WorkID                       string                                  `json:"work_id,omitempty"`
	WorkQuestionPrompt           string                                  `json:"work_question_prompt,omitempty"`
	WorkQuestionSchema           []byte                                  `json:"work_question_schema,omitempty"`
	WorkQuestionID               string                                  `json:"work_question_id,omitempty"`
	WorkAnswer                   []byte                                  `json:"work_answer,omitempty"`
	WorkBlockReason              string                                  `json:"work_block_reason,omitempty"`
	ControlBundle                *boatstackruntime.ControlBundleContract `json:"control_bundle,omitempty"`
	ControlBundleFingerprint     string                                  `json:"control_bundle_fingerprint,omitempty"`
	ControlBundleRevision        string                                  `json:"control_bundle_revision,omitempty"`
	InvocationEvidence           *invocation.Evidence                    `json:"invocation_evidence,omitempty"`
	InputRequest                 *invocation.InputRequest                `json:"input_request,omitempty"`
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
	if r.InputRequest != nil && r.InvocationEvidence != nil {
		return fmt.Errorf("surface request cannot carry both an input request and ready invocation evidence")
	}
	if r.InputRequest != nil {
		if r.Operation != OperationResolve || r.InputRequest.Validate() != nil || r.InputRequest.RunID != r.FlowID || r.InputRequest.ProgramFingerprint != r.ProgramFingerprint || r.InputRequest.EntryID != r.EntryID || r.InputRequest.TransitionID != string(r.TransitionID) {
			return fmt.Errorf("surface input request does not match the selected Flow transition")
		}
	}
	if r.InvocationEvidence != nil {
		if err := r.InvocationEvidence.Validate(); err != nil || r.InvocationEvidence.RunID != r.FlowID || r.InvocationEvidence.ProgramFingerprint != r.ProgramFingerprint || r.InvocationEvidence.EntryID != r.EntryID || r.InvocationEvidence.TransitionID != string(r.TransitionID) {
			return fmt.Errorf("surface invocation evidence does not match the selected Flow transition")
		}
	}
	if r.ControlBundle != nil {
		if err := r.ControlBundle.Validate(); err != nil {
			return err
		}
		if r.ControlBundleFingerprint != r.ControlBundle.Source.Fingerprint {
			return fmt.Errorf("CONTROL_BUNDLE_INVALID: request fingerprint does not match trusted source bundle")
		}
	} else if r.ControlBundleFingerprint != "" {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: request fingerprint has no trusted bundle")
	}
	if r.ControlBundleRevision != "" && (r.ControlBundle == nil || !gitObjectIdentity.MatchString(r.ControlBundleRevision)) {
		return fmt.Errorf("CONTROL_BUNDLE_INVALID: request revision has no trusted bundle or is not a Git object identity")
	}
	if len(r.DelegatedAuthorities) != 0 && (r.ProgramID == "" || len(r.DelegationBindingFingerprint) != 64 || len(r.DelegationRequestFingerprint) != 64) {
		return fmt.Errorf("surface delegated Flow request requires exact binding and request fingerprints")
	}
	for _, authority := range r.DelegatedAuthorities {
		if !authority.Valid() || authority == catalog.AuthorityNone {
			return fmt.Errorf("surface delegated Flow request has invalid authority %q", authority)
		}
	}
	for id, input := range r.WorkInputs {
		if !flowContextIdentity.MatchString(id) {
			return fmt.Errorf("surface foreground work input has invalid identity %q", id)
		}
		if err := input.Validate(); err != nil {
			return fmt.Errorf("surface foreground work input %q: %w", id, err)
		}
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
	if r.Operation == OperationExplain && (r.Prescription.ID != "" || r.IdempotencyKey != "") {
		return fmt.Errorf("explain request cannot carry a prescription or idempotency key")
	}
	if r.Operation == OperationGuard && (strings.TrimSpace(r.Command) == "" || len(r.Command) > 1<<20) {
		return fmt.Errorf("guard operation requires a bounded command")
	}
	if strings.HasPrefix(string(r.Operation), "work-") {
		if r.FlowID == "" || !flowContextIdentity.MatchString(r.WorkID) {
			return fmt.Errorf("foreground work operation requires semantic run and work identity")
		}
		switch r.Operation {
		case OperationWorkShow, OperationWorkComplete:
			if r.WorkQuestionPrompt != "" || len(r.WorkQuestionSchema) != 0 || r.WorkQuestionID != "" || len(r.WorkAnswer) != 0 || r.WorkBlockReason != "" {
				return fmt.Errorf("foreground work %s cannot carry mutation payload", r.Operation)
			}
		case OperationWorkInputRequired:
			if strings.TrimSpace(r.WorkQuestionPrompt) == "" || r.WorkQuestionID != "" || len(r.WorkAnswer) != 0 || r.WorkBlockReason != "" {
				return fmt.Errorf("foreground work input-required requires only a question prompt and optional schema")
			}
		case OperationWorkAnswer:
			if !flowContextIdentity.MatchString(r.WorkQuestionID) || len(r.WorkAnswer) == 0 || !json.Valid(r.WorkAnswer) || r.WorkQuestionPrompt != "" || len(r.WorkQuestionSchema) != 0 || r.WorkBlockReason != "" {
				return fmt.Errorf("foreground work answer requires the exact question and bounded JSON answer")
			}
		case OperationWorkBlock:
			if strings.TrimSpace(r.WorkBlockReason) == "" || r.WorkQuestionPrompt != "" || len(r.WorkQuestionSchema) != 0 || r.WorkQuestionID != "" || len(r.WorkAnswer) != 0 {
				return fmt.Errorf("foreground work block requires only a reason")
			}
		}
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
	PriorProgramFingerprint     string                      `json:"prior_program_fingerprint"`
	CandidateProgramFingerprint string                      `json:"candidate_program_fingerprint"`
	ProgramDeltaFingerprint     string                      `json:"program_delta_fingerprint"`
	RequiredTransition          catalog.TransitionID        `json:"required_transition"`
	AcceptanceFlag              string                      `json:"acceptance_flag"`
	HumanIdentity               *humanidentity.Presentation `json:"human_identity,omitempty"`
}

type Response struct {
	SchemaVersion  int                         `json:"schema_version"`
	Operation      Operation                   `json:"operation"`
	ProgramID      string                      `json:"program_id,omitempty"`
	EntryID        string                      `json:"entry_id,omitempty"`
	RunID          string                      `json:"run_id,omitempty"`
	Objective      model.Objective             `json:"objective,omitempty"`
	Snapshot       *model.Snapshot             `json:"snapshot,omitempty"`
	Decision       *supervisor.Decision        `json:"decision,omitempty"`
	Trace          *general.DecisionTrace      `json:"trace,omitempty"`
	Question       *Question                   `json:"question,omitempty"`
	Prescription   *protocol.Prescription      `json:"prescription,omitempty"`
	Admission      *protocol.Admission         `json:"admission,omitempty"`
	Receipt        *protocol.TransitionReceipt `json:"receipt,omitempty"`
	Replayed       bool                        `json:"replayed,omitempty"`
	Catalog        []catalog.Transition        `json:"catalog,omitempty"`
	Events         []map[string]any            `json:"events,omitempty"`
	Doctor         *DoctorReport               `json:"doctor,omitempty"`
	ProgramChange  *ProgramChange              `json:"program_change,omitempty"`
	Guard          *supervisor.GuardDecision   `json:"guard,omitempty"`
	Error          string                      `json:"error,omitempty"`
	Delegation     *DelegationRequired         `json:"delegation,omitempty"`
	CommitRequired *CommitRequired             `json:"commit_required,omitempty"`
	Work           *foregroundwork.Record      `json:"work,omitempty"`
	InputRequest   *invocation.InputRequest    `json:"input_request,omitempty"`
	Invocation     *invocation.Evidence        `json:"invocation_evidence,omitempty"`
}

type DelegationRequired struct {
	Code               string                     `json:"code"`
	RunID              string                     `json:"run_id"`
	RequestFingerprint string                     `json:"request_fingerprint"`
	Authorities        []catalog.AuthorityClass   `json:"authorities"`
	Description        string                     `json:"description"`
	HumanIdentity      humanidentity.Presentation `json:"human_identity"`
}

// CommitRequired is a typed suspension at a repository revision boundary.
// Boatstack preserves the run and installed bytes but does not mint Git commit
// authority; the caller must commit the exact control bundle and resume.
type CommitRequired struct {
	Code                     string `json:"code"`
	RunID                    string `json:"run_id"`
	Revision                 string `json:"revision"`
	ControlBundleFingerprint string `json:"control_bundle_fingerprint"`
	Description              string `json:"description"`
}

// Question is a typed suspension, not a background task. Supplying its
// required evidence and resolving again with the same run identity resumes the
// existing command context.
type Question struct {
	ID            string                      `json:"id"`
	RunID         string                      `json:"run_id"`
	TransitionID  catalog.TransitionID        `json:"transition_id"`
	Prompt        string                      `json:"prompt,omitempty"`
	Parameters    []catalog.ParameterSpec     `json:"parameters,omitempty"`
	Authority     []catalog.AuthorityClass    `json:"authority,omitempty"`
	AuthorityAll  []catalog.AuthorityClass    `json:"authority_all,omitempty"`
	HumanIdentity *humanidentity.Presentation `json:"human_identity,omitempty"`
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
