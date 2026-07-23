package boatstack

import (
	"fmt"
	"strings"
)

// Capability describes a named producer of PR evidence. It is the generic spine
// that concrete evidence types register on: detection ("can this repository
// produce the evidence?"), capture orchestration ("what command runs it, and
// when?"), and provisioning ("if it is unavailable, how do we help?") all read
// this registry instead of hard-coding a single evidence type.
//
// Boatstack ships the contract in this registry, not the capture harness. The
// harness is authored in the user's repository and invoked through the resolved
// repository command; Boatstack only records what the command must satisfy.
type Capability struct {
	// Name is the canonical capability identifier, e.g. "visual".
	Name string
	// CommandAliases are the project.commands keys that satisfy the capability,
	// in priority order. The first non-empty command wins.
	CommandAliases []string
	// AdmittedStages are the delivery stages in which a capture attempt may run.
	AdmittedStages []string
	// RetryClass is the operation retry class recorded for a capture attempt.
	RetryClass string
}

// CapabilityResolution is the portable capability cut for one capability: it
// reports whether the repository owns a command that produces this evidence.
type CapabilityResolution struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // repository-command | unavailable
	Command string `json:"command,omitempty"`
}

// capabilityRegistry holds every evidence capability Boatstack knows about.
// Adding a provider is a single entry here plus its tenant-specific manifest
// contract (see visual_evidence.go for the first tenant).
var capabilityRegistry = map[string]Capability{
	"visual": {
		Name:           "visual",
		CommandAliases: []string{"visual", "screenshot", "e2e"},
		AdmittedStages: []string{"BUILD", "TEST_PASSED"},
		RetryClass:     "IDEMPOTENT_EXTERNAL",
	},
}

// LookupCapability returns the registered capability metadata for a name.
func LookupCapability(name string) (Capability, bool) {
	capability, ok := capabilityRegistry[strings.TrimSpace(name)]
	return capability, ok
}

// ResolveCapability performs the repository-owned capability cut: it selects the
// first project command alias that is set. It is the generic detection primitive
// shared by capture orchestration and provisioning. Kind is "repository-command"
// when the repository owns a command, otherwise "unavailable".
func ResolveCapability(name string, config ProjectConfig) (CapabilityResolution, error) {
	capability, ok := LookupCapability(name)
	if !ok {
		return CapabilityResolution{}, fmt.Errorf("unknown evidence capability %q", name)
	}
	for _, alias := range capability.CommandAliases {
		if command := strings.TrimSpace(config.Project.Commands[alias]); command != "" {
			return CapabilityResolution{Name: capability.Name, Kind: "repository-command", Command: command}, nil
		}
	}
	return CapabilityResolution{Name: capability.Name, Kind: "unavailable"}, nil
}
