package boatstack

// Transactional mutation boundary.
//
// Coding agents propose changes; this deterministic boundary decides what
// becomes managed-artifact state. A MutationSet is validated in scratch,
// promoted through one atomic all-or-nothing write, and recorded as a receipt
// that both permits the next state and makes the change reversible without
// fresh model reasoning.
//
// The boundary is the bounded actuator required by nonblocking supervisory
// control: whenever the guard removes the raw ability to write, this API is the
// sanctioned way to reach the next valid state. A rejected precondition (stale
// base, outdated authority) never persists an identity, so a proposal recomputed
// against current state can still succeed — rejection never deadlocks.
//
// It reuses Boatstack's existing integrity primitives: SHA256* for content
// identity, atomicWriteMode (temp+fsync+rename) for promotion, gitCommonDir for
// the receipt store, rejectSymlinkComponents/resolveRepositoryRelativePath for
// path safety, and the operationID scheme + an O_EXCL lock mirroring
// withOperationLock for idempotency and mutual exclusion.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	mutationSchemaVersion = 1
	MutationProtocol      = "operator.mutation.v1"
)

// Sentinel errors let callers (and tests) distinguish deterministic refusals
// from genuine I/O faults. Every refusal below leaves accepted state unchanged.
var (
	ErrMutationInvalidCandidate  = errors.New("mutation candidate failed validation before promotion")
	ErrMutationStaleBase         = errors.New("mutation rejected: a base artifact changed since it was read")
	ErrMutationOutdatedAuthority = errors.New("mutation rejected: supervisor authority changed since it was authorized")
	ErrMutationVerificationFailed = errors.New("mutation rolled back: post-write verification failed")
	ErrMutationScope             = errors.New("mutation operation falls outside its declared scope")
	ErrMutationConflict          = errors.New("mutation cannot be undone: the artifact diverged from its recorded post-image")
)

// MutationOperation is a single file change within a transaction. Candidate holds
// the exact bytes to promote; the boundary asserts the on-disk image hashes to
// the same value after promotion. When Absent is true the operation deletes the
// path instead (Candidate is ignored), which makes the inverse of a create
// expressible as an ordinary MutationSet — the boundary is closed under
// inversion.
type MutationOperation struct {
	Path      string      // repo-relative, slash-separated
	Candidate []byte      // exact bytes to promote (ignored when Absent)
	Mode      fs.FileMode // file mode; 0 means 0o644
	Absent    bool        // when true, the post-image is the file's absence (delete)
}

// MutationAuthority binds a mutation to the supervisor state that authorized it.
// Expected is the token the proposal was authorized under; Observed is the token
// recomputed from current supervisor state at apply time. A mismatch means the
// authority has moved on and the mutation must be re-derived.
type MutationAuthority struct {
	Expected string
	Observed string
}

// MutationSet is a proposed atomic change to managed artifacts. It is an
// in-process request; the PreCheck/PostCheck hooks are not persisted.
type MutationSet struct {
	Protocol   string
	Kind       string
	Scope      []string          // allowed repo-relative paths; every operation path must be listed
	Base       map[string]string // repo-relative path -> expected pre-image sha256 ("" or absent = must not exist)
	Authority  MutationAuthority
	Operations []MutationOperation

	// PreCheck validates the candidate bytes before anything is promoted. A
	// non-zero error means the candidate is invalid and accepted state is left
	// untouched (ErrMutationInvalidCandidate).
	PreCheck func(candidate map[string][]byte) error
	// PostCheck validates the promoted artifacts on disk. A non-zero error
	// triggers automatic rollback to the exact pre-image
	// (ErrMutationVerificationFailed).
	PostCheck func() error
}

// MutationFileChange records the before/after identity of one promoted path and
// carries the inverse image needed to reverse the change deterministically.
type MutationFileChange struct {
	Path          string      `json:"path"`
	ExistedBefore bool        `json:"existed_before"`
	BeforeSHA256  string      `json:"before_sha256,omitempty"`
	AfterSHA256   string      `json:"after_sha256"`
	Mode          fs.FileMode `json:"mode"`
	BeforeBase64  string      `json:"before_base64,omitempty"` // inverse image; empty when the file was absent
}

