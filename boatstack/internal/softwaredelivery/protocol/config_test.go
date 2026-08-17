package protocol

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/humanidentity"
)

const literalIdentityJSON = `"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},`

func literalIdentity() IdentitySettings {
	return IdentitySettings{Default: "developer", Roles: map[string]humanidentity.Descriptor{"developer": {Kind: humanidentity.KindLiteral, Value: "operator"}}}
}

func TestProjectConfigurationIsStrictAndVersioned(t *testing.T) {
	valid := []byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli","codex"],"projections":["codex"]}`)
	if _, err := DecodeProjectConfig(valid); err != nil {
		t.Fatal(err)
	}
	invalid := [][]byte{
		[]byte(`{"schema_version":4,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`),
		[]byte(`{"schema_version":5,"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`),
		[]byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["unknown"],"projections":[]}`),
		[]byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[],"legacy":true}`),
		[]byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},"project":{"name":"product","default_branch":"--upload-pack=bad","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`),
		[]byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"]}`),
		[]byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":null}`),
	}
	for _, value := range invalid {
		if _, err := DecodeProjectConfig(value); err == nil {
			t.Fatalf("invalid configuration was accepted: %s", value)
		}
	}
}

func TestRepositorySubprocessExtensionsAreStrictAndSemanticallyFingerprinted(t *testing.T) {
	// control-law: repository-extension-selection-binds-exact-executable-and-settings
	executable := filepath.Join(t.TempDir(), "extension")
	base := ProjectConfig{
		SchemaVersion: ConfigSchemaVersion,
		Identity:      literalIdentity(),
		Project:       ProjectSettings{Name: "product", DefaultBranch: "main", Commands: map[string]string{}},
		Policy:        PolicySettings{PlanApproval: "human", VisualEvidence: "optional"},
		Hosts:         []string{"cli", "sdk"},
		Projections:   []string{},
		Extensions: []SubprocessExtensionSettings{{
			ID: "example.guard", Version: "1.0.0", Executable: executable, SHA256: strings.Repeat("a", 64),
			Manifest: json.RawMessage(`{"id":"example.guard","version":"1.0.0","protocol_version":1,"settings_schema":{"type":"object"},"privacy_classification":"metadata-only","telemetry_classification":"transition-receipt"}`),
			Settings: json.RawMessage(`{"level":"strict","enabled":true}`), DeadlineMillis: 1000, StdoutBytes: 2048, StderrBytes: 1024,
		}},
	}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	_, fingerprint, err := ProjectConfigFingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	reordered := base
	reordered.Hosts = []string{"sdk", "cli"}
	reordered.Extensions = append([]SubprocessExtensionSettings(nil), base.Extensions...)
	reordered.Extensions[0].Settings = json.RawMessage(`{ "enabled" : true, "level" : "strict" }`)
	raw, _ = json.Marshal(reordered)
	_, reorderedFingerprint, err := ProjectConfigFingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	if reorderedFingerprint != fingerprint {
		t.Fatalf("representation changed extension policy fingerprint: %s != %s", reorderedFingerprint, fingerprint)
	}

	changed := base
	changed.Extensions = append([]SubprocessExtensionSettings(nil), base.Extensions...)
	changed.Extensions[0].SHA256 = strings.Repeat("b", 64)
	raw, _ = json.Marshal(changed)
	_, changedFingerprint, err := ProjectConfigFingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	if changedFingerprint == fingerprint {
		t.Fatal("executable identity change retained repository policy fingerprint")
	}

	invalid := base
	invalid.Extensions = append([]SubprocessExtensionSettings(nil), base.Extensions...)
	invalid.Extensions[0].Executable = "relative/extension"
	raw, _ = json.Marshal(invalid)
	if _, err := DecodeProjectConfig(raw); err == nil {
		t.Fatal("relative subprocess executable was accepted")
	}
}

