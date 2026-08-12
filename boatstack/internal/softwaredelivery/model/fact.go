package model

import (
	"fmt"
	"time"
)

// FactStatus keeps absence, uncertainty, staleness, ambiguity, and conflict
// distinct. A zero status is invalid so omitted controlling facts fail closed.
type FactStatus string

const (
	FactKnown       FactStatus = "known"
	FactAbsent      FactStatus = "absent"
	FactUnknown     FactStatus = "unknown"
	FactStale       FactStatus = "stale"
	FactAmbiguous   FactStatus = "ambiguous"
	FactConflicting FactStatus = "conflicting"
)

func (s FactStatus) Valid() bool {
	switch s {
	case FactKnown, FactAbsent, FactUnknown, FactStale, FactAmbiguous, FactConflicting:
		return true
	default:
		return false
	}
}

// Evidence identifies the observation that supports a controlling fact.
type Evidence struct {
	Source      string    `json:"source"`
	Fingerprint string    `json:"fingerprint"`
	Revision    string    `json:"revision,omitempty"`
	ObservedAt  time.Time `json:"observed_at,omitempty"`
}

// Fact is the only representation for controlling observed values.
type Fact[T any] struct {
	Status   FactStatus `json:"status"`
	Value    T          `json:"value,omitempty"`
	Evidence []Evidence `json:"evidence,omitempty"`
	Detail   string     `json:"detail,omitempty"`
}

func Known[T any](value T, evidence Evidence) Fact[T] {
	return Fact[T]{Status: FactKnown, Value: value, Evidence: []Evidence{evidence}}
}

func Absent[T any](detail string, evidence ...Evidence) Fact[T] {
	return Fact[T]{Status: FactAbsent, Detail: detail, Evidence: evidence}
}

func Unknown[T any](status FactStatus, detail string, evidence ...Evidence) Fact[T] {
	return Fact[T]{Status: status, Detail: detail, Evidence: evidence}
}

func (f Fact[T]) Validate(name string) error {
	if !f.Status.Valid() {
		return fmt.Errorf("%s: invalid or missing fact status %q", name, f.Status)
	}
	if f.Status == FactKnown && len(f.Evidence) == 0 {
		return fmt.Errorf("%s: known controlling fact has no evidence", name)
	}
	for i, evidence := range f.Evidence {
		if evidence.Source == "" || evidence.Fingerprint == "" {
			return fmt.Errorf("%s: evidence %d requires source and fingerprint", name, i)
		}
	}
	return nil
}