// MutationReceipt is the durable record of an applied mutation. It is the single
// source of truth for idempotent replay and for UndoMutation.
type MutationReceipt struct {
	SchemaVersion int                  `json:"schema_version"`
	MutationID    string               `json:"mutation_id"`
	Protocol      string               `json:"protocol"`
	Kind          string               `json:"kind"`
	Status        string               `json:"status"` // APPLIED | ROLLED_BACK | REJECTED | UNDONE
	Reason        string               `json:"reason,omitempty"`
	Scope         []string             `json:"scope"`
	Changes       []MutationFileChange `json:"changes"`
	Authority     string               `json:"authority_sha256,omitempty"`
	RecordedAt    string               `json:"recorded_at"`
}

func mutationDirectory(repo string) (string, error) {
	common, err := gitCommonDir(repo)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "boatstack", "mutations", "v1"), nil
}

func mutationPath(repo, id string) (string, error) {
	segment, err := safeCacheSegment(id, "mutation id")
	if err != nil {
		return "", err
	}
	directory, err := mutationDirectory(repo)
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, segment+".json")
	common, err := gitCommonDir(repo)
	if err != nil {
		return "", err
	}
	if err := rejectSymlinkComponents(common, path); err != nil {
		return "", err
	}
	return path, nil
}

// withMutationLock mirrors withOperationLock: an O_EXCL lockfile beside the
// receipt gives mutual exclusion across processes, with stale-lock reclamation.
func withMutationLock(repo, id string, apply func() error) error {
	path, err := mutationPath(repo, id)
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
	return fmt.Errorf("mutation %s is busy", id)
}

func loadMutationReceipt(repo, id string) (MutationReceipt, bool, error) {
	path, err := mutationPath(repo, id)
	if err != nil {
		return MutationReceipt{}, false, err
	}
	value, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return MutationReceipt{}, false, nil
	}
	if err != nil {
		return MutationReceipt{}, false, err
	}
	var receipt MutationReceipt
	if err := DecodeJSON("load mutation receipt", path, value, &receipt); err != nil {
		return MutationReceipt{}, false, err
	}
	return receipt, true, nil
}

func saveMutationReceipt(repo string, receipt MutationReceipt) error {
	path, err := mutationPath(repo, receipt.MutationID)
	if err != nil {
		return err
	}
	value, err := MarshalJSON(receipt)
	if err != nil {
		return err
	}
	return atomicWriteMode(path, value, 0o600)
}

func mutationMode(mode fs.FileMode) fs.FileMode {
	if mode == 0 {
		return 0o644
	}
	return mode
}

// currentImage returns the on-disk sha256 of a resolved path, or ("", false)
// when the file is absent. A directory or unreadable file is a hard error.
func currentImage(native string) (string, bool, error) {
	value, err := os.ReadFile(native)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return SHA256Bytes(value), true, nil
}

// mutationIdentity derives the stable id for a MutationSet. It covers what is
// being written (kind + each path and candidate hash) and the declared base, so
// an identical proposal replays and a different proposal is a distinct mutation.
// It deliberately excludes the transient Authority.Observed value so a rejected
// authority check does not fork the identity of the corrected retry.
func mutationIdentity(m MutationSet, ops []resolvedOperation) string {
	parts := make([]string, 0, len(ops))
	for _, op := range ops {
		parts = append(parts, op.rel+"\x1f"+op.candidateHash+"\x1f"+m.Base[op.rel])
	}
	sort.Strings(parts)
	fingerprint := SHA256Bytes([]byte(strings.Join(parts, "\x1e")))
	target := SHA256Bytes([]byte(strings.Join(sortedScope(m.Scope), "\x1e")))
	return operationID("mutation\x00"+strings.TrimSpace(m.Kind), target, fingerprint)
}

func sortedScope(scope []string) []string {
	out := append([]string(nil), scope...)
	sort.Strings(out)
	return out
}