func TestProjectConfigurationFingerprintIsSemanticAndStrict(t *testing.T) {
	one := []byte("{\n  \"schema_version\": 5,\n  \"identity\": {\"default\": \"developer\", \"roles\": {\"developer\": {\"kind\": \"literal\", \"value\": \"operator\"}}},\n  \"project\": {\"name\": \"product\", \"default_branch\": \"main\", \"commands\": {\"test\": \"go test ./...\"}},\n  \"policy\": {\"plan_approval\": \"human\", \"visual_evidence\": \"optional\"},\n  \"hosts\": [\"codex\", \"cli\"],\n  \"projections\": [\"codex\"]\n}\n")
	two := []byte("{\r\n\"hosts\":[\"cli\",\"codex\"],\r\n\"projections\":[\"codex\"],\r\n\"identity\":{\"roles\":{\"developer\":{\"value\":\"operator\",\"kind\":\"literal\"}},\"default\":\"developer\"},\r\n\"policy\":{\"external_effect_authority\":\"human-or-autonomy-plus-provider\",\"visual_evidence\":\"optional\",\"plan_approval\":\"human\"},\r\n\"project\":{\"commands\":{\"test\":\"go test ./...\"},\"default_branch\":\"main\",\"name\":\"product\"},\r\n\"schema_version\":5\r\n}\r\n")
	_, oneFingerprint, err := ProjectConfigFingerprint(one)
	if err != nil {
		t.Fatal(err)
	}
	_, twoFingerprint, err := ProjectConfigFingerprint(two)
	if err != nil {
		t.Fatal(err)
	}
	if oneFingerprint != twoFingerprint {
		t.Fatalf("representation changed semantic fingerprint: %s != %s", oneFingerprint, twoFingerprint)
	}

	changed := []byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}}},"project":{"name":"product","default_branch":"main","commands":{"test":"go test ./..."}},"policy":{"plan_approval":"human","visual_evidence":"required"},"hosts":["cli","codex"],"projections":["codex"]}`)
	_, changedFingerprint, err := ProjectConfigFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedFingerprint == oneFingerprint {
		t.Fatal("controlling policy change retained the old configuration fingerprint")
	}

	unknown := append([]byte(nil), one[:len(one)-2]...)
	unknown = append(unknown, []byte(",\"unknown\":true}\n")...)
	if _, _, err := ProjectConfigFingerprint(unknown); err == nil {
		t.Fatal("unknown configuration field acquired a semantic fingerprint")
	}
}

func TestProjectProjectionsAreExplicitCanonicalAndNonsemantic(t *testing.T) {
	base := ProjectConfig{
		SchemaVersion: ConfigSchemaVersion,
		Identity:      literalIdentity(),
		Project:       ProjectSettings{Name: "product", DefaultBranch: "main", Commands: map[string]string{}},
		Policy:        PolicySettings{PlanApproval: "human", VisualEvidence: "optional"},
		Hosts:         []string{"cli", "codex", "claude", "cursor", "gemini"},
		Projections:   []string{"cursor", "codex"},
	}
	raw, _ := json.Marshal(base)
	config, fingerprint, err := ProjectConfigFingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := config.ProjectionSelectionFingerprint()
	if err != nil || len(selection) != 64 {
		t.Fatalf("selection fingerprint = %q, %v", selection, err)
	}
	reordered := base
	reordered.Hosts = []string{"gemini", "cursor", "claude", "codex", "cli"}
	reordered.Projections = []string{"codex", "cursor"}
	raw, _ = json.Marshal(reordered)
	_, reorderedFingerprint, err := ProjectConfigFingerprint(raw)
	if err != nil || reorderedFingerprint != fingerprint {
		t.Fatalf("reordered fingerprint = %q, %v, want %q", reorderedFingerprint, err, fingerprint)
	}

	changed := base
	changed.Projections = []string{"codex"}
	raw, _ = json.Marshal(changed)
	changedConfig, changedFingerprint, err := ProjectConfigFingerprint(raw)
	if err != nil {
		t.Fatal(err)
	}
	if changedFingerprint == fingerprint {
		t.Fatal("projection membership retained project configuration fingerprint")
	}
	if !reflect.DeepEqual(config.ControlPolicy(), changedConfig.ControlPolicy()) {
		t.Fatalf("projection membership changed runtime policy: %#v != %#v", config.ControlPolicy(), changedConfig.ControlPolicy())
	}

	for _, projection := range []string{"codex", "claude", "cursor", "gemini"} {
		candidate := base
		candidate.Projections = []string{projection}
		raw, _ = json.Marshal(candidate)
		if _, err := DecodeProjectConfig(raw); err != nil {
			t.Fatalf("valid projection %q: %v", projection, err)
		}
	}
	for _, projection := range []string{"cli", "mcp", "sdk", "unknown"} {
		candidate := base
		candidate.Projections = []string{projection}
		raw, _ = json.Marshal(candidate)
		if _, err := DecodeProjectConfig(raw); err == nil {
			t.Fatalf("invalid projection %q was accepted", projection)
		}
	}
	withoutHost := base
	withoutHost.Hosts = []string{"cli", "codex"}
	withoutHost.Projections = []string{"cursor"}
	raw, _ = json.Marshal(withoutHost)
	if _, err := DecodeProjectConfig(raw); err == nil || !strings.Contains(err.Error(), "PROJECT_PROJECTION_HOST_DISABLED") {
		t.Fatalf("disabled-host projection error = %v", err)
	}
	empty := base
	empty.Projections = []string{}
	raw, _ = json.Marshal(empty)
	if _, err := DecodeProjectConfig(raw); err != nil {
		t.Fatalf("explicit empty projections: %v", err)
	}
}

func TestProjectConfigurationBindsHumanIdentityDescriptor(t *testing.T) {
	literal := []byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"example-operator"}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`)
	command := []byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"command","command":"gh","args":["api","user","--jq",".login"]}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`)
	literalConfig, literalFingerprint, err := ProjectConfigFingerprint(literal)
	if err != nil {
		t.Fatal(err)
	}
	commandConfig, commandFingerprint, err := ProjectConfigFingerprint(command)
	if err != nil {
		t.Fatal(err)
	}
	if literalConfig.Identity.Roles["developer"].Kind != humanidentity.KindLiteral || commandConfig.Identity.Roles["developer"].Kind != humanidentity.KindCommand {
		t.Fatalf("decoded identities literal=%#v command=%#v", literalConfig.Identity, commandConfig.Identity)
	}
	if literalFingerprint == commandFingerprint {
		t.Fatal("identity descriptor change preserved project configuration fingerprint")
	}
	for _, invalid := range [][]byte{
		[]byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"command","command":"gh"}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`),
		[]byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"command","command":"gh","args":null}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`),
		[]byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"actor","command":""}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`),
		[]byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"actor","unknown":true}}},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`),
	} {
		if _, err := DecodeProjectConfig(invalid); err == nil {
			t.Fatalf("invalid identity config was accepted: %s", invalid)
		}
	}
}

func TestProjectConfigurationRequiresExplicitNamedRoles(t *testing.T) {
	base := ProjectConfig{
		SchemaVersion: ConfigSchemaVersion,
		Identity: IdentitySettings{Default: "developer", Roles: map[string]humanidentity.Descriptor{
			"developer":       {Kind: humanidentity.KindLiteral, Value: "operator"},
			"release-manager": {Kind: humanidentity.KindLiteral, Value: "release-operator"},
		}},
		Project: ProjectSettings{Name: "product", DefaultBranch: "main", Commands: map[string]string{}},
		Policy:  PolicySettings{PlanApproval: "human", VisualEvidence: "optional"}, Hosts: []string{"cli"}, Projections: []string{},
	}
	raw, _ := json.Marshal(base)
	decoded, fingerprint, err := ProjectConfigFingerprint(raw)
	if err != nil || decoded.Identity.Default != "developer" || len(fingerprint) != 64 {
		t.Fatalf("named roles = %#v fingerprint=%q err=%v", decoded.Identity, fingerprint, err)
	}
	reordered := base
	reordered.Identity.Roles = map[string]humanidentity.Descriptor{
		"release-manager": base.Identity.Roles["release-manager"],
		"developer":       base.Identity.Roles["developer"],
	}
	reorderedRaw, _ := json.Marshal(reordered)
	_, reorderedFingerprint, err := ProjectConfigFingerprint(reorderedRaw)
	if err != nil || reorderedFingerprint != fingerprint {
		t.Fatalf("role map order changed fingerprint: %q != %q, %v", reorderedFingerprint, fingerprint, err)
	}
	changedDefault := base
	changedDefault.Identity.Default = "release-manager"
	changedRaw, _ := json.Marshal(changedDefault)
	changedConfig, changedFingerprint, err := ProjectConfigFingerprint(changedRaw)
	if err != nil || changedFingerprint == fingerprint {
		t.Fatalf("changed default fingerprint=%q err=%v", changedFingerprint, err)
	}
	if !reflect.DeepEqual(decoded.ControlPolicy(), changedConfig.ControlPolicy()) {
		t.Fatal("human identity roles changed runtime host policy")
	}

	for name, identity := range map[string]IdentitySettings{
		"missing default": {Roles: base.Identity.Roles},
		"unknown default": {Default: "unknown", Roles: base.Identity.Roles},
		"empty roles":     {Default: "developer", Roles: map[string]humanidentity.Descriptor{}},
		"null roles":      {Default: "developer", Roles: nil},
		"invalid role":    {Default: "Developer", Roles: map[string]humanidentity.Descriptor{"Developer": base.Identity.Roles["developer"]}},
	} {
		candidate := base
		candidate.Identity = identity
		raw, _ := json.Marshal(candidate)
		if _, err := DecodeProjectConfig(raw); err == nil {
			t.Fatalf("%s identity was accepted: %s", name, raw)
		}
	}
	unknown := []byte(`{"schema_version":5,"identity":{"default":"developer","roles":{"developer":{"kind":"literal","value":"operator"}},"implicit":true},"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"projections":[]}`)
	if _, err := DecodeProjectConfig(unknown); err == nil {
		t.Fatal("unknown identity field was accepted")
	}
}

func TestGitReferencesRejectOptionsAndRevisionExpressions(t *testing.T) {
	for _, value := range []string{"main", "feature/v2", "HEAD", "0123456789abcdef"} {
		if err := ValidateGitReference(value); err != nil {
			t.Fatalf("valid reference %q: %v", value, err)
		}
	}
	for _, value := range []string{"--help", "HEAD~1", "main..other", "refs/heads/.hidden", "main.lock"} {
		if err := ValidateGitReference(value); err == nil {
			t.Fatalf("unsafe reference %q was accepted", value)
		}
	}
	if err := ValidateGitBranch("HEAD"); err == nil {
		t.Fatal("reserved HEAD was accepted as a branch")
	}
}
