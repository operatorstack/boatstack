package catalog

import (
	"fmt"
	"sort"
	"strings"
)

// Capability names a kernel-enforced class of effect. A declaration narrows
// what a program may exercise; it never grants authority.
type Capability string

const (
	CapabilityRepositoryWrite    Capability = "repository.write"
	CapabilityCommandExecute     Capability = "command.execute"
	CapabilityProductMutate      Capability = "product.mutate"
	CapabilityPublicationPrepare Capability = "publication.prepare"
	CapabilityPublicationPublish Capability = "publication.publish"
	CapabilityHumanApprove       Capability = "human.approve"
)

func (c Capability) Valid() bool {
	switch c {
	case CapabilityRepositoryWrite, CapabilityCommandExecute, CapabilityProductMutate,
		CapabilityPublicationPrepare, CapabilityPublicationPublish, CapabilityHumanApprove:
		return true
	default:
		return false
	}
}

type CapabilitySet map[Capability]bool

func NewCapabilitySet(values ...Capability) CapabilitySet {
	set := CapabilitySet{}
	for _, value := range values {
		if value.Valid() {
			set[value] = true
		}
	}
	return set
}

func (s CapabilitySet) ContainsAll(values []Capability) bool {
	for _, value := range values {
		if !s[value] {
			return false
		}
	}
	return true
}

func (s CapabilitySet) Sorted() []Capability {
	result := make([]Capability, 0, len(s))
	for value, present := range s {
		if present {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func NormalizeCapabilities(field string, values []Capability) ([]Capability, error) {
	seen := CapabilitySet{}
	for _, value := range values {
		if !value.Valid() {
			return nil, fmt.Errorf("%s contains unknown capability %q", field, value)
		}
		if seen[value] {
			return nil, fmt.Errorf("%s duplicates capability %q", field, value)
		}
		seen[value] = true
	}
	return seen.Sorted(), nil
}

func UnionCapabilities(groups ...[]Capability) []Capability {
	set := CapabilitySet{}
	for _, group := range groups {
		for _, value := range group {
			if value.Valid() {
				set[value] = true
			}
		}
	}
	return set.Sorted()
}

func MissingCapability(required []Capability, available CapabilitySet) Capability {
	for _, value := range required {
		if !available[value] {
			return value
		}
	}
	return ""
}

// AuthorityCapabilities is kernel-owned. Receipt classes are evidence sources;
// repository programs cannot alter this mapping.
func AuthorityCapabilities(authority AuthoritySet) CapabilitySet {
	granted := CapabilitySet{}
	grant := func(values ...Capability) {
		for _, value := range values {
			granted[value] = true
		}
	}
	if authority[AuthorityRepository] {
		grant(CapabilityRepositoryWrite, CapabilityCommandExecute, CapabilityProductMutate, CapabilityPublicationPrepare)
	}
	if authority[AuthorityHuman] {
		grant(CapabilityRepositoryWrite, CapabilityCommandExecute, CapabilityProductMutate, CapabilityPublicationPrepare, CapabilityHumanApprove)
	}
	if authority[AuthorityAutonomy] {
		grant(CapabilityRepositoryWrite, CapabilityCommandExecute, CapabilityProductMutate, CapabilityPublicationPrepare)
	}
	if authority[AuthorityProvider] {
		grant(CapabilityPublicationPublish)
	}
	return granted
}

// KernelEffectCapabilities independently classifies the minimum capabilities
// of a compiled effect. It never trusts RequiredCapabilities to weaken this
// result. RuntimeExecution is compiler-derived, not repository-authored.
func KernelEffectCapabilities(transition Transition) []Capability {
	if !transition.Controllable() {
		return nil
	}
	required := NewCapabilitySet(CapabilityRepositoryWrite)
	if transition.RuntimeExecution {
		required[CapabilityCommandExecute] = true
	}
	id := string(transition.Effect)
	switch id {
	case "gate.build.record", "gate.test.record", "workspace.cut", "workspace.sync", "workspace.cleanup", "workspace.reap",
		"publication.observe", "publication.reconcile", "publication.execute", "publication.correct":
		required[CapabilityCommandExecute] = true
	}
	if strings.HasPrefix(id, "objective.") || strings.HasPrefix(id, "plan.") || strings.HasPrefix(id, "workspace.") ||
		strings.HasPrefix(id, "gate.") || strings.HasPrefix(id, "evidence.") || strings.HasPrefix(id, "delivery.") ||
		strings.HasPrefix(id, "publication.") {
		required[CapabilityProductMutate] = true
	}
	if id == "publication.preview" {
		required[CapabilityPublicationPrepare] = true
	}
	if id == "publication.execute" || id == "publication.correct" {
		required[CapabilityPublicationPublish] = true
	}
	return required.Sorted()
}

func RequiredCapabilities(transition Transition) []Capability {
	return UnionCapabilities(transition.RequiredCapabilities, KernelEffectCapabilities(transition))
}