type resolvedOperation struct {
	rel           string // repo-relative, slash form
	native        string // absolute filesystem path
	candidate     []byte
	candidateHash string // "" denotes an absent (deleted) post-image
	mode          fs.FileMode
	absent        bool
}

func (m MutationSet) resolve(repo string) ([]resolvedOperation, error) {
	if strings.TrimSpace(m.Protocol) != MutationProtocol {
		return nil, fmt.Errorf("mutation protocol must be %s", MutationProtocol)
	}
	if strings.TrimSpace(m.Kind) == "" {
		return nil, fmt.Errorf("mutation requires a kind")
	}
	if len(m.Operations) == 0 {
		return nil, fmt.Errorf("mutation requires at least one operation")
	}
	scope := map[string]bool{}
	for _, path := range m.Scope {
		scope[filepath.ToSlash(strings.TrimSpace(path))] = true
	}
	resolved := make([]resolvedOperation, 0, len(m.Operations))
	seen := map[string]bool{}
	for _, op := range m.Operations {
		rel := filepath.ToSlash(strings.TrimSpace(op.Path))
		if rel == "" {
			return nil, fmt.Errorf("mutation operation path is empty")
		}
		if !scope[rel] {
			return nil, fmt.Errorf("%w: %s", ErrMutationScope, rel)
		}
		if seen[rel] {
			return nil, fmt.Errorf("mutation names %s more than once", rel)
		}
		seen[rel] = true
		native, err := resolveRepositoryRelativePath(repo, rel)
		if err != nil {
			return nil, err
		}
		if err := rejectSymlinkComponents(repo, native); err != nil {
			return nil, err
		}
		if op.Absent {
			// An absent post-image has no candidate bytes; its hash sentinel is ""
			// so identity and receipts distinguish it from any real content.
			resolved = append(resolved, resolvedOperation{
				rel: rel, native: native, mode: mutationMode(op.Mode), absent: true,
			})
			continue
		}
		resolved = append(resolved, resolvedOperation{
			rel: rel, native: native, candidate: op.Candidate,
			candidateHash: SHA256Bytes(op.Candidate), mode: mutationMode(op.Mode),
		})
	}
	return resolved, nil
}

