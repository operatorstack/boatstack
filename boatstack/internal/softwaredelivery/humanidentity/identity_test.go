package humanidentity_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
)

func TestLiteralAndCommandDescriptorsHaveDeterministicDistinctFingerprints(t *testing.T) {
	literal := humanidentity.Descriptor{Kind: humanidentity.KindLiteral, Value: "example-operator"}
	command := humanidentity.Descriptor{Kind: humanidentity.KindCommand, Command: "gh", Args: []string{"api", "user", "--jq", ".login"}}
	for _, descriptor := range []humanidentity.Descriptor{literal, command} {
		first, err := descriptor.Fingerprint()
		if err != nil {
			t.Fatal(err)
		}
		second, err := descriptor.Fingerprint()
		if err != nil || first != second || len(first) != 64 {
			t.Fatalf("nondeterministic fingerprint first=%q second=%q err=%v", first, second, err)
		}
		presentation, err := humanidentity.NewPresentation("developer", descriptor)
		if err != nil || presentation.Role != "developer" || presentation.ProviderFingerprint != first || presentation.Validate() != nil {
			t.Fatalf("presentation = %#v, err=%v", presentation, err)
		}
	}
	literalFingerprint, _ := literal.Fingerprint()
	changedFingerprint, _ := (humanidentity.Descriptor{Kind: humanidentity.KindLiteral, Value: "another-actor"}).Fingerprint()
	commandFingerprint, _ := command.Fingerprint()
	if literalFingerprint == changedFingerprint || literalFingerprint == commandFingerprint {
		t.Fatal("descriptor change preserved provider fingerprint")
	}
}

func TestRoleValidationAndBindingFingerprint(t *testing.T) {
	for _, role := range []string{"developer", "release-manager", "team.one_operator"} {
		if err := humanidentity.ValidateRole(role); err != nil {
			t.Fatalf("valid role %q: %v", role, err)
		}
	}
	for _, role := range []string{"", "Developer", "1developer", "developer role", strings.Repeat("x", humanidentity.MaxRoleBytes+1)} {
		if err := humanidentity.ValidateRole(role); err == nil {
			t.Fatalf("invalid role %q was accepted", role)
		}
	}
	descriptor := humanidentity.Descriptor{Kind: humanidentity.KindLiteral, Value: "operator"}
	developer, _ := humanidentity.NewPresentation("developer", descriptor)
	release, _ := humanidentity.NewPresentation("release-manager", descriptor)
	if developer.ProviderFingerprint != release.ProviderFingerprint {
		t.Fatal("role changed descriptor-only provider fingerprint")
	}
	developerBinding, _ := developer.BindingFingerprint()
	releaseBinding, _ := release.BindingFingerprint()
	if developerBinding == releaseBinding {
		t.Fatal("different roles shared a binding fingerprint")
	}
}

func TestCommandDescriptorJSONPreservesExplicitEmptyArgv(t *testing.T) {
	raw, err := json.Marshal(humanidentity.Descriptor{Kind: humanidentity.KindCommand, Command: "company-identity", Args: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"kind":"command","command":"company-identity","args":[]}` {
		t.Fatalf("command JSON = %s", raw)
	}
	var decoded humanidentity.Descriptor
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Args == nil {
		t.Fatalf("decoded command = %#v, err=%v", decoded, err)
	}
}

func TestDescriptorValidationRejectsMalformedUnionAndBounds(t *testing.T) {
	tooManyArgs := make([]string, humanidentity.MaxArgumentCount+1)
	for index := range tooManyArgs {
		tooManyArgs[index] = "x"
	}
	tests := []humanidentity.Descriptor{
		{},
		{Kind: "github", Value: "example-operator"},
		{Kind: humanidentity.KindLiteral},
		{Kind: humanidentity.KindLiteral, Value: "bad actor"},
		{Kind: humanidentity.KindLiteral, Value: "actor", Command: "gh"},
		{Kind: humanidentity.KindCommand, Args: []string{}},
		{Kind: humanidentity.KindCommand, Command: "gh"},
		{Kind: humanidentity.KindCommand, Command: "gh", Args: []string{"bad\x00argument"}},
		{Kind: humanidentity.KindCommand, Command: "gh", Args: tooManyArgs},
		{Kind: humanidentity.KindCommand, Command: strings.Repeat("x", humanidentity.MaxCommandBytes+1), Args: []string{}},
		{Kind: humanidentity.KindCommand, Command: "gh", Args: []string{strings.Repeat("x", humanidentity.MaxArgumentBytes+1)}},
	}
	for _, descriptor := range tests {
		if err := descriptor.Validate(); err == nil {
			t.Fatalf("malformed descriptor was accepted: %#v", descriptor)
		}
	}
}

func TestInterpretCommandOutputIsPureAndStrict(t *testing.T) {
	for _, output := range [][]byte{[]byte("example-operator"), []byte("example-operator\n"), []byte("example-operator\r\n")} {
		actor, err := humanidentity.InterpretCommandOutput(0, output)
		if err != nil || actor != "example-operator" {
			t.Fatalf("output %q => actor=%q err=%v", output, actor, err)
		}
	}
	for _, test := range []struct {
		status int
		value  []byte
	}{
		{1, []byte("example-operator\n")},
		{0, nil},
		{0, []byte("\n")},
		{0, []byte("example-operator\nother")},
		{0, []byte("example-operator\n\n")},
		{0, []byte("example-operator\x00")},
		{0, []byte("bad actor\n")},
		{0, []byte(strings.Repeat("x", humanidentity.MaxCommandOutputBytes+1))},
	} {
		if _, err := humanidentity.InterpretCommandOutput(test.status, test.value); err == nil {
			t.Fatalf("invalid command output status=%d value=%q was accepted", test.status, test.value)
		}
	}
}
