package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// The admitted review policy is the pair of repository assets the previous
// CI reviewer already pinned to the pull-request base revision, plus the
// convergence bounds. All of it is hashed into the review Program's domain
// contract fingerprint: changing any part produces a different program
// identity and honestly invalidates every prior prescription and receipt.
const (
	policyPromptPath = ".github/codex/review-prompt.md"
	policySchemaPath = ".github/codex/review-output-schema.json"

	// defaultMaxRounds bounds one review generation. The mined fixture
	// (testdata/review_rounds.json) observed a maximum of 14 review rounds
	// on one pull request; 16 covers every observed loop with headroom.
	defaultMaxRounds = 16

	// defaultStallWindow is the number of consecutive non-improving
	// submissions after which the loop escalates instead of recording
	// another round. The mined history shows measures normally move within
	// one to two rounds of a fix landing.
	defaultStallWindow = 3
)

// defaultWeights maps finding priority P0..P3 to its weight in the
// convergence measure V = sum(weight(priority)) over open findings.
var defaultWeights = [4]int{1000, 100, 10, 1}

type Policy struct {
	PromptPath   string `json:"prompt_path"`
	PromptSHA256 string `json:"prompt_sha256"`
	SchemaPath   string `json:"schema_path"`
	SchemaSHA256 string `json:"schema_sha256"`
	MaxRounds    int    `json:"max_rounds"`
	StallWindow  int    `json:"stall_window"`
	Weights      [4]int `json:"weights"`

	PromptBytes []byte `json:"-"`
	SchemaBytes []byte `json:"-"`
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func newPolicy(promptBytes, schemaBytes []byte) Policy {
	return Policy{
		PromptPath:   policyPromptPath,
		PromptSHA256: sha256Hex(promptBytes),
		SchemaPath:   policySchemaPath,
		SchemaSHA256: sha256Hex(schemaBytes),
		MaxRounds:    defaultMaxRounds,
		StallWindow:  defaultStallWindow,
		Weights:      defaultWeights,
		PromptBytes:  promptBytes,
		SchemaBytes:  schemaBytes,
	}
}

// loadWorktreePolicy reads the policy assets from the repository worktree.
// The local loop always reviews under the policy present in the tree being
// reviewed; CI verification separately re-admits the policy from the pull
// request base revision.
func loadWorktreePolicy(repoRoot string) (Policy, error) {
	prompt, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(policyPromptPath)))
	if err != nil {
		return Policy{}, fmt.Errorf("review policy prompt is unavailable: %w", err)
	}
	schema, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(policySchemaPath)))
	if err != nil {
		return Policy{}, fmt.Errorf("review policy schema is unavailable: %w", err)
	}
	return newPolicy(prompt, schema), nil
}

// loadRevisionPolicy reads the policy assets from an exact committed
// revision, mirroring the base-revision admission the retired CI reviewer
// performed with `git show "$BASE_SHA:<asset>"`.
func loadRevisionPolicy(repo *gitRepo, revision string) (Policy, error) {
	prompt, err := repo.showFile(revision, policyPromptPath)
	if err != nil {
		return Policy{}, fmt.Errorf("revision %s has no admitted review policy prompt: %w", revision, err)
	}
	schema, err := repo.showFile(revision, policySchemaPath)
	if err != nil {
		return Policy{}, fmt.Errorf("revision %s has no admitted review policy schema: %w", revision, err)
	}
	return newPolicy(prompt, schema), nil
}

// contractFingerprint is the domain contract identity compiled into the
// review Program. It covers the exact policy asset bytes and the exact
// convergence bounds; the executable transition law is separately covered
// by the kernel Program fingerprint.
func (p Policy) contractFingerprint() (string, error) {
	encoded, err := json.Marshal(struct {
		PromptPath   string `json:"prompt_path"`
		PromptSHA256 string `json:"prompt_sha256"`
		SchemaPath   string `json:"schema_path"`
		SchemaSHA256 string `json:"schema_sha256"`
		MaxRounds    int    `json:"max_rounds"`
		StallWindow  int    `json:"stall_window"`
		Weights      [4]int `json:"weights"`
	}{p.PromptPath, p.PromptSHA256, p.SchemaPath, p.SchemaSHA256, p.MaxRounds, p.StallWindow, p.Weights})
	if err != nil {
		return "", err
	}
	return sha256Hex(encoded), nil
}

func (p Policy) validate() error {
	if len(p.PromptBytes) == 0 || len(p.SchemaBytes) == 0 {
		return fmt.Errorf("review policy requires non-empty prompt and schema assets")
	}
	if p.PromptSHA256 != sha256Hex(p.PromptBytes) || p.SchemaSHA256 != sha256Hex(p.SchemaBytes) {
		return fmt.Errorf("review policy asset hashes do not identify their exact bytes")
	}
	if p.MaxRounds < 1 || p.StallWindow < 1 {
		return fmt.Errorf("review policy requires positive round and stall bounds")
	}
	for _, weight := range p.Weights {
		if weight < 1 {
			return fmt.Errorf("review policy requires positive priority weights")
		}
	}
	return nil
}
