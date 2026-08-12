package protocol

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectConfigurationIsStrictAndVersioned(t *testing.T) {
	valid := []byte(`{"schema_version":2,"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli","codex"]}`)
	if _, err := DecodeProjectConfig(valid); err != nil {
		t.Fatal(err)
	}
	invalid := [][]byte{
		[]byte(`{"schema_version":1,"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"]}`),
		[]byte(`{"schema_version":2,"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["unknown"]}`),
		[]byte(`{"schema_version":2,"project":{"name":"product","default_branch":"main","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"],"legacy":true}`),
		[]byte(`{"schema_version":2,"project":{"name":"product","default_branch":"--upload-pack=bad","commands":{}},"policy":{"plan_approval":"human","visual_evidence":"optional"},"hosts":["cli"]}`),
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
		Project:       ProjectSettings{Name: "product", DefaultBranch: "main", Commands: map[string]string{}},
		Policy:        PolicySettings{PlanApproval: "human", VisualEvidence: "optional"},
		Hosts:         []string{"cli", "sdk"},
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
	one := []byte("{\n  \"schema_version\": 2,\n  \"project\": {\"name\": \"product\", \"default_branch\": \"main\", \"commands\": {\"test\": \"go test ./...\"}},\n  \"policy\": {\"plan_approval\": \"human\", \"visual_evidence\": \"optional\"},\n  \"hosts\": [\"codex\", \"cli\"]\n}\n")
	two := []byte("{\r\n\"hosts\":[\"cli\",\"codex\"],\r\n\"policy\":{\"external_effect_authority\":\"human-or-autonomy-plus-provider\",\"visual_evidence\":\"optional\",\"plan_approval\":\"human\"},\r\n\"project\":{\"commands\":{\"test\":\"go test ./...\"},\"default_branch\":\"main\",\"name\":\"product\"},\r\n\"schema_version\":2\r\n}\r\n")
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

	changed := []byte(`{"schema_version":2,"project":{"name":"product","default_branch":"main","commands":{"test":"go test ./..."}},"policy":{"plan_approval":"human","visual_evidence":"required"},"hosts":["cli","codex"]}`)
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
