// Package invocation materializes canonical Control Program transition
// parameters without knowing any domain vocabulary or effect mechanism.
package invocation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/controlprogram"
)

const (
	EvidenceSchema         = "transition-invocation"
	EvidenceSchemaRevision = 2
	RequestSchema          = "transition-input-request"
	RequestSchemaRevision  = 2
	ReceiptSchema          = "transition-input-receipt"
	ReceiptSchemaRevision  = 2
)

type Context struct {
	RunID                       string
	ProgramFingerprint          string
	ExecutionProgramFingerprint string
	EntryID                     string
	TargetID                    string
	TransitionID                string
	StateRevision               uint64
	ContextFingerprint          string
	ControlBundleFingerprint    string
	ExecutionScopeFingerprint   string
	InputRequestGeneration      uint64
	InputRequestSupersession    *InputRequestSupersession
	EntryInputs                 map[string]Value
	State                       map[string]Value
	Receipts                    map[string]Value
	WorkOutputs                 map[string]Value
	InputReceipts               map[string]InputReceipt
}

type Value struct {
	Type                controlprogram.ValueTypeDefinition
	Canonical           string
	SecretReference     string
	Provenance          string
	ProducerFingerprint string
	AuthorityReceipts   []string
}

type Resolver interface {
	ResolveParameter(controlprogram.ParameterResolverBinding, Context) (Value, error)
}

type ResolvedParameter struct {
	Name                string                             `json:"name"`
	Type                controlprogram.ValueTypeDefinition `json:"type"`
	Value               string                             `json:"value,omitempty"`
	SecretReference     string                             `json:"secret_reference,omitempty"`
	ValueFingerprint    string                             `json:"value_fingerprint"`
	ProducerKind        controlprogram.ParameterSourceKind `json:"producer_kind"`
	ProducerFingerprint string                             `json:"producer_fingerprint"`
	AuthorityReceipts   []string                           `json:"authority_receipts"`
}

type Evidence struct {
	Schema                      string              `json:"schema"`
	SchemaRevision              int                 `json:"schema_revision"`
	RunID                       string              `json:"run_id"`
	ProgramFingerprint          string              `json:"program_fingerprint"`
	ExecutionProgramFingerprint string              `json:"execution_program_fingerprint"`
	EntryID                     string              `json:"entry_id"`
	TargetID                    string              `json:"target_id"`
	TransitionID                string              `json:"transition_id"`
	StateRevision               uint64              `json:"state_revision"`
	ContextFingerprint          string              `json:"context_fingerprint"`
	ControlBundleFingerprint    string              `json:"control_bundle_fingerprint,omitempty"`
	Parameters                  []ResolvedParameter `json:"parameters"`
	InvocationFingerprint       string              `json:"invocation_fingerprint"`
}

type RequestedParameter struct {
	ID          string                              `json:"id"`
	Type        controlprogram.ValueTypeDefinition  `json:"type"`
	Description string                              `json:"description"`
	Secret      bool                                `json:"secret"`
	Authority   controlprogram.AuthorityRequirement `json:"authority"`
}

type InputRequest struct {
	Schema                      string                    `json:"schema"`
	SchemaRevision              int                       `json:"schema_revision"`
	ID                          string                    `json:"id"`
	Code                        string                    `json:"code"`
	RunID                       string                    `json:"run_id"`
	ProgramFingerprint          string                    `json:"program_fingerprint"`
	ExecutionProgramFingerprint string                    `json:"execution_program_fingerprint"`
	EntryID                     string                    `json:"entry_id"`
	TargetID                    string                    `json:"target_id"`
	TransitionID                string                    `json:"transition_id"`
	Fingerprint                 string                    `json:"fingerprint"`
	StateRevision               uint64                    `json:"state_revision"`
	ContextFingerprint          string                    `json:"context_fingerprint"`
	ControlBundleFingerprint    string                    `json:"control_bundle_fingerprint,omitempty"`
	ExecutionScopeFingerprint   string                    `json:"execution_scope_fingerprint"`
	Generation                  uint64                    `json:"generation,omitempty"`
	Supersession                *InputRequestSupersession `json:"supersession,omitempty"`
	Parameters                  []RequestedParameter      `json:"parameters"`
}

