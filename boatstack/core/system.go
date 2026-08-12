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

	"github.com/operatorstack/boatstack/boatstack/delivery"
)

const (
	ID      = "boatstack.core"
	Version = "1.0.0"
)

type system struct{}

//go:embed transitions.json
var transitionDeclarations []byte

func System() delivery.CoreSystemDefinition { return system{} }

func (system) CoreManifest(context.Context) (delivery.CoreSystemManifest, error) {
	transitions, err := decodeTransitions()
	if err != nil {
		return delivery.CoreSystemManifest{}, err
	}
	var capabilities []delivery.Capability
	for index := range transitions {
		transitions[index].RequiredCapabilities = delivery.KernelEffectCapabilities(transitions[index])
		capabilities = delivery.UnionCapabilities(capabilities, transitions[index].RequiredCapabilities)
	}
	return delivery.CoreSystemManifest{ID: ID, Version: Version, Capabilities: capabilities, Transitions: transitions}, nil
}

func decodeTransitions() ([]delivery.Transition, error) {
	decoder := json.NewDecoder(bytes.NewReader(transitionDeclarations))
	decoder.DisallowUnknownFields()
	var transitions []delivery.Transition
	if err := decoder.Decode(&transitions); err != nil {
		return nil, fmt.Errorf("decode CoreSystem transitions: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("CoreSystem transition declarations contain trailing JSON")
	}
	return transitions, nil
}
