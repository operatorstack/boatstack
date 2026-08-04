package boatstack

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func authorityFixture(t *testing.T, repo string) (ExternalAuthorityPolicy, AuthorityBoundaryReceipt, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	context, err := ResolveAuthorityContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	authorityNow = func() time.Time { return now }
	t.Cleanup(func() { authorityNow = time.Now })
	t.Setenv(AuthorityHostSessionEnv, "session-123")
	t.Setenv(AuthorityPrincipalEnv, "principal-sha256")
	storeValue, err := MarshalJSON(externalAuthorityTrustStore{SchemaVersion: 1, Issuers: map[string]string{"broker": base64.StdEncoding.EncodeToString(publicKey)}})
	if err != nil {
		t.Fatal(err)
	}
	trustStore := filepath.Join(t.TempDir(), "authority-trust-store.json")
	if err := os.WriteFile(trustStore, storeValue, 0o600); err != nil {
		t.Fatal(err)
	}
	authorityTrustStoreProtected = func(string) error { return nil }
	t.Cleanup(func() { authorityTrustStoreProtected = protectedExternalTrustStore })
	policy := ExternalAuthorityPolicy{Mode: "credential-enforced", TrustStore: trustStore}
	receipt := AuthorityBoundaryReceipt{
		SchemaVersion: 1, RepositoryFingerprint: context.RepositoryFingerprint,
		WorktreeFingerprint: context.WorktreeFingerprint, HostSession: "session-123",
		PrincipalFingerprint: "principal-sha256", AuthorityClass: AuthorityClassRepositoryOnly,
		CloudControlPlaneAuthority: false, EnforcedBy: "credential-broker", Issuer: "broker",
		IssuedAt: now.Add(-time.Minute).Format(time.RFC3339), ExpiresAt: now.Add(10 * time.Minute).Format(time.RFC3339),
	}
	return policy, receipt, privateKey
}

// control-law: categorical-authority-requires-external-attestation
func TestCredentialEnforcedAuthorityRejectsPrincipalOwnedTrustStore(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	value, err := MarshalJSON(externalAuthorityTrustStore{SchemaVersion: 1, Issuers: map[string]string{"self": base64.StdEncoding.EncodeToString(publicKey)}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "self-authored-trust-store.json")
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
	status, reason := verifyAuthorityBoundary(runTestRepo(t), &ExternalAuthorityPolicy{Mode: "credential-enforced", TrustStore: path})
	if status != AuthorityHookGuarded || !strings.Contains(reason, "trust store") {
		t.Fatalf("principal-owned trust store was accepted: %s: %s", status, reason)
	}
}

func signAuthorityReceipt(t *testing.T, receipt AuthorityBoundaryReceipt, privateKey ed25519.PrivateKey) AuthorityBoundaryReceipt {
	t.Helper()
	payload, err := AuthorityReceiptSigningBytes(receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return receipt
}

func writeAuthorityReceipt(t *testing.T, receipt AuthorityBoundaryReceipt) string {
	t.Helper()
	value, err := MarshalJSON(receipt)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "authority-receipt.json")
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(AuthorityReceiptEnv, path)
	return path
}

// control-law: categorical-authority-requires-external-attestation
func TestCredentialEnforcedAuthorityReceiptIsAccepted(t *testing.T) {
	repo := runTestRepo(t)
	policy, receipt, privateKey := authorityFixture(t, repo)
	writeAuthorityReceipt(t, signAuthorityReceipt(t, receipt, privateKey))
	status, reason := verifyAuthorityBoundary(repo, &policy)
	if status != AuthorityCredentialEnforced || !strings.Contains(reason, "external authority") {
		t.Fatalf("valid external authority receipt was rejected: %s: %s", status, reason)
	}
}

// control-law: categorical-authority-requires-external-attestation
func TestCredentialEnforcedAuthorityReceiptRejectsInvalidBindings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AuthorityBoundaryReceipt)
		env    func(*testing.T)
	}{
		{"forged signature", func(r *AuthorityBoundaryReceipt) { r.PrincipalFingerprint = "changed-after-signing" }, func(t *testing.T) { t.Setenv(AuthorityPrincipalEnv, "changed-after-signing") }},
		{"stale", func(r *AuthorityBoundaryReceipt) {
			r.IssuedAt = "2026-08-04T10:00:00Z"
			r.ExpiresAt = "2026-08-04T10:10:00Z"
		}, nil},
		{"wrong worktree", func(r *AuthorityBoundaryReceipt) { r.WorktreeFingerprint = "wrong" }, nil},
		{"wrong session", func(r *AuthorityBoundaryReceipt) { r.HostSession = "other" }, nil},
		{"wrong principal", func(r *AuthorityBoundaryReceipt) { r.PrincipalFingerprint = "other" }, nil},
		{"overprivileged", func(r *AuthorityBoundaryReceipt) { r.CloudControlPlaneAuthority = true }, nil},
		{"self-authored issuer", func(r *AuthorityBoundaryReceipt) { r.Issuer = "untrusted" }, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := runTestRepo(t)
			policy, receipt, privateKey := authorityFixture(t, repo)
			receipt = signAuthorityReceipt(t, receipt, privateKey)
			test.mutate(&receipt)
			if test.name != "forged signature" {
				receipt = signAuthorityReceipt(t, receipt, privateKey)
			}
			if test.env != nil {
				test.env(t)
			}
			writeAuthorityReceipt(t, receipt)
			status, _ := verifyAuthorityBoundary(repo, &policy)
			if status != AuthorityHookGuarded {
				t.Fatalf("invalid %s receipt was accepted: %s", test.name, status)
			}
		})
	}
}

