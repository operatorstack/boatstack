// Package humanidentity owns the repository-selected human actor descriptor.
// It validates and fingerprints descriptor data but never executes commands.
package humanidentity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	MaxActorBytes          = 1024
	MaxRoleBytes           = 128
	MaxCommandBytes        = 256
	MaxArgumentCount       = 32
	MaxArgumentBytes       = 1024
	MaxDescriptorArgvBytes = 8 << 10
	MaxCommandOutputBytes  = 1024
)

const (
	KindLiteral = "literal"
	KindCommand = "command"
)

var actorPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var rolePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// Descriptor is the closed, domain-neutral human identity provider contract.
// Args must be present for command descriptors, including when it is empty.
type Descriptor struct {
	Kind    string   `json:"kind"`
	Value   string   `json:"value,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

func (d *Descriptor) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&fields); err != nil {
		return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: trailing JSON")
	}
	var kind string
	if value, ok := fields["kind"]; !ok || json.Unmarshal(value, &kind) != nil {
		return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: kind is required")
	}
	decodeExact := func(names ...string) error {
		if len(fields) != len(names) {
			return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: %s descriptor has unknown, missing, or inapplicable fields", kind)
		}
		for _, name := range names {
			if _, ok := fields[name]; !ok {
				return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: %s descriptor is missing %s", kind, name)
			}
		}
		return nil
	}
	var candidate Descriptor
	switch kind {
	case KindLiteral:
		if err := decodeExact("kind", "value"); err != nil {
			return err
		}
		candidate.Kind = kind
		if err := json.Unmarshal(fields["value"], &candidate.Value); err != nil {
			return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: literal value must be a string")
		}
	case KindCommand:
		if err := decodeExact("kind", "command", "args"); err != nil {
			return err
		}
		candidate.Kind = kind
		if err := json.Unmarshal(fields["command"], &candidate.Command); err != nil {
			return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: command must be a string")
		}
		if string(fields["args"]) == "null" || json.Unmarshal(fields["args"], &candidate.Args) != nil || candidate.Args == nil {
			return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: args must be an explicit string array")
		}
	default:
		return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: unsupported kind %q", kind)
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*d = candidate
	return nil
}

func (d Descriptor) MarshalJSON() ([]byte, error) {
	canonical, err := d.Canonical()
	if err != nil {
		return nil, err
	}
	return json.Marshal(canonical)
}

// Presentation is safe host-facing provenance. It proposes an actor-selection
// mechanism but grants no authority and proves no provider capability.
type Presentation struct {
	Role                string     `json:"role"`
	ProviderFingerprint string     `json:"provider_fingerprint"`
	Descriptor          Descriptor `json:"descriptor"`
}

func ValidateRole(role string) error {
	if len(role) == 0 || len(role) > MaxRoleBytes || !rolePattern.MatchString(role) {
		return fmt.Errorf("HUMAN_IDENTITY_ROLE_INVALID: role must be 1-%d bytes and match %s", MaxRoleBytes, rolePattern)
	}
	return nil
}

func ValidateActor(actor string) error {
	if len(actor) == 0 || len(actor) > MaxActorBytes || !actorPattern.MatchString(actor) {
		return fmt.Errorf("HUMAN_ACTOR_INVALID: actor must be 1-%d bytes and match %s", MaxActorBytes, actorPattern)
	}
	return nil
}

func (d Descriptor) Validate() error {
	switch d.Kind {
	case KindLiteral:
		if d.Command != "" || d.Args != nil {
			return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: literal descriptor contains command fields")
		}
		if err := ValidateActor(d.Value); err != nil {
			return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: %w", err)
		}
	case KindCommand:
		if d.Value != "" || d.Command == "" || len(d.Command) > MaxCommandBytes || strings.TrimSpace(d.Command) == "" || strings.ContainsRune(d.Command, 0) || d.Args == nil {
			return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: command descriptor requires a bounded command and explicit args")
		}
		if len(d.Args) > MaxArgumentCount {
			return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: command descriptor exceeds %d arguments", MaxArgumentCount)
		}
		total := len(d.Command)
		for _, argument := range d.Args {
			if len(argument) > MaxArgumentBytes || strings.ContainsRune(argument, 0) {
				return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: command argument is invalid")
			}
			total += len(argument)
		}
		if total > MaxDescriptorArgvBytes {
			return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: command and args exceed %d bytes", MaxDescriptorArgvBytes)
		}
	default:
		return fmt.Errorf("HUMAN_IDENTITY_DESCRIPTOR_INVALID: unsupported kind %q", d.Kind)
	}
	return nil
}

// Canonical returns the representation used for provider fingerprinting.
func (d Descriptor) Canonical() (any, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if d.Kind == KindLiteral {
		return struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}{Kind: KindLiteral, Value: d.Value}, nil
	}
	return struct {
		Kind    string   `json:"kind"`
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}{Kind: KindCommand, Command: d.Command, Args: append([]string{}, d.Args...)}, nil
}

func (d Descriptor) Fingerprint() (string, error) {
	canonical, err := d.Canonical()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func NewPresentation(role string, descriptor Descriptor) (Presentation, error) {
	if err := ValidateRole(role); err != nil {
		return Presentation{}, err
	}
	fingerprint, err := descriptor.Fingerprint()
	if err != nil {
		return Presentation{}, err
	}
	return Presentation{Role: role, ProviderFingerprint: fingerprint, Descriptor: descriptor}, nil
}

func (p Presentation) Validate() error {
	if err := ValidateRole(p.Role); err != nil {
		return fmt.Errorf("HUMAN_IDENTITY_PRESENTATION_INVALID: %w", err)
	}
	fingerprint, err := p.Descriptor.Fingerprint()
	if err != nil || p.ProviderFingerprint != fingerprint {
		return fmt.Errorf("HUMAN_IDENTITY_PRESENTATION_INVALID: provider fingerprint does not match descriptor")
	}
	return nil
}

// BindingFingerprint binds a semantic role to its descriptor-only provider
// fingerprint without redefining either identity.
func (p Presentation) BindingFingerprint() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		SchemaVersion       int    `json:"schema_version"`
		Role                string `json:"role"`
		ProviderFingerprint string `json:"provider_fingerprint"`
	}{SchemaVersion: 1, Role: p.Role, ProviderFingerprint: p.ProviderFingerprint})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// InterpretCommandOutput applies the host contract to already captured output.
// It does not start or resolve an executable.
func InterpretCommandOutput(exitStatus int, stdout []byte) (string, error) {
	if exitStatus != 0 {
		return "", fmt.Errorf("HUMAN_IDENTITY_RESOLUTION_FAILED: command exited with status %d", exitStatus)
	}
	if len(stdout) == 0 || len(stdout) > MaxCommandOutputBytes {
		return "", fmt.Errorf("HUMAN_IDENTITY_RESOLUTION_FAILED: stdout must be 1-%d bytes", MaxCommandOutputBytes)
	}
	value := append([]byte(nil), stdout...)
	if bytes.HasSuffix(value, []byte("\r\n")) {
		value = value[:len(value)-2]
	} else if bytes.HasSuffix(value, []byte("\n")) {
		value = value[:len(value)-1]
	}
	if len(value) == 0 || bytes.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("HUMAN_IDENTITY_RESOLUTION_FAILED: stdout must contain exactly one non-empty logical line")
	}
	actor := string(value)
	if err := ValidateActor(actor); err != nil {
		return "", fmt.Errorf("HUMAN_IDENTITY_RESOLUTION_FAILED: %w", err)
	}
	return actor, nil
}