// InputRequestSupersession binds a new immutable request generation to the
// rejected answer generation it replaces. The prior request and its receipts
// remain unchanged.
type InputRequestSupersession struct {
	PreviousRequestFingerprint string    `json:"previous_request_fingerprint"`
	Reason                     string    `json:"reason"`
	Actor                      string    `json:"actor"`
	Host                       string    `json:"host"`
	CreatedAt                  time.Time `json:"created_at"`
}

type InputReceipt struct {
	Schema                      string                             `json:"schema"`
	SchemaRevision              int                                `json:"schema_revision"`
	ID                          string                             `json:"id"`
	RunID                       string                             `json:"run_id"`
	ProgramFingerprint          string                             `json:"program_fingerprint"`
	ExecutionProgramFingerprint string                             `json:"execution_program_fingerprint"`
	EntryID                     string                             `json:"entry_id"`
	TargetID                    string                             `json:"target_id"`
	TransitionID                string                             `json:"transition_id"`
	ParameterID                 string                             `json:"parameter_id"`
	Type                        controlprogram.ValueTypeDefinition `json:"type"`
	Value                       string                             `json:"value,omitempty"`
	SecretReference             string                             `json:"secret_reference,omitempty"`
	ValueFingerprint            string                             `json:"value_fingerprint"`
	ProducerFingerprint         string                             `json:"producer_fingerprint"`
	RequestFingerprint          string                             `json:"request_fingerprint"`
	StateRevision               uint64                             `json:"state_revision"`
	ContextFingerprint          string                             `json:"context_fingerprint"`
	ControlBundleFingerprint    string                             `json:"control_bundle_fingerprint,omitempty"`
	ExecutionScopeFingerprint   string                             `json:"execution_scope_fingerprint"`
	Actor                       string                             `json:"actor"`
	Host                        string                             `json:"host"`
	AuthorityReceipts           []string                           `json:"authority_receipts"`
	CreatedAt                   time.Time                          `json:"created_at"`
	ExpiresAt                   time.Time                          `json:"expires_at,omitempty"`
	Scope                       string                             `json:"scope"`
	Fingerprint                 string                             `json:"fingerprint"`
}

type Result struct {
	Ready   *Evidence
	Request *InputRequest
	Blocker *Blocker
}

type Blocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (r InputRequest) Validate() error {
	if r.Schema != RequestSchema || r.SchemaRevision != RequestSchemaRevision || r.ID == "" || r.Code != "TRANSITION_INPUT_REQUIRED" || r.RunID == "" || len(r.ProgramFingerprint) != 64 || len(r.ExecutionProgramFingerprint) != 64 || r.EntryID == "" || r.TargetID == "" || r.TransitionID == "" || len(r.ContextFingerprint) != 64 || len(r.ExecutionScopeFingerprint) != 64 || r.Fingerprint == "" || len(r.Parameters) == 0 {
		return fmt.Errorf("input request envelope is invalid")
	}
	generation := r.EffectiveGeneration()
	if generation == 1 && r.Supersession != nil {
		return fmt.Errorf("first input request generation cannot supersede another request")
	}
	if generation > 1 {
		if r.Supersession == nil || len(r.Supersession.PreviousRequestFingerprint) != 64 || strings.TrimSpace(r.Supersession.Reason) == "" || strings.TrimSpace(r.Supersession.Actor) == "" || strings.TrimSpace(r.Supersession.Host) == "" || r.Supersession.CreatedAt.IsZero() {
			return fmt.Errorf("superseding input request has invalid lineage")
		}
	}
	identity := r
	identity.Fingerprint = ""
	if fingerprint(identity) != r.Fingerprint {
		return fmt.Errorf("input request failed content identity verification")
	}
	return nil
}