// control-law: categorical-authority-requires-external-attestation
func TestCredentialEnforcedModeFailsClosedWithoutReceiptAndPreservesEffect(t *testing.T) {
	repo := runTestRepo(t)
	t.Setenv(AuthorityReceiptEnv, "")
	config := testConfig()
	config.Project.DefaultBranch = "main"
	config.Adapters = []string{"cursor"}
	config.Workflow.ExternalAuthority = &ExternalAuthorityPolicy{Mode: "credential-enforced", TrustStore: filepath.Join(t.TempDir(), "trust-store.json")}
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
	status := CheckRunPreflight(repo, "")
	externalEffectApplied := false
	if status.VerificationStatus == "VERIFIED" {
		externalEffectApplied = true
	}
	if status.Relation != "AUTHORITY_BOUNDARY" || status.AuthorityStatus != AuthorityHookGuarded || externalEffectApplied {
		t.Fatalf("missing receipt did not fail closed before the effect: %+v effect=%t", status, externalEffectApplied)
	}
}

// control-law: categorical-authority-requires-external-attestation
func TestRunPreflightReportsCredentialEnforcedForValidReceipt(t *testing.T) {
	repo := runTestRepo(t)
	policy, receipt, privateKey := authorityFixture(t, repo)
	config := testConfig()
	config.Project.DefaultBranch = "main"
	config.Adapters = []string{"cursor"}
	config.Workflow.ExternalAuthority = &policy
	value, err := MarshalJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".product-loop", "project.json"), value, 0o644); err != nil {
		t.Fatal(err)
	}
	writeAuthorityReceipt(t, signAuthorityReceipt(t, receipt, privateKey))
	remote := filepath.Join(t.TempDir(), "origin.git")
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, output)
	}
	for _, args := range [][]string{
		{"remote", "add", "origin", remote},
		{"push", "-u", "origin", "main"},
		{"switch", "-c", "feature"},
	} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
		}
	}
	status := CheckRunPreflight(repo, "")
	if status.VerificationStatus != "VERIFIED" || status.AuthorityStatus != AuthorityCredentialEnforced {
		t.Fatalf("valid receipt did not admit the managed run: %+v", status)
	}
}

func TestExternalAuthorityPolicyValidation(t *testing.T) {
	config := testConfig()
	config.Workflow.ExternalAuthority = &ExternalAuthorityPolicy{Mode: "credential-enforced"}
	if err := ValidateConfig(config); err == nil || !strings.Contains(err.Error(), "trust_store") {
		t.Fatalf("credential-enforced mode accepted no external trust store: %v", err)
	}
	config.Workflow.ExternalAuthority = &ExternalAuthorityPolicy{Mode: "unknown"}
	if err := ValidateConfig(config); err == nil || !strings.Contains(err.Error(), "external_authority.mode") {
		t.Fatalf("unknown external authority mode was accepted: %v", err)
	}
}
