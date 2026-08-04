package boatstack

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

const (
	AuthorityHookGuarded         = "HOOK_GUARDED"
	AuthorityCredentialEnforced  = "CREDENTIAL_ENFORCED"
	AuthorityClassRepositoryOnly = "repository-only"
	AuthorityReceiptEnv          = "BOATSTACK_AUTHORITY_RECEIPT"
	AuthorityHostSessionEnv      = "BOATSTACK_HOST_SESSION"
	AuthorityPrincipalEnv        = "BOATSTACK_PRINCIPAL_FINGERPRINT"
	maxAuthorityReceiptLifetime  = 15 * time.Minute
)

var (
	authorityNow                 = time.Now
	authorityTrustStoreProtected = protectedExternalTrustStore
)

// ExternalAuthorityPolicy chooses whether a managed run relies on hook-only
// interception or requires an independently signed, credential-enforced boundary.
// Boatstack stores only public verification keys and never signs its own receipt.
type ExternalAuthorityPolicy struct {
	Mode       string `json:"mode,omitempty"` // "" | "hook-only" | "credential-enforced"
	TrustStore string `json:"trust_store,omitempty"`
}

type externalAuthorityTrustStore struct {
	SchemaVersion int               `json:"schema_version"`
	Issuers       map[string]string `json:"issuers"`
}

// AuthorityBoundaryReceipt is supplied by an external authority such as service
// IAM, a credential broker, or an isolated host. Signature covers every field
// except Signature using AuthorityReceiptSigningBytes.
type AuthorityBoundaryReceipt struct {
	SchemaVersion              int    `json:"schema_version"`
	RepositoryFingerprint      string `json:"repository_fingerprint"`
	WorktreeFingerprint        string `json:"worktree_fingerprint"`
	HostSession                string `json:"host_session"`
	PrincipalFingerprint       string `json:"principal_fingerprint"`
	AuthorityClass             string `json:"authority_class"`
	CloudControlPlaneAuthority bool   `json:"cloud_control_plane_authority"`
	EnforcedBy                 string `json:"enforced_by"`
	Issuer                     string `json:"issuer"`
	IssuedAt                   string `json:"issued_at"`
	ExpiresAt                  string `json:"expires_at"`
	Signature                  string `json:"signature"`
}

type AuthorityContext struct {
	SchemaVersion         int    `json:"schema_version"`
	RepositoryFingerprint string `json:"repository_fingerprint"`
	WorktreeFingerprint   string `json:"worktree_fingerprint"`
}

// AuthorityReceiptSigningBytes is the stable external-attestor wire contract.
func AuthorityReceiptSigningBytes(receipt AuthorityBoundaryReceipt) ([]byte, error) {
	receipt.Signature = ""
	return json.Marshal(receipt)
}

func ResolveAuthorityContext(repoInput string) (AuthorityContext, error) {
	repo, err := ResolveRepository(repoInput)
	if err != nil {
		return AuthorityContext{}, err
	}
	common, err := gitCommonDir(repo)
	if err != nil {
		return AuthorityContext{}, err
	}
	repoPath, err := filepath.Abs(repo)
	if err != nil {
		return AuthorityContext{}, err
	}
	commonPath, err := filepath.Abs(common)
	if err != nil {
		return AuthorityContext{}, err
	}
	return AuthorityContext{
		SchemaVersion:         1,
		RepositoryFingerprint: SHA256Bytes([]byte(filepath.Clean(commonPath))),
		WorktreeFingerprint:   SHA256Bytes([]byte(filepath.Clean(repoPath))),
	}, nil
}

func normalizedAuthorityMode(policy *ExternalAuthorityPolicy) string {
	if policy == nil || strings.TrimSpace(policy.Mode) == "" {
		return "hook-only"
	}
	return strings.TrimSpace(policy.Mode)
}

func validateExternalAuthorityPolicy(policy *ExternalAuthorityPolicy) error {
	mode := normalizedAuthorityMode(policy)
	if mode != "hook-only" && mode != "credential-enforced" {
		return fmt.Errorf("workflow.external_authority.mode must be \"hook-only\" or \"credential-enforced\"")
	}
	if mode == "credential-enforced" && (policy == nil || !filepath.IsAbs(strings.TrimSpace(policy.TrustStore))) {
		return fmt.Errorf("workflow.external_authority.trust_store must be an absolute external path for credential-enforced mode")
	}
	return nil
}

func ownerID(info os.FileInfo) (uint64, bool) {
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return 0, false
	}
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return 0, false
	}
	uid := value.FieldByName("Uid")
	if !uid.IsValid() || !uid.CanUint() {
		return 0, false
	}
	return uid.Uint(), true
}