// EffectiveGeneration treats legacy requests without an explicit generation
// as the first immutable generation.
func (r InputRequest) EffectiveGeneration() uint64 {
	if r.Generation == 0 {
		return 1
	}
	return r.Generation
}

func (e Evidence) Validate() error {
	if e.Schema != EvidenceSchema || e.SchemaRevision != EvidenceSchemaRevision || e.RunID == "" || len(e.ProgramFingerprint) != 64 || len(e.ExecutionProgramFingerprint) != 64 || e.EntryID == "" || e.TargetID == "" || e.TransitionID == "" || len(e.ContextFingerprint) != 64 || len(e.InvocationFingerprint) != 64 {
		return fmt.Errorf("invocation evidence envelope is invalid")
	}
	identity := e
	identity.InvocationFingerprint = ""
	if fingerprint(identity) != e.InvocationFingerprint {
		return fmt.Errorf("invocation evidence failed content identity verification")
	}
	return nil
}

func Materialize(contracts []controlprogram.OperatorParameter, bindings []controlprogram.TransitionParameterBinding, context Context, resolver Resolver) (Result, error) {
	if context.RunID == "" || len(context.ProgramFingerprint) != 64 || len(context.ExecutionProgramFingerprint) != 64 || context.EntryID == "" || context.TargetID == "" || context.TransitionID == "" || len(context.ContextFingerprint) != 64 || len(context.ExecutionScopeFingerprint) != 64 {
		return Result{}, fmt.Errorf("invocation materializer requires exact run, program, entry, target, transition, and context identity")
	}
	byBinding := map[string]controlprogram.ParameterProducer{}
	for _, binding := range bindings {
		byBinding[binding.Parameter] = binding.Producer
	}
	hostRequest := inputRequestForHostBindings(contracts, byBinding, context)
	var parameters []ResolvedParameter
	var requested []RequestedParameter
	for _, contract := range contracts {
		producer, bound := byBinding[contract.ID]
		if !bound {
			if contract.Required {
				return Result{}, fmt.Errorf("required parameter %q has no compiled producer", contract.ID)
			}
			continue
		}
		requestFingerprint := ""
		if hostRequest != nil {
			requestFingerprint = hostRequest.Fingerprint
		}
		value, available, err := materializeOne(contract, producer, context, resolver, requestFingerprint)
		if err != nil {
			return Result{Blocker: &Blocker{Code: "TRANSITION_INPUT_BLOCKED", Detail: err.Error()}}, nil
		}
		if !available {
			if producer.Kind != controlprogram.ParameterSourceHostInput || producer.Request == nil {
				return Result{Blocker: &Blocker{Code: "TRANSITION_INPUT_UNAVAILABLE", Detail: fmt.Sprintf("parameter %q producer is not currently available", contract.ID)}}, nil
			}
			requested = append(requested, RequestedParameter{ID: contract.ID, Type: contract.Type, Description: producer.Request.Description, Secret: contract.Secret, Authority: controlprogram.AuthorityRequirement{AnyOf: append([]string(nil), producer.Request.Authorities...)}})
			continue
		}
		if err := validateValue(contract.Type, value.Canonical, value.SecretReference, contract.Secret); err != nil {
			return Result{Blocker: &Blocker{Code: "TRANSITION_INPUT_INVALID", Detail: fmt.Sprintf("parameter %q: %v", contract.ID, err)}}, nil
		}
		valueFingerprint := digest(value.Canonical + "\x00" + value.SecretReference)
		producerFingerprint := value.ProducerFingerprint
		if producerFingerprint == "" {
			producerFingerprint = fingerprintProducer(producer)
		}
		parameter := ResolvedParameter{Name: contract.ID, Type: contract.Type, ValueFingerprint: valueFingerprint, ProducerKind: producer.Kind, ProducerFingerprint: producerFingerprint, AuthorityReceipts: append([]string(nil), value.AuthorityReceipts...)}
		if contract.Secret {
			parameter.SecretReference = value.SecretReference
		} else {
			parameter.Value = value.Canonical
		}
		parameters = append(parameters, parameter)
	}
	if len(requested) != 0 {
		return Result{Request: hostRequest}, nil
	}
	sort.Slice(parameters, func(i, j int) bool { return parameters[i].Name < parameters[j].Name })
	evidence := Evidence{Schema: EvidenceSchema, SchemaRevision: EvidenceSchemaRevision, RunID: context.RunID, ProgramFingerprint: context.ProgramFingerprint, ExecutionProgramFingerprint: context.ExecutionProgramFingerprint, EntryID: context.EntryID, TargetID: context.TargetID, TransitionID: context.TransitionID, StateRevision: context.StateRevision, ContextFingerprint: context.ContextFingerprint, ControlBundleFingerprint: context.ControlBundleFingerprint, Parameters: parameters}
	evidence.InvocationFingerprint = fingerprintWithoutField(evidence, "InvocationFingerprint")
	return Result{Ready: &evidence}, nil
}

