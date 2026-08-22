package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/operatorstack/boatstack/boatstack/kernel"
)

const storeSchemaVersion = 1

// storeDocument is the single durable transaction unit: control state and
// its committed receipts live in one file replaced atomically, so a target
// state and its receipt become visible together or not at all.
type storeDocument struct {
	SchemaVersion int                 `json:"schema_version"`
	State         kernel.ControlState `json:"state"`
	Receipts      []kernel.Receipt    `json:"receipts"`
}

// journalDocument is the domain-owned round journal. Rounds are append-only
// across generations; a reopen starts a new generation instead of editing
// history.
type journalDocument struct {
	SchemaVersion int            `json:"schema_version"`
	Generation    int            `json:"generation"`
	Rounds        []journalRound `json:"rounds"`
}

type journalRound struct {
	Generation           int    `json:"generation"`
	Index                int    `json:"index"`
	CandidateFingerprint string `json:"candidate_fingerprint"`
	ReviewedTree         string `json:"reviewed_tree"`
	HeadCommit           string `json:"head_commit"`
	MergeBase            string `json:"merge_base"`
	Verdict              string `json:"verdict"`
	Measure              int    `json:"measure"`
	BlockingMeasure      int    `json:"blocking_measure"`
	FindingCount         int    `json:"finding_count"`
	Priorities           [4]int `json:"priorities"`
	Transition           string `json:"transition"`
}

type stagedCandidate struct {
	Fingerprint  string `json:"fingerprint"`
	ReviewedTree string `json:"reviewed_tree"`
}

// fileStore owns one review control instance's durable state under the
// repository's .git directory, so nothing here can enter a commit.
type fileStore struct {
	dir     string
	initial kernel.ControlState
}

func newFileStore(gitDir, instanceID string, program kernel.ProgramIdentity) *fileStore {
	return &fileStore{
		dir: filepath.Join(gitDir, "boatstack-review", instanceID),
		initial: kernel.ControlState{
			InstanceID: instanceID,
			Program:    program,
			Mode:       modeUnreviewed,
			Revision:   1,
		},
	}
}

func (s *fileStore) statePath() string   { return filepath.Join(s.dir, "store.json") }
func (s *fileStore) journalPath() string { return filepath.Join(s.dir, "journal.json") }
func (s *fileStore) stagingPath() string { return filepath.Join(s.dir, "candidate.json") }
func (s *fileStore) stagingMetaPath() string {
	return filepath.Join(s.dir, "candidate-meta.json")
}
func (s *fileStore) roundsDir() string { return filepath.Join(s.dir, "rounds") }
func (s *fileStore) lockPath() string  { return filepath.Join(s.dir, "lock") }

func writeFileAtomic(path string, value []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if _, err := temp.Write(value); err != nil {
		temp.Close()
		os.Remove(tempPath)
		return err
	}
	if err := temp.Close(); err != nil {
		os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return err
	}
	return nil
}

func (s *fileStore) loadDocument() (storeDocument, error) {
	value, err := os.ReadFile(s.statePath())
	if os.IsNotExist(err) {
		return storeDocument{SchemaVersion: storeSchemaVersion, State: s.initial}, nil
	}
	if err != nil {
		return storeDocument{}, err
	}
	var document storeDocument
	if err := json.Unmarshal(value, &document); err != nil {
		return storeDocument{}, fmt.Errorf("review store %s does not decode: %w", s.statePath(), err)
	}
	if document.SchemaVersion != storeSchemaVersion {
		return storeDocument{}, fmt.Errorf("review store %s has unsupported schema version %d", s.statePath(), document.SchemaVersion)
	}
	return document, nil
}

func (s *fileStore) saveDocument(document storeDocument) error {
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.statePath(), append(encoded, '\n'))
}

func (s *fileStore) Load(context.Context, string) (kernel.ControlState, error) {
	document, err := s.loadDocument()
	if err != nil {
		return kernel.ControlState{}, err
	}
	return document.State, nil
}

func (s *fileStore) BeginEffect(_ context.Context, revision uint64, target kernel.ControlState) error {
	document, err := s.loadDocument()
	if err != nil {
		return err
	}
	if document.State.Revision != revision {
		return fmt.Errorf("stale revision: store has %d, attempt expects %d", document.State.Revision, revision)
	}
	document.State = target
	return s.saveDocument(document)
}

func (s *fileStore) CommitTransition(_ context.Context, revision uint64, target kernel.ControlState, receipt kernel.Receipt) error {
	document, err := s.loadDocument()
	if err != nil {
		return err
	}
	if document.State.Revision != revision {
		return fmt.Errorf("stale revision: store has %d, commit expects %d", document.State.Revision, revision)
	}
	document.State = target
	document.Receipts = append(document.Receipts, receipt)
	return s.saveDocument(document)
}

func (s *fileStore) loadJournal() (journalDocument, error) {
	value, err := os.ReadFile(s.journalPath())
	if os.IsNotExist(err) {
		return journalDocument{SchemaVersion: storeSchemaVersion, Generation: 1}, nil
	}
	if err != nil {
		return journalDocument{}, err
	}
	var document journalDocument
	if err := json.Unmarshal(value, &document); err != nil {
		return journalDocument{}, fmt.Errorf("review journal %s does not decode: %w", s.journalPath(), err)
	}
	if document.SchemaVersion != storeSchemaVersion {
		return journalDocument{}, fmt.Errorf("review journal %s has unsupported schema version %d", s.journalPath(), document.SchemaVersion)
	}
	return document, nil
}

func (s *fileStore) saveJournal(document journalDocument) error {
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.journalPath(), append(encoded, '\n'))
}

