// Package core owns the first-party CoreSystem definition. It declares
// Boatstack operational capabilities but no software-delivery flow.
package core

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"

	"github.com/operatorstack/boatstack/boatstack/control"
)

const (
	ID      = "boatstack.core"
	Version = "1.0.0"
)

type system struct{}

//go:embed transitions.json
var transitionDeclarations []byte

func System() control.CoreSystemDefinition { return system{} }

func (system) CoreManifest(context.Context) (control.CoreSystemManifest, error) {
	transitions, err := decodeTransitions()
	if err != nil {
		return control.CoreSystemManifest{}, err
	}
	var capabilities []control.Capability
	for index := range transitions {
		transitions[index].RequiredCapabilities = control.KernelEffectCapabilities(transitions[index])
		capabilities = control.UnionCapabilities(capabilities, transitions[index].RequiredCapabilities)
	}
	return control.CoreSystemManifest{ID: ID, Version: Version, Capabilities: capabilities, Transitions: transitions}, nil
}

func decodeTransitions() ([]control.Transition, error) {
	decoder := json.NewDecoder(bytes.NewReader(transitionDeclarations))
	decoder.DisallowUnknownFields()
	var transitions []control.Transition
	if err := decoder.Decode(&transitions); err != nil {
		return nil, fmt.Errorf("decode CoreSystem transitions: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("CoreSystem transition declarations contain trailing JSON")
	}
	return transitions, nil
}