func materializeOne(contract controlprogram.OperatorParameter, producer controlprogram.ParameterProducer, context Context, resolver Resolver, requestFingerprint string) (Value, bool, error) {
	switch producer.Kind {
	case controlprogram.ParameterSourceEntryInput:
		value, ok := context.EntryInputs[producer.Input]
		return value, ok, nil
	case controlprogram.ParameterSourceState:
		value, ok := context.State[producer.Facet]
		return value, ok, nil
	case controlprogram.ParameterSourceReceipt:
		value, ok := context.Receipts[producer.Transition+"/"+producer.Field]
		return value, ok, nil
	case controlprogram.ParameterSourceWorkOutput:
		value, ok := context.WorkOutputs[producer.Work+"/"+producer.Output]
		return value, ok, nil
	case controlprogram.ParameterSourceTrustedResolver:
		if resolver == nil || producer.Binding == nil {
			return Value{}, false, fmt.Errorf("trusted resolver is unavailable")
		}
		value, err := resolver.ResolveParameter(*producer.Binding, context)
		return value, err == nil, err
	case controlprogram.ParameterSourceHostInput:
		receipt, ok := context.InputReceipts[contract.ID+"@"+requestFingerprint]
		if !ok {
			return Value{}, false, nil
		}
		if err := receipt.ValidateCurrent(context, contract, producer, requestFingerprint); err != nil {
			return Value{}, false, err
		}
		return Value{Type: receipt.Type, Canonical: receipt.Value, SecretReference: receipt.SecretReference, ProducerFingerprint: receipt.ProducerFingerprint, AuthorityReceipts: append([]string(nil), receipt.AuthorityReceipts...)}, true, nil
	default:
		return Value{}, false, fmt.Errorf("unknown producer kind %q", producer.Kind)
	}
}