// ApplyMutation validates and promotes a MutationSet as one atomic transaction.
// On success it returns an APPLIED receipt; on a deterministic refusal it returns
// a receipt whose Status explains the refusal along with the matching sentinel
// error, having left every artifact untouched.
func ApplyMutation(repoPath string, m MutationSet) (MutationReceipt, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return MutationReceipt{}, err
	}
	ops, err := m.resolve(repo)
	if err != nil {
		return MutationReceipt{}, err
	}
	id := mutationIdentity(m, ops)
	authorityHash := SHA256Bytes([]byte(m.Authority.Expected))

	var result MutationReceipt
	err = withMutationLock(repo, id, func() error {
		// Idempotency: an identical proposal whose post-image is already on disk
		// replays the recorded receipt without writing again.
		if existing, ok, loadErr := loadMutationReceipt(repo, id); loadErr != nil {
			return loadErr
		} else if ok && existing.Status == "APPLIED" && receiptStillApplied(repo, existing) {
			result = existing
			return nil
		}

		// Preconditions run before any write and before any durable identity, so a
		// refusal cannot deadlock a corrected retry.
		if strings.TrimSpace(m.Authority.Expected) != strings.TrimSpace(m.Authority.Observed) {
			result = MutationReceipt{
				SchemaVersion: mutationSchemaVersion, MutationID: id, Protocol: MutationProtocol,
				Kind: m.Kind, Status: "REJECTED", Reason: "outdated supervisor authority",
				Scope: sortedScope(m.Scope), RecordedAt: operationTimestamp(),
			}
			return ErrMutationOutdatedAuthority
		}
		for _, op := range ops {
			expected := strings.TrimSpace(m.Base[op.rel])
			current, exists, imgErr := currentImage(op.native)
			if imgErr != nil {
				return imgErr
			}
			observed := ""
			if exists {
				observed = current
			}
			if observed != expected {
				result = MutationReceipt{
					SchemaVersion: mutationSchemaVersion, MutationID: id, Protocol: MutationProtocol,
					Kind: m.Kind, Status: "REJECTED", Reason: "stale base artifact: " + op.rel,
					Scope: sortedScope(m.Scope), RecordedAt: operationTimestamp(),
				}
				return ErrMutationStaleBase
			}
		}

		// Validate the candidate in scratch (never on the accepted tree).
		if m.PreCheck != nil {
			candidate := map[string][]byte{}
			for _, op := range ops {
				candidate[op.rel] = op.candidate
			}
			if checkErr := m.PreCheck(candidate); checkErr != nil {
				result = MutationReceipt{
					SchemaVersion: mutationSchemaVersion, MutationID: id, Protocol: MutationProtocol,
					Kind: m.Kind, Status: "REJECTED", Reason: "invalid candidate: " + checkErr.Error(),
					Scope: sortedScope(m.Scope), RecordedAt: operationTimestamp(),
				}
				return fmt.Errorf("%w: %v", ErrMutationInvalidCandidate, checkErr)
			}
		}

		// Capture the inverse image, then promote every file atomically.
		changes := make([]MutationFileChange, 0, len(ops))
		for _, op := range ops {
			before, existed, readErr := readInverse(op.native)
			if readErr != nil {
				return readErr
			}
			change := MutationFileChange{
				Path: op.rel, ExistedBefore: existed, AfterSHA256: op.candidateHash, Mode: op.mode,
			}
			if existed {
				change.BeforeSHA256 = SHA256Bytes(before)
				change.BeforeBase64 = base64.StdEncoding.EncodeToString(before)
			}
			changes = append(changes, change)
		}

		promoted := make([]promotedChange, 0, len(ops))
		promoteErr := func() error {
			for i, op := range ops {
				if op.absent {
					if rmErr := os.Remove(op.native); rmErr != nil && !os.IsNotExist(rmErr) {
						return rmErr
					}
					promoted = append(promoted, promotedChange{change: changes[i], native: op.native})
					if _, exists, imgErr := currentImage(op.native); imgErr != nil {
						return imgErr
					} else if exists {
						return fmt.Errorf("deleted artifact %s is still present after promotion", op.rel)
					}
					continue
				}
				if writeErr := atomicWriteMode(op.native, op.candidate, op.mode); writeErr != nil {
					return writeErr
				}
				promoted = append(promoted, promotedChange{change: changes[i], native: op.native})
				got, err := SHA256File(op.native)
				if err != nil {
					return err
				}
				if got != op.candidateHash {
					return fmt.Errorf("promoted bytes for %s do not match the candidate", op.rel)
				}
			}
			return nil
		}()
		if promoteErr != nil {
			rollbackMutation(promoted)
			return promoteErr
		}

		// Post-write verification against the real tree; failure rolls back.
		if m.PostCheck != nil {
			if checkErr := m.PostCheck(); checkErr != nil {
				rollbackMutation(promoted)
				result = MutationReceipt{
					SchemaVersion: mutationSchemaVersion, MutationID: id, Protocol: MutationProtocol,
					Kind: m.Kind, Status: "ROLLED_BACK", Reason: "post-write verification failed: " + checkErr.Error(),
					Scope: sortedScope(m.Scope), RecordedAt: operationTimestamp(),
				}
				return fmt.Errorf("%w: %v", ErrMutationVerificationFailed, checkErr)
			}
		}

		result = MutationReceipt{
			SchemaVersion: mutationSchemaVersion, MutationID: id, Protocol: MutationProtocol,
			Kind: m.Kind, Status: "APPLIED", Scope: sortedScope(m.Scope),
			Changes: changes, Authority: authorityHash, RecordedAt: operationTimestamp(),
		}
		return saveMutationReceipt(repo, result)
	})
	return result, err
}