// currentRounds returns the rounds of the active generation in order.
func (d journalDocument) currentRounds() []journalRound {
	var rounds []journalRound
	for _, round := range d.Rounds {
		if round.Generation == d.Generation {
			rounds = append(rounds, round)
		}
	}
	return rounds
}

func (s *fileStore) stageCandidate(candidateBytes []byte, reviewedTree string) error {
	meta, err := json.MarshalIndent(stagedCandidate{
		Fingerprint:  candidateFingerprint(candidateBytes),
		ReviewedTree: reviewedTree,
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.stagingPath(), candidateBytes); err != nil {
		return err
	}
	return writeFileAtomic(s.stagingMetaPath(), append(meta, '\n'))
}

// loadStagedCandidate returns the staged candidate bytes and metadata, or ok
// false when nothing is staged. A staged candidate whose metadata is missing
// or inconsistent with the exact bytes is reported as an integrity error.
func (s *fileStore) loadStagedCandidate() (candidate []byte, meta stagedCandidate, ok bool, err error) {
	candidate, err = os.ReadFile(s.stagingPath())
	if os.IsNotExist(err) {
		return nil, stagedCandidate{}, false, nil
	}
	if err != nil {
		return nil, stagedCandidate{}, false, err
	}
	metaBytes, err := os.ReadFile(s.stagingMetaPath())
	if err != nil {
		return nil, stagedCandidate{}, false, fmt.Errorf("staged candidate metadata is unavailable: %w", err)
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, stagedCandidate{}, false, fmt.Errorf("staged candidate metadata does not decode: %w", err)
	}
	if meta.Fingerprint != candidateFingerprint(candidate) {
		return nil, stagedCandidate{}, false, fmt.Errorf("staged candidate bytes do not match their recorded fingerprint")
	}
	return candidate, meta, true, nil
}

func (s *fileStore) clearStagedCandidate() error {
	for _, path := range []string{s.stagingPath(), s.stagingMetaPath()} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// archiveRound stores the exact candidate bytes content-addressed and
// appends the round record to the journal.
func (s *fileStore) archiveRound(candidateBytes []byte, round journalRound) error {
	journal, err := s.loadJournal()
	if err != nil {
		return err
	}
	round.Generation = journal.Generation
	round.Index = len(journal.currentRounds()) + 1
	if err := writeFileAtomic(filepath.Join(s.roundsDir(), round.CandidateFingerprint+".json"), candidateBytes); err != nil {
		return err
	}
	journal.Rounds = append(journal.Rounds, round)
	if err := s.saveJournal(journal); err != nil {
		return err
	}
	return s.clearStagedCandidate()
}

func (s *fileStore) roundBytes(fingerprint string) ([]byte, error) {
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(fingerprint) {
		return nil, fmt.Errorf("round fingerprint %q is not a sha256 identity", fingerprint)
	}
	return os.ReadFile(filepath.Join(s.roundsDir(), fingerprint+".json"))
}

// archive moves the whole instance directory aside, preserving receipts and
// journal for inspection while releasing the instance identity.
func (s *fileStore) archive(timestamp string) (string, error) {
	archived := s.dir + "-archived-" + timestamp
	if err := os.Rename(s.dir, archived); err != nil {
		return "", err
	}
	return archived, nil
}

func (s *fileStore) nextGeneration() error {
	journal, err := s.loadJournal()
	if err != nil {
		return err
	}
	journal.Generation++
	if err := s.saveJournal(journal); err != nil {
		return err
	}
	return s.clearStagedCandidate()
}

// directoryLocker serializes one control instance with an exclusive lock
// directory. A crash can leave the lock behind; the error names the exact
// path so the operator can remove a stale lock deliberately.
type directoryLocker struct{ path string }

func (l directoryLocker) Acquire(context.Context, string) (kernel.Lock, error) {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return nil, err
	}
	if err := os.Mkdir(l.path, 0o755); err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another boatstack-reviewer invocation holds %s; remove it only if that process is gone", l.path)
		}
		return nil, err
	}
	return directoryLock{path: l.path}, nil
}

type directoryLock struct{ path string }

func (l directoryLock) Unlock() error { return os.Remove(l.path) }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

var instanceSanitizer = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// instanceIDForBranch derives a semantic control-instance identity from a
// branch name; slashes and other non-semantic characters become dashes.
func instanceIDForBranch(branch string) (string, error) {
	value := instanceSanitizer.ReplaceAllString(strings.TrimSpace(branch), "-")
	value = strings.Trim(value, "-._")
	if value == "" {
		return "", fmt.Errorf("branch %q does not yield a usable instance identity; pass --delivery", branch)
	}
	return value, nil
}

// localAuthority materializes the invoking actor's capability receipts. This
// is local, self-declared authority: the sealed receipt records the actor,
// and origin authenticity remains not-proven, exactly like work-package
// verification reports.
func localAuthority(actor string, now time.Time, capabilities ...kernel.Capability) (kernel.Authority, error) {
	sanitized := instanceSanitizer.ReplaceAllString(strings.TrimSpace(actor), "-")
	sanitized = strings.Trim(sanitized, "-._")
	if sanitized == "" {
		return kernel.Authority{}, fmt.Errorf("an explicit --actor identity is required")
	}
	return kernel.Authority{Receipts: []kernel.AuthorityReceipt{{
		ID:           "local-" + sanitized,
		Subject:      actor,
		Fingerprint:  "local-actor:" + sanitized,
		Capabilities: capabilities,
		IssuedAt:     now.Add(-time.Second),
	}}}, nil
}