func (r InputReceipt) ValidateCurrent(context Context, contract controlprogram.OperatorParameter, producer controlprogram.ParameterProducer, requestFingerprint string) error {
	if r.Schema != ReceiptSchema || r.SchemaRevision != ReceiptSchemaRevision || r.ID == "" || r.Fingerprint == "" || r.Scope != "transition" || len(r.ExecutionProgramFingerprint) != 64 || len(r.ExecutionScopeFingerprint) != 64 || producer.Request == nil {
		return fmt.Errorf("input receipt envelope is invalid")
	}
	identity := r
	identity.Fingerprint = ""
	if fingerprint(identity) != r.Fingerprint {
		return fmt.Errorf("input receipt failed content identity verification")
	}
	identities := []struct {
		field   string
		differs bool
	}{
		{"run", r.RunID != context.RunID}, {"program", r.ProgramFingerprint != context.ProgramFingerprint},
		{"execution-program", r.ExecutionProgramFingerprint != context.ExecutionProgramFingerprint},
		{"entry", r.EntryID != context.EntryID}, {"target", r.TargetID != context.TargetID},
		{"transition", r.TransitionID != context.TransitionID}, {"parameter", r.ParameterID != contract.ID},
		{"state", r.StateRevision != context.StateRevision}, {"context", r.ContextFingerprint != context.ContextFingerprint},
		{"control-bundle", r.ControlBundleFingerprint != context.ControlBundleFingerprint},
		{"execution-scope", r.ExecutionScopeFingerprint != context.ExecutionScopeFingerprint},
		{"request", r.RequestFingerprint != requestFingerprint},
	}
	for _, identity := range identities {
		if identity.differs {
			return fmt.Errorf("input receipt is stale because its %s identity changed", identity.field)
		}
	}
	if !r.ExpiresAt.IsZero() && !time.Now().UTC().Before(r.ExpiresAt) {
		return fmt.Errorf("input receipt is expired")
	}
	if !sameType(r.Type, contract.Type) || r.ValueFingerprint != digest(r.Value+"\x00"+r.SecretReference) || r.ProducerFingerprint != fingerprintProducer(producer) {
		return fmt.Errorf("input receipt type, value, or producer binding changed")
	}
	return validateValue(contract.Type, r.Value, r.SecretReference, contract.Secret)
}

func inputRequestForHostBindings(contracts []controlprogram.OperatorParameter, producers map[string]controlprogram.ParameterProducer, context Context) *InputRequest {
	var requested []RequestedParameter
	for _, contract := range contracts {
		producer, ok := producers[contract.ID]
		if !ok || producer.Kind != controlprogram.ParameterSourceHostInput || producer.Request == nil {
			continue
		}
		requested = append(requested, RequestedParameter{
			ID: contract.ID, Type: contract.Type, Description: producer.Request.Description, Secret: contract.Secret,
			Authority: controlprogram.AuthorityRequirement{AnyOf: append([]string(nil), producer.Request.Authorities...)},
		})
	}
	if len(requested) == 0 {
		return nil
	}
	sort.Slice(requested, func(i, j int) bool { return requested[i].ID < requested[j].ID })
	request := InputRequest{Schema: RequestSchema, SchemaRevision: RequestSchemaRevision, Code: "TRANSITION_INPUT_REQUIRED", RunID: context.RunID, ProgramFingerprint: context.ProgramFingerprint, ExecutionProgramFingerprint: context.ExecutionProgramFingerprint, EntryID: context.EntryID, TargetID: context.TargetID, TransitionID: context.TransitionID, StateRevision: context.StateRevision, ContextFingerprint: context.ContextFingerprint, ControlBundleFingerprint: context.ControlBundleFingerprint, ExecutionScopeFingerprint: context.ExecutionScopeFingerprint, Generation: context.InputRequestGeneration, Supersession: context.InputRequestSupersession, Parameters: requested}
	request.ID = "input-" + fingerprintWithoutField(request, "Fingerprint")[:24]
	request.Fingerprint = fingerprintWithoutField(request, "Fingerprint")
	return &request
}

// SupersedeRequest creates the next immutable request generation after a
// semantic rejection. It never modifies the prior request or any answer
// receipt bound to it.
func SupersedeRequest(prior InputRequest, reason, actor, host string, now time.Time) (InputRequest, error) {
	if err := prior.Validate(); err != nil {
		return InputRequest{}, err
	}
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(actor) == "" || strings.TrimSpace(host) == "" || now.IsZero() {
		return InputRequest{}, fmt.Errorf("input request supersession requires reason, actor, host, and time")
	}
	next := prior
	next.ID, next.Fingerprint = "", ""
	next.Generation = prior.EffectiveGeneration() + 1
	next.Supersession = &InputRequestSupersession{
		PreviousRequestFingerprint: prior.Fingerprint,
		Reason:                     strings.TrimSpace(reason), Actor: strings.TrimSpace(actor), Host: strings.TrimSpace(host), CreatedAt: now.UTC(),
	}
	next.ID = "input-" + fingerprintWithoutField(next, "Fingerprint")[:24]
	next.Fingerprint = fingerprintWithoutField(next, "Fingerprint")
	if err := next.Validate(); err != nil {
		return InputRequest{}, err
	}
	return next, nil
}

