package boatstack

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	operationSchemaVersion = 1
	operationLeaseDuration = 15 * time.Minute
	operationRetention     = 7 * 24 * time.Hour
)

type OperationState string

const (
	OperationPrepared          OperationState = "PREPARED"
	OperationAuthorized        OperationState = "AUTHORIZED"
	OperationExecuting         OperationState = "EXECUTING"
	OperationReconcileRequired OperationState = "RECONCILE_REQUIRED"
	OperationRetryable         OperationState = "RETRYABLE"
	OperationSucceeded         OperationState = "SUCCEEDED"
	OperationFailedFinal       OperationState = "FAILED_FINAL"
)

type OperationScope struct {
	Feature    string `json:"feature,omitempty"`
	Slice      string `json:"slice,omitempty"`
	Worktree   string `json:"worktree,omitempty"`
	HeadBranch string `json:"head_branch,omitempty"`
}

type OperationLease struct {
	TokenSHA256 string `json:"token_sha256"`
	AttemptKey  string `json:"attempt_key"`
	Tool        string `json:"tool,omitempty"`
	Target      string `json:"target"`
	ExpiresAt   string `json:"expires_at"`
}

type OperationObservation struct {
	Status   string `json:"status,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Evidence string `json:"evidence,omitempty"`
	At       string `json:"at,omitempty"`
}

// OperationReceipt is deliberately secret-free. It stores fingerprints and
// bounded observations, never complete commands, tool arguments, or responses.
type OperationReceipt struct {
	SchemaVersion            int                  `json:"schema_version"`
	OperationID              string               `json:"operation_id"`
	Kind                     string               `json:"kind"`
	Scope                    OperationScope       `json:"scope"`
	Target                   string               `json:"target"`
	PackageFingerprint       string               `json:"package_fingerprint"`
	AuthorizationFingerprint string               `json:"authorization_fingerprint,omitempty"`
	State                    OperationState       `json:"state"`
	RetryClass               string               `json:"retry_class"`
	Attempt                  int                  `json:"attempt"`
	MaxAttempts              int                  `json:"max_attempts"`
	ExpectedPostcondition    string               `json:"expected_postcondition"`
	Lease                    *OperationLease      `json:"lease,omitempty"`
	Observation              OperationObservation `json:"observation,omitempty"`
	CreatedAt                string               `json:"created_at"`
	UpdatedAt                string               `json:"updated_at"`
}

type OperationPrepareOptions struct {
	Repo                     string
	Kind                     string
	Scope                    OperationScope
	Target                   string
	PackageFingerprint       string
	AuthorizationFingerprint string
	RetryClass               string
	MaxAttempts              int
	ExpectedPostcondition    string
}

type OperationBeginResult struct {
	Receipt    OperationReceipt
	LeaseToken string
}

type OperationStatusResult struct {
	SchemaVersion          int               `json:"schema_version"`
	VerificationStatus     string            `json:"verification_status"`
	Operation              *OperationReceipt `json:"operation,omitempty"`
	Blocker                string            `json:"blocker,omitempty"`
	ReconciliationRequired bool              `json:"reconciliation_required"`
	NextOperation          string            `json:"next_operation"`
}

var operationNow = time.Now

var ErrOperationInFlight = errors.New("identical operation is already executing")

func operationTimestamp() string {
	return operationNow().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func operationDirectory(repo string) (string, error) {
	common, err := gitCommonDir(repo)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "boatstack", "operations", "v1"), nil
}

func operationPath(repo, operationID string) (string, error) {
	id, err := safeCacheSegment(operationID, "operation id")
	if err != nil {
		return "", err
	}
	directory, err := operationDirectory(repo)
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, id+".json")
	common, err := gitCommonDir(repo)
	if err != nil {
		return "", err
	}
	if err := rejectSymlinkComponents(common, path); err != nil {
		return "", err
	}
	return path, nil
}

func operationID(kind, target, fingerprint string) string {
	return SHA256Bytes([]byte(strings.TrimSpace(kind) + "\x00" + strings.TrimSpace(target) + "\x00" + strings.TrimSpace(fingerprint)))[:24]
}

func validOperationState(state OperationState) bool {
	switch state {
	case OperationPrepared, OperationAuthorized, OperationExecuting, OperationReconcileRequired, OperationRetryable, OperationSucceeded, OperationFailedFinal:
		return true
	default:
		return false
	}
}

func validRetryClass(value string) bool {
	switch value {
	case "READ_ONLY", "ATOMIC_LOCAL", "IDEMPOTENT_EXTERNAL", "RECONCILE_FIRST":
		return true
	default:
		return false
	}
}

func validateOperation(receipt OperationReceipt) error {
	if receipt.SchemaVersion != operationSchemaVersion || receipt.OperationID == "" || receipt.Kind == "" || receipt.Target == "" || receipt.PackageFingerprint == "" {
		return fmt.Errorf("operation receipt identity is invalid")
	}
	if !validOperationState(receipt.State) || !validRetryClass(receipt.RetryClass) || receipt.MaxAttempts < 1 || receipt.Attempt < 0 || receipt.Attempt > receipt.MaxAttempts {
		return fmt.Errorf("operation receipt state is invalid")
	}
	if receipt.State == OperationExecuting && receipt.Lease == nil {
		return fmt.Errorf("executing operation has no lease")
	}
	return nil
}

func loadOperation(repo, id string) (OperationReceipt, error) {
	path, err := operationPath(repo, id)
	if err != nil {
		return OperationReceipt{}, err
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return OperationReceipt{}, err
	}
	var receipt OperationReceipt
	if err := DecodeJSON("load operation receipt", path, value, &receipt); err != nil {
		return OperationReceipt{}, err
	}
	if err := validateOperation(receipt); err != nil {
		return OperationReceipt{}, err
	}
	return receipt, nil
}

func saveOperation(repo string, receipt OperationReceipt) error {
	if err := validateOperation(receipt); err != nil {
		return err
	}
	path, err := operationPath(repo, receipt.OperationID)
	if err != nil {
		return err
	}
	value, err := MarshalJSON(receipt)
	if err != nil {
		return err
	}
	return atomicWriteMode(path, value, 0o600)
}

func withOperationLock(repo, id string, apply func() error) error {
	path, err := operationPath(repo, id)
	if err != nil {
		return err
	}
	lock := strings.TrimSuffix(path, ".json") + ".lock"
	common, err := gitCommonDir(repo)
	if err != nil {
		return err
	}
	if err := rejectSymlinkComponents(common, lock); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lock), 0o700); err != nil {
		return err
	}
	for attempt := 0; attempt < 100; attempt++ {
		file, openErr := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if openErr == nil {
			_, _ = fmt.Fprintf(file, "%d %s\n", os.Getpid(), operationTimestamp())
			_ = file.Close()
			defer os.Remove(lock)
			return apply()
		}
		if !os.IsExist(openErr) {
			return openErr
		}
		if info, statErr := os.Stat(lock); statErr == nil && operationNow().Sub(info.ModTime()) > time.Minute {
			_ = os.Remove(lock)
			continue
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("operation %s is busy", id)
}

func PrepareOperation(options OperationPrepareOptions) (OperationReceipt, error) {
	repo, err := ResolveRepository(options.Repo)
	if err != nil {
		return OperationReceipt{}, err
	}
	// Retention is best-effort and never prevents a new supervised operation.
	_ = compactOperations(repo)
	kind := strings.TrimSpace(options.Kind)
	target := strings.TrimSpace(options.Target)
	fingerprint := strings.TrimSpace(options.PackageFingerprint)
	if kind == "" || target == "" || fingerprint == "" || strings.TrimSpace(options.ExpectedPostcondition) == "" {
		return OperationReceipt{}, fmt.Errorf("operation requires kind, target, package fingerprint, and expected postcondition")
	}
	retryClass := strings.ToUpper(strings.TrimSpace(options.RetryClass))
	if !validRetryClass(retryClass) {
		return OperationReceipt{}, fmt.Errorf("unsupported operation retry class %q", options.RetryClass)
	}
	maximum := options.MaxAttempts
	if maximum == 0 {
		maximum = 3
	}
	if maximum < 1 || maximum > 10 {
		return OperationReceipt{}, fmt.Errorf("operation max attempts must be between 1 and 10")
	}
	id := operationID(kind, target, fingerprint)
	var result OperationReceipt
	err = withOperationLock(repo, id, func() error {
		existing, loadErr := loadOperation(repo, id)
		if loadErr == nil {
			if existing.Kind != kind || existing.Target != target || existing.PackageFingerprint != fingerprint ||
				existing.RetryClass != retryClass || existing.MaxAttempts != maximum ||
				existing.ExpectedPostcondition != strings.TrimSpace(options.ExpectedPostcondition) || existing.Scope != options.Scope {
				return fmt.Errorf("existing operation identity does not match the prepared package")
			}
			authorization := strings.TrimSpace(options.AuthorizationFingerprint)
			if authorization != "" && existing.AuthorizationFingerprint != "" && existing.AuthorizationFingerprint != authorization {
				return fmt.Errorf("operation authorization fingerprint changed after preparation")
			}
			if authorization != "" && existing.State == OperationPrepared {
				existing.AuthorizationFingerprint = authorization
				existing.State = OperationAuthorized
				existing.UpdatedAt = operationTimestamp()
				if err := saveOperation(repo, existing); err != nil {
					return err
				}
			}
			result = existing
			return nil
		}
		if !os.IsNotExist(loadErr) {
			return loadErr
		}
		now := operationTimestamp()
		state := OperationPrepared
		if strings.TrimSpace(options.AuthorizationFingerprint) != "" {
			state = OperationAuthorized
		}
		result = OperationReceipt{
			SchemaVersion: operationSchemaVersion, OperationID: id, Kind: kind, Scope: options.Scope,
			Target: target, PackageFingerprint: fingerprint, AuthorizationFingerprint: strings.TrimSpace(options.AuthorizationFingerprint),
			State: state, RetryClass: retryClass, MaxAttempts: maximum,
			ExpectedPostcondition: strings.TrimSpace(options.ExpectedPostcondition), CreatedAt: now, UpdatedAt: now,
		}
		return saveOperation(repo, result)
	})
	return result, err
}

func AuthorizeOperation(repoPath, id, packageFingerprint, authorizationFingerprint string) (OperationReceipt, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return OperationReceipt{}, err
	}
	var result OperationReceipt
	err = withOperationLock(repo, id, func() error {
		receipt, loadErr := loadOperation(repo, id)
		if loadErr != nil {
			return loadErr
		}
		if receipt.PackageFingerprint != strings.TrimSpace(packageFingerprint) || strings.TrimSpace(authorizationFingerprint) == "" {
			return fmt.Errorf("operation authorization does not match the prepared package")
		}
		if receipt.AuthorizationFingerprint != "" && receipt.AuthorizationFingerprint != strings.TrimSpace(authorizationFingerprint) {
			return fmt.Errorf("operation authorization fingerprint changed after preparation")
		}
		if receipt.State != OperationPrepared && receipt.State != OperationAuthorized {
			return fmt.Errorf("operation %s cannot be authorized from %s", id, receipt.State)
		}
		receipt.AuthorizationFingerprint = strings.TrimSpace(authorizationFingerprint)
		receipt.State = OperationAuthorized
		receipt.UpdatedAt = operationTimestamp()
		result = receipt
		return saveOperation(repo, receipt)
	})
	return result, err
}

func randomLeaseToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func BeginOperation(repoPath, id, attemptKey, tool string) (OperationBeginResult, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return OperationBeginResult{}, err
	}
	attemptKey = strings.TrimSpace(attemptKey)
	if attemptKey == "" {
		return OperationBeginResult{}, fmt.Errorf("operation attempt key is required")
	}
	var result OperationBeginResult
	err = withOperationLock(repo, id, func() error {
		receipt, loadErr := loadOperation(repo, id)
		if loadErr != nil {
			return loadErr
		}
		if receipt.State == OperationSucceeded {
			result.Receipt = receipt
			return nil
		}
		if receipt.State == OperationExecuting {
			expires, parseErr := time.Parse(time.RFC3339, receipt.Lease.ExpiresAt)
			if parseErr != nil || !operationNow().Before(expires) {
				receipt.State = OperationReconcileRequired
				receipt.Lease = nil
				receipt.Observation = OperationObservation{Status: "UNKNOWN", Detail: "execution lease expired before completion was observed", At: operationTimestamp()}
				receipt.UpdatedAt = operationTimestamp()
				if err := saveOperation(repo, receipt); err != nil {
					return err
				}
				result.Receipt = receipt
				return fmt.Errorf("operation completion is unknown; reconcile before retry")
			}
			result.Receipt = receipt
			return ErrOperationInFlight
		}
		if receipt.State != OperationAuthorized && receipt.State != OperationRetryable {
			return fmt.Errorf("operation %s cannot begin from %s", id, receipt.State)
		}
		if receipt.AuthorizationFingerprint == "" {
			return fmt.Errorf("operation %s has no fingerprinted authorization", id)
		}
		if receipt.Attempt >= receipt.MaxAttempts {
			receipt.State = OperationFailedFinal
			receipt.Observation = OperationObservation{Status: "FAILED", Detail: "persistent retry budget exhausted", At: operationTimestamp()}
			receipt.UpdatedAt = operationTimestamp()
			_ = saveOperation(repo, receipt)
			result.Receipt = receipt
			return fmt.Errorf("operation retry budget is exhausted")
		}
		token, tokenErr := randomLeaseToken()
		if tokenErr != nil {
			return tokenErr
		}
		receipt.Attempt++
		receipt.State = OperationExecuting
		receipt.Lease = &OperationLease{
			TokenSHA256: SHA256Bytes([]byte(token)), AttemptKey: attemptKey, Tool: strings.TrimSpace(tool), Target: receipt.Target,
			ExpiresAt: operationNow().UTC().Add(operationLeaseDuration).Truncate(time.Second).Format(time.RFC3339),
		}
		receipt.Observation = OperationObservation{}
		receipt.UpdatedAt = operationTimestamp()
		if err := saveOperation(repo, receipt); err != nil {
			return err
		}
		result = OperationBeginResult{Receipt: receipt, LeaseToken: token}
		return nil
	})
	return result, err
}

func completeOperation(repoPath, id, leaseToken, attemptKey, outcome, detail, evidence string, trustedAttempt bool) (OperationReceipt, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return OperationReceipt{}, err
	}
	var result OperationReceipt
	err = withOperationLock(repo, id, func() error {
		receipt, loadErr := loadOperation(repo, id)
		if loadErr != nil {
			return loadErr
		}
		if receipt.State == OperationSucceeded || receipt.State == OperationFailedFinal {
			result = receipt
			return nil
		}
		if receipt.State != OperationExecuting || receipt.Lease == nil {
			return fmt.Errorf("operation %s has no executing attempt to complete", id)
		}
		if trustedAttempt {
			if strings.TrimSpace(attemptKey) == "" || receipt.Lease.AttemptKey != strings.TrimSpace(attemptKey) {
				return fmt.Errorf("operation completion does not match the active attempt")
			}
		} else if SHA256Bytes([]byte(strings.TrimSpace(leaseToken))) != receipt.Lease.TokenSHA256 {
			return fmt.Errorf("operation lease is invalid or replayed")
		}
		outcome = strings.ToUpper(strings.TrimSpace(outcome))
		switch outcome {
		case "SUCCEEDED":
			receipt.State = OperationSucceeded
		case "RETRYABLE":
			if receipt.RetryClass == "RECONCILE_FIRST" {
				return fmt.Errorf("reconcile-first operation cannot be marked retryable without reconciliation")
			}
			if receipt.Attempt >= receipt.MaxAttempts {
				receipt.State = OperationFailedFinal
			} else {
				receipt.State = OperationRetryable
			}
		case "UNKNOWN":
			receipt.State = OperationReconcileRequired
		case "FAILED_FINAL":
			receipt.State = OperationFailedFinal
		default:
			return fmt.Errorf("unsupported operation outcome %q", outcome)
		}
		receipt.Lease = nil
		receipt.Observation = OperationObservation{Status: outcome, Detail: boundedObservation(detail), Evidence: boundedObservation(evidence), At: operationTimestamp()}
		receipt.UpdatedAt = operationTimestamp()
		result = receipt
		return saveOperation(repo, receipt)
	})
	return result, err
}

func boundedObservation(value string) string {
	value = strings.TrimSpace(value)
	secretAssignment := regexp.MustCompile(`(?i)\b(token|password|secret|authorization|api[_-]?key)\s*[:=]\s*[^\s,;]+`)
	value = secretAssignment.ReplaceAllString(value, "$1=<redacted>")
	bearer := regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
	value = bearer.ReplaceAllString(value, "Bearer <redacted>")
	if len(value) > 512 {
		return value[:512]
	}
	return value
}

func CompleteOperation(repo, id, leaseToken, outcome, detail, evidence string) (OperationReceipt, error) {
	return completeOperation(repo, id, leaseToken, "", outcome, detail, evidence, false)
}

func CompleteOperationAttempt(repo, id, attemptKey, outcome, detail, evidence string) (OperationReceipt, error) {
	return completeOperation(repo, id, "", attemptKey, outcome, detail, evidence, true)
}

func RecordOperationReconciliation(repoPath, id, result, detail, evidence string) (OperationReceipt, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return OperationReceipt{}, err
	}
	var output OperationReceipt
	err = withOperationLock(repo, id, func() error {
		receipt, loadErr := loadOperation(repo, id)
		if loadErr != nil {
			return loadErr
		}
		if receipt.State != OperationReconcileRequired {
			return fmt.Errorf("operation %s does not require reconciliation", id)
		}
		switch strings.ToUpper(strings.TrimSpace(result)) {
		case "OBSERVED_SUCCEEDED":
			receipt.State = OperationSucceeded
		case "OBSERVED_ABSENT", "OBSERVED_PARTIAL":
			if receipt.Attempt >= receipt.MaxAttempts {
				receipt.State = OperationFailedFinal
			} else {
				receipt.State = OperationRetryable
			}
		case "STILL_UNKNOWN":
			receipt.State = OperationReconcileRequired
		default:
			return fmt.Errorf("unsupported reconciliation result %q", result)
		}
		receipt.Observation = OperationObservation{Status: strings.ToUpper(strings.TrimSpace(result)), Detail: boundedObservation(detail), Evidence: boundedObservation(evidence), At: operationTimestamp()}
		receipt.UpdatedAt = operationTimestamp()
		output = receipt
		return saveOperation(repo, receipt)
	})
	return output, err
}

func operationReceipts(repo string) ([]OperationReceipt, error) {
	directory, err := operationDirectory(repo)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	values := []OperationReceipt{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		receipt, loadErr := loadOperation(repo, strings.TrimSuffix(entry.Name(), ".json"))
		if loadErr != nil {
			return nil, loadErr
		}
		values = append(values, receipt)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].OperationID < values[j].OperationID })
	return values, nil
}

func refreshExpiredOperation(repo, id string) (OperationReceipt, error) {
	var result OperationReceipt
	err := withOperationLock(repo, id, func() error {
		receipt, err := loadOperation(repo, id)
		if err != nil {
			return err
		}
		if receipt.State == OperationExecuting && receipt.Lease != nil {
			expires, parseErr := time.Parse(time.RFC3339, receipt.Lease.ExpiresAt)
			if parseErr != nil || !operationNow().Before(expires) {
				receipt.State = OperationReconcileRequired
				receipt.Lease = nil
				receipt.Observation = OperationObservation{Status: "UNKNOWN", Detail: "execution lease expired before completion was observed", At: operationTimestamp()}
				receipt.UpdatedAt = operationTimestamp()
				if err := saveOperation(repo, receipt); err != nil {
					return err
				}
			}
		}
		result = receipt
		return nil
	})
	return result, err
}

func ResolveOperationStatus(repoPath, id string) (OperationStatusResult, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return OperationStatusResult{}, err
	}
	if strings.TrimSpace(id) != "" {
		receipt, loadErr := refreshExpiredOperation(repo, strings.TrimSpace(id))
		if loadErr != nil {
			return OperationStatusResult{}, loadErr
		}
		return operationStatusFor(receipt), nil
	}
	branch := strings.TrimSpace(gitOutput(repo, "branch", "--show-current"))
	receipts, err := operationReceipts(repo)
	if err != nil {
		return OperationStatusResult{}, err
	}
	active := []OperationReceipt{}
	for _, receipt := range receipts {
		if receipt.State == OperationExecuting {
			if refreshed, refreshErr := refreshExpiredOperation(repo, receipt.OperationID); refreshErr == nil {
				receipt = refreshed
			}
		}
		if receipt.State == OperationSucceeded || receipt.State == OperationFailedFinal {
			continue
		}
		if receipt.Scope.HeadBranch == "" || receipt.Scope.HeadBranch == branch {
			active = append(active, receipt)
		}
	}
	if len(active) == 0 {
		return OperationStatusResult{SchemaVersion: operationSchemaVersion, VerificationStatus: "VERIFIED", NextOperation: "none"}, nil
	}
	if len(active) > 1 {
		return OperationStatusResult{SchemaVersion: operationSchemaVersion, VerificationStatus: "AMBIGUOUS", Blocker: "more than one unfinished operation matches the current branch", NextOperation: "specify_operation"}, nil
	}
	return operationStatusFor(active[0]), nil
}

func operationStatusFor(receipt OperationReceipt) OperationStatusResult {
	next := "none"
	blocker := ""
	switch receipt.State {
	case OperationPrepared:
		next = "authorize"
	case OperationAuthorized, OperationRetryable:
		next = "execute"
	case OperationExecuting:
		next = "wait"
		blocker = "an authorized attempt is already executing"
	case OperationReconcileRequired:
		next = "reconcile"
		blocker = "completion was not observed"
	case OperationFailedFinal:
		next = "manual_recovery"
		blocker = receipt.Observation.Detail
	}
	copy := receipt
	return OperationStatusResult{
		SchemaVersion: operationSchemaVersion, VerificationStatus: "VERIFIED", Operation: &copy,
		Blocker: blocker, ReconciliationRequired: receipt.State == OperationReconcileRequired, NextOperation: next,
	}
}

func compactOperations(repo string) error {
	receipts, err := operationReceipts(repo)
	if err != nil {
		return err
	}
	cutoff := operationNow().Add(-operationRetention)
	for _, receipt := range receipts {
		if receipt.State != OperationSucceeded && receipt.State != OperationFailedFinal {
			continue
		}
		updated, parseErr := time.Parse(time.RFC3339, receipt.UpdatedAt)
		if parseErr != nil || updated.After(cutoff) || receipt.Observation.Detail == "" {
			continue
		}
		receipt.Observation.Detail = "terminal receipt compacted"
		receipt.Observation.Evidence = ""
		if err := saveOperation(repo, receipt); err != nil {
			return err
		}
	}
	return nil
}