// protectedExternalTrustStore refuses trust roots the managed principal can
// replace. Production strict mode therefore requires an operator-provisioned
// file outside the repository under non-writable parent directories.
func protectedExternalTrustStore(path string) error {
	path = filepath.Clean(path)
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("external authority trust store path is missing or contains a symlink")
		}
		if current == path && !info.Mode().IsRegular() {
			return fmt.Errorf("external authority trust store is not a regular file")
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("external authority trust store path is group- or world-writable")
		}
		uid, ok := ownerID(info)
		if !ok || uid == uint64(os.Geteuid()) {
			return fmt.Errorf("external authority trust store path is owned by the managed principal")
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}

func loadExternalTrustStore(policy *ExternalAuthorityPolicy) (map[string]string, error) {
	path := filepath.Clean(policy.TrustStore)
	if err := authorityTrustStoreProtected(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Size() > 64*1024 {
		return nil, fmt.Errorf("external authority trust store is missing or too large")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var store externalAuthorityTrustStore
	if err := DecodeJSON("load external authority trust store", path, raw, &store); err != nil || store.SchemaVersion != 1 || len(store.Issuers) == 0 {
		return nil, fmt.Errorf("external authority trust store is malformed")
	}
	for issuer, encoded := range store.Issuers {
		key, err := base64.StdEncoding.DecodeString(encoded)
		if strings.TrimSpace(issuer) == "" || err != nil || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("external authority trust store contains an invalid issuer")
		}
	}
	return store.Issuers, nil
}

func verifyAuthorityBoundary(repo string, policy *ExternalAuthorityPolicy) (string, string) {
	if normalizedAuthorityMode(policy) != "credential-enforced" {
		return AuthorityHookGuarded, "Boatstack hooks guard known irreversible operations; cloud authority is not externally attested."
	}
	trustedIssuers, trustErr := loadExternalTrustStore(policy)
	if trustErr != nil {
		return AuthorityHookGuarded, "external authority trust store is not protected or valid"
	}
	path := strings.TrimSpace(os.Getenv(AuthorityReceiptEnv))
	if path == "" || !filepath.IsAbs(path) {
		return AuthorityHookGuarded, "credential-enforced mode requires an absolute external authority receipt path"
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 64*1024 {
		return AuthorityHookGuarded, "external authority receipt is missing, unsafe, or too large"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return AuthorityHookGuarded, "external authority receipt could not be read"
	}
	var receipt AuthorityBoundaryReceipt
	if err := DecodeJSON("load external authority receipt", path, raw, &receipt); err != nil {
		return AuthorityHookGuarded, "external authority receipt is malformed"
	}
	if receipt.SchemaVersion != 1 || receipt.AuthorityClass != AuthorityClassRepositoryOnly || receipt.CloudControlPlaneAuthority {
		return AuthorityHookGuarded, "external authority receipt does not attest repository-only authority"
	}
	switch receipt.EnforcedBy {
	case "service-iam", "credential-broker", "isolated-host":
	default:
		return AuthorityHookGuarded, "external authority receipt names an unsupported enforcement boundary"
	}
	context, err := ResolveAuthorityContext(repo)
	if err != nil || receipt.RepositoryFingerprint != context.RepositoryFingerprint || receipt.WorktreeFingerprint != context.WorktreeFingerprint {
		return AuthorityHookGuarded, "external authority receipt is bound to a different repository or worktree"
	}
	if receipt.HostSession == "" || receipt.HostSession != strings.TrimSpace(os.Getenv(AuthorityHostSessionEnv)) {
		return AuthorityHookGuarded, "external authority receipt is bound to a different host session"
	}
	if receipt.PrincipalFingerprint == "" || receipt.PrincipalFingerprint != strings.TrimSpace(os.Getenv(AuthorityPrincipalEnv)) {
		return AuthorityHookGuarded, "external authority receipt is bound to a different principal"
	}
	issued, issuedErr := time.Parse(time.RFC3339, receipt.IssuedAt)
	expires, expiresErr := time.Parse(time.RFC3339, receipt.ExpiresAt)
	now := authorityNow().UTC()
	if issuedErr != nil || expiresErr != nil || expires.Before(now) || issued.After(now.Add(time.Minute)) || !expires.After(issued) || expires.Sub(issued) > maxAuthorityReceiptLifetime {
		return AuthorityHookGuarded, "external authority receipt is stale or has an invalid lifetime"
	}
	encodedKey, ok := trustedIssuers[receipt.Issuer]
	if !ok {
		return AuthorityHookGuarded, "external authority receipt issuer is not trusted"
	}
	publicKey, keyErr := base64.StdEncoding.DecodeString(encodedKey)
	signature, signatureErr := base64.StdEncoding.DecodeString(receipt.Signature)
	payload, payloadErr := AuthorityReceiptSigningBytes(receipt)
	if keyErr != nil || signatureErr != nil || payloadErr != nil || len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(publicKey), payload, signature) {
		return AuthorityHookGuarded, "external authority receipt signature is invalid"
	}
	return AuthorityCredentialEnforced, "An external authority attests repository-only credentials for this repository, worktree, host session, and principal."
}