func SealReceipt(receipt InputReceipt) (InputReceipt, error) {
	receipt.Schema, receipt.SchemaRevision = ReceiptSchema, ReceiptSchemaRevision
	if receipt.ID == "" {
		receipt.ID = "input-" + digest(strings.Join([]string{receipt.RunID, receipt.TransitionID, receipt.ParameterID, receipt.RequestFingerprint}, "\x00"))[:24]
	}
	if receipt.CreatedAt.IsZero() {
		receipt.CreatedAt = time.Now().UTC()
	}
	receipt.ValueFingerprint = digest(receipt.Value + "\x00" + receipt.SecretReference)
	identity := receipt
	identity.Fingerprint = ""
	receipt.Fingerprint = fingerprint(identity)
	return receipt, nil
}

// ProducerFingerprint returns the stable identity used to bind invocation
// receipts to one exact compiled producer declaration.
func ProducerFingerprint(producer controlprogram.ParameterProducer) string {
	return fingerprintProducer(producer)
}

// BindControlBundle seals already materialized parameter evidence to the exact
// active control bundle derived from those values. Producers are
// rematerialized before this step on every resolve and apply attempt.
func BindControlBundle(evidence Evidence, bundleFingerprint string) (Evidence, error) {
	if len(bundleFingerprint) != 64 {
		return Evidence{}, fmt.Errorf("invocation evidence requires an exact control-bundle fingerprint")
	}
	evidence.ControlBundleFingerprint = bundleFingerprint
	evidence.InvocationFingerprint = ""
	evidence.InvocationFingerprint = fingerprint(evidence)
	if err := evidence.Validate(); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

// ValidateAnswer checks one canonical host answer against its trusted
// parameter contract before a runtime receipt can be recorded.
func ValidateAnswer(contract controlprogram.OperatorParameter, value, secretReference string) error {
	return validateValue(contract.Type, value, secretReference, contract.Secret)
}

func validateValue(valueType controlprogram.ValueTypeDefinition, value, secretReference string, secret bool) error {
	if secret {
		if value != "" || secretReference == "" {
			return fmt.Errorf("secret values require only an opaque runtime reference")
		}
		return nil
	}
	if secretReference != "" || value == "" {
		return fmt.Errorf("non-secret values require canonical plaintext")
	}
	switch valueType.Kind {
	case "string":
		return nil
	case "boolean":
		if value != "true" && value != "false" {
			return fmt.Errorf("expected boolean")
		}
	case "integer":
		integer, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("expected integer")
		}
		if valueType.Minimum != nil && integer < *valueType.Minimum {
			return fmt.Errorf("integer is below minimum")
		}
		if valueType.Maximum != nil && integer > *valueType.Maximum {
			return fmt.Errorf("integer exceeds maximum")
		}
	case "json":
		if !json.Valid([]byte(value)) {
			return fmt.Errorf("expected canonical JSON")
		}
	default:
		return fmt.Errorf("unknown value type %q", valueType.Kind)
	}
	return nil
}

func sameType(left, right controlprogram.ValueTypeDefinition) bool {
	leftRaw, _ := json.Marshal(left)
	rightRaw, _ := json.Marshal(right)
	return string(leftRaw) == string(rightRaw)
}
func fingerprintProducer(value controlprogram.ParameterProducer) string { return fingerprint(value) }
func fingerprint(value any) string                                      { raw, _ := json.Marshal(value); return digest(string(raw)) }
func fingerprintWithoutField(value any, _ string) string                { return fingerprint(value) }
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
