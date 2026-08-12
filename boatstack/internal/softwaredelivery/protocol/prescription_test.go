package protocol

import (
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/catalog"
	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/model"
)

func TestPrescriptionContentIdentityBindsTransitionStateProgramAndSnapshot(t *testing.T) {
	// control-law: resolution emits one immutable state-program CAS identity
	base := model.Snapshot{
		Observation: model.Observation{Invocation: model.InvocationContext{RepositoryID: "repo-fixture"}, StateRevision: 41, ProgramFingerprint: strings.Repeat("a", 64)},
		Fingerprint: strings.Repeat("b", 64),
	}
	snapshot := func(revision uint64, program, fingerprint string) model.Snapshot {
		value := base
		value.StateRevision = revision
		value.ProgramFingerprint = program
		value.Fingerprint = fingerprint
		return value
	}
	transition := catalog.Transition{ID: "program/advance"}
	capabilities := CapabilityProjection{AuthorityFingerprint: "auth-test", Required: []catalog.Capability{catalog.CapabilityRepositoryWrite}, Effective: []catalog.Capability{catalog.CapabilityRepositoryWrite}}
	one, err := NewPrescription(base, transition, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if err := one.ValidateCurrent(base, transition, capabilities); err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name       string
		snapshot   model.Snapshot
		transition catalog.Transition
	}{
		{name: "instance", snapshot: func() model.Snapshot { value := base; value.Invocation.RepositoryID = "repo-other"; return value }(), transition: transition},
		{name: "state", snapshot: snapshot(42, strings.Repeat("a", 64), strings.Repeat("b", 64)), transition: transition},
		{name: "program", snapshot: snapshot(41, strings.Repeat("c", 64), strings.Repeat("b", 64)), transition: transition},
		{name: "snapshot", snapshot: snapshot(41, strings.Repeat("a", 64), strings.Repeat("d", 64)), transition: transition},
		{name: "transition", snapshot: base, transition: catalog.Transition{ID: "program/other"}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			other, err := NewPrescription(mutation.snapshot, mutation.transition, capabilities)
			if err != nil {
				t.Fatal(err)
			}
			if other.ID == one.ID {
				t.Fatal("load-bearing prescription change preserved content identity")
			}
		})
	}
	tampered := one
	tampered.ExpectedStateRevision++
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered prescription retained validity")
	}

	for name, changed := range map[string]CapabilityProjection{
		"authority":            {AuthorityFingerprint: "auth-other", Required: capabilities.Required, Effective: capabilities.Effective},
		"required-capability":  {AuthorityFingerprint: capabilities.AuthorityFingerprint, Required: []catalog.Capability{catalog.CapabilityCommandExecute}, Effective: []catalog.Capability{catalog.CapabilityCommandExecute}},
		"effective-capability": {AuthorityFingerprint: capabilities.AuthorityFingerprint, Required: capabilities.Required, Effective: []catalog.Capability{catalog.CapabilityCommandExecute}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := one.ValidateCurrent(base, transition, changed); err == nil {
				t.Fatal("old prescription accepted changed authority or capability context")
			}
		})
	}
}
