package ports

import (
	"context"
	"time"

	"github.com/operatorstack/boatstack/boatstack/internal/kernel/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/model"
	"github.com/operatorstack/boatstack/boatstack/internal/kernel/protocol"
)

type Observer interface {
	Observe(context.Context, ObservationRequest) (model.Observation, error)
}

type ObservationRequest struct {
	Invocation         model.InvocationContext
	IgnoreAdmissionID  string
	VerifyTransitionID catalog.TransitionID
}

type ControllerLayout struct {
	RepositoryRoot    string
	GitCommonRoot     string
	StateRoot         string
	SharedRoot        string
	FlowRoot          string
	EmbeddedStateRoot string
	ExternalStateRoot string
	StatePath         string
	BindingPath       string
	JournalRoot       string
	ReceiptPath       string
	EventPath         string
	LockRoot          string
	ConfigPath        string
	ConfigAuthority   string
	EvidenceRoot      string
}

type InvocationResolver interface {
	ResolveInvocation(context.Context, string, string, string) (model.InvocationContext, error)
	ResolveLayout(context.Context, model.InvocationContext) (ControllerLayout, model.InvocationContext, error)
}

type Clock interface{ Now() time.Time }

type Lock interface{ Release() error }

type Locker interface {
	Acquire(context.Context, model.InvocationContext, []string) (Lock, error)
}

type Journal interface {
	Begin(context.Context, protocol.Admission, catalog.Transition) error
	Stage(context.Context, string, []ResourceMutation) error
	Mark(context.Context, string, string) error
	Commit(context.Context, protocol.TransitionReceipt) error
	Abort(context.Context, string, string) error
	RequireRecovery(context.Context, string, string) error
}

type EffectSettlement string

const (
	EffectSettled EffectSettlement = "settled"
	EffectUnknown EffectSettlement = "unknown"
)

type EffectResult struct {
	Settlement EffectSettlement
	Detail     string
}

type ResourceMutation struct {
	Resource    string `json:"resource"`
	Owner       string `json:"owner"`
	Path        string `json:"path"`
	Prior       []byte `json:"prior,omitempty"`
	Target      []byte `json:"target,omitempty"`
	PriorExists bool   `json:"prior_exists"`
	Mode        uint32 `json:"mode"`
	InstallLast bool   `json:"install_last,omitempty"`
	Delete      bool   `json:"delete,omitempty"`
}

type PreparedEffect interface {
	Manifest() []ResourceMutation
	VerificationInvocation() (model.InvocationContext, bool)
	Execute(context.Context) (EffectResult, error)
	Rollback(context.Context) error
}

type EffectDriver interface {
	Prepare(context.Context, protocol.Admission, catalog.Transition) (PreparedEffect, error)
}

type ReceiptStore interface {
	Bind(context.Context, string, protocol.Admission) error
	Unbind(string)
	NextSequence(context.Context, string) (uint64, error)
	FindByIdempotency(context.Context, model.InvocationContext, string) (protocol.TransitionReceipt, bool, error)
	Append(context.Context, protocol.TransitionReceipt) error
}
