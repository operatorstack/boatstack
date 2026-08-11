package protocol

import "testing"

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
