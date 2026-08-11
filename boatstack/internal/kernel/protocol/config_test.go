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