func readInverse(native string) ([]byte, bool, error) {
	value, err := os.ReadFile(native)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

// promotedChange pairs a recorded change with its resolved filesystem path so
// rollback can restore the exact pre-image without re-resolving.
type promotedChange struct {
	change MutationFileChange
	native string
}

// rollbackMutation restores each already-promoted file to its exact pre-image,
// removing files that did not exist before. Best-effort: a failed restore leaves
// the remaining files as-is, and the caller reports the original error.
func rollbackMutation(promoted []promotedChange) {
	for i := len(promoted) - 1; i >= 0; i-- {
		change := promoted[i].change
		native := promoted[i].native
		if native == "" {
			continue
		}
		if !change.ExistedBefore {
			_ = os.Remove(native)
			continue
		}
		before, decodeErr := base64.StdEncoding.DecodeString(change.BeforeBase64)
		if decodeErr != nil {
			continue
		}
		_ = atomicWriteMode(native, before, mutationMode(change.Mode))
	}
}

// receiptStillApplied reports whether every recorded post-image is still the
// on-disk truth, which is the precondition for treating a repeat call as a
// no-op replay rather than a fresh mutation.
func receiptStillApplied(repo string, receipt MutationReceipt) bool {
	for _, change := range receipt.Changes {
		native, err := resolveRepositoryRelativePath(repo, change.Path)
		if err != nil {
			return false
		}
		current, exists, imgErr := currentImage(native)
		if imgErr != nil {
			return false
		}
		if change.AfterSHA256 == "" {
			// The recorded post-image is the file's absence (a delete).
			if exists {
				return false
			}
			continue
		}
		if !exists || current != change.AfterSHA256 {
			return false
		}
	}
	return true
}

// UndoMutation deterministically reverses an applied mutation by replaying its
// inverse through the same boundary. The inverse is a first-class MutationSet:
// each recorded before-image becomes the candidate (or an absent operation when
// the file did not exist before), and the recorded post-image becomes the base
// precondition. Because the inverse goes through ApplyMutation it is atomic and
// verified, and it produces its own reversible receipt — so redo is simply
// undoing that returned receipt. The base precondition is the divergence guard:
// if any artifact no longer matches its recorded post-image, the boundary
// refuses with a stale base, which UndoMutation surfaces as ErrMutationConflict
// rather than clobbering newer work. Applying an already-reversed mutation
// replays idempotently through ApplyMutation's identity short-circuit; the
// original receipt is left immutable as history.
func UndoMutation(repoPath, mutationID string) (MutationReceipt, error) {
	repo, err := ResolveRepository(repoPath)
	if err != nil {
		return MutationReceipt{}, err
	}
	id := strings.TrimSpace(mutationID)
	receipt, ok, err := loadMutationReceipt(repo, id)
	if err != nil {
		return MutationReceipt{}, err
	}
	if !ok {
		return MutationReceipt{}, fmt.Errorf("no mutation receipt for %s", id)
	}
	if receipt.Status != "APPLIED" {
		return MutationReceipt{}, fmt.Errorf("mutation %s is not in an undoable state (%s)", id, receipt.Status)
	}

	scope := make([]string, 0, len(receipt.Changes))
	base := map[string]string{}
	ops := make([]MutationOperation, 0, len(receipt.Changes))
	for _, change := range receipt.Changes {
		scope = append(scope, change.Path)
		base[change.Path] = change.AfterSHA256 // "" means the post-image is absent
		if !change.ExistedBefore {
			ops = append(ops, MutationOperation{Path: change.Path, Absent: true})
			continue
		}
		before, decodeErr := base64.StdEncoding.DecodeString(change.BeforeBase64)
		if decodeErr != nil {
			return MutationReceipt{}, decodeErr
		}
		ops = append(ops, MutationOperation{Path: change.Path, Candidate: before, Mode: change.Mode})
	}

	inverse := MutationSet{
		Protocol:   MutationProtocol,
		Kind:       "undo:" + receipt.Kind,
		Scope:      scope,
		Base:       base,
		Operations: ops,
	}
	undone, applyErr := ApplyMutation(repo, inverse)
	if errors.Is(applyErr, ErrMutationStaleBase) {
		return undone, fmt.Errorf("%w: %s diverged from its recorded post-image", ErrMutationConflict, receipt.MutationID)
	}
	return undone, applyErr
}
