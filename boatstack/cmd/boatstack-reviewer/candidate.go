package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// verdictCorrect and verdictIncorrect are the only overall verdicts the
// admitted output schema allows.
const (
	verdictCorrect   = "patch is correct"
	verdictIncorrect = "patch is incorrect"
)

// reviewDocument mirrors the admitted output schema. The schema bytes remain
// the contract: every candidate is validated against the exact policy schema
// before this projection is trusted.
type reviewDocument struct {
	Findings           []reviewFinding `json:"findings"`
	OverallCorrectness string          `json:"overall_correctness"`
	OverallExplanation string          `json:"overall_explanation"`
	OverallConfidence  float64         `json:"overall_confidence_score"`
}

type reviewFinding struct {
	Title           string       `json:"title"`
	Body            string       `json:"body"`
	ConfidenceScore float64      `json:"confidence_score"`
	Priority        int          `json:"priority"`
	CodeLocation    reviewAnchor `json:"code_location"`
}

type reviewAnchor struct {
	AbsoluteFilePath string `json:"absolute_file_path"`
	Side             string `json:"side"`
	LineRange        struct {
		Start int `json:"start"`
		End   int `json:"end"`
	} `json:"line_range"`
}

// candidateSummary is the deterministic projection of one staged candidate
// that enters the domain observation. Validity is recomputed on every
// observation from the exact staged bytes and the exact current diff; the
// proposer's own claims are never trusted.
type candidateSummary struct {
	Fingerprint    string   `json:"fingerprint"`
	ReviewedTree   string   `json:"reviewed_tree"`
	Valid          bool     `json:"valid"`
	InvalidReasons []string `json:"invalid_reasons,omitempty"`
	Verdict        string   `json:"verdict,omitempty"`
	Measure        int      `json:"measure"`
	FindingCount   int      `json:"finding_count"`
	Priorities     [4]int   `json:"priorities"`
}

// candidateFingerprint identifies candidate review bytes by the sha256 of
// their compacted JSON form. Compaction is a whitespace-only normalization
// (key order and literals are preserved), so the identity survives the
// re-indentation a candidate undergoes when embedded in a sealed receipt.
// Non-JSON bytes hash as-is; they are rejected later as invalid candidates.
func candidateFingerprint(candidateBytes []byte) string {
	var compact bytes.Buffer
	if err := json.Compact(&compact, candidateBytes); err != nil {
		return sha256Hex(candidateBytes)
	}
	return sha256Hex(compact.Bytes())
}

// evaluateCandidate performs the deterministic admission checks: exact
// schema validation against the admitted policy schema bytes, diff-anchor
// validation against the exact merge-base..head diff, and the convergence
// measure. It never trusts the proposer; an unparseable or out-of-contract
// candidate is a refusal reason, not an error.
func evaluateCandidate(policy Policy, candidateBytes []byte, stagedTree, repoRoot, diff string) candidateSummary {
	summary := candidateSummary{
		Fingerprint:  candidateFingerprint(candidateBytes),
		ReviewedTree: stagedTree,
	}
	reject := func(reason string) candidateSummary {
		summary.Valid = false
		summary.InvalidReasons = append(summary.InvalidReasons, reason)
		return summary
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(candidateBytes))
	if err != nil {
		return reject("candidate is not valid JSON: " + err.Error())
	}
	schemaDocument, err := jsonschema.UnmarshalJSON(bytes.NewReader(policy.SchemaBytes))
	if err != nil {
		return reject("admitted output schema does not decode: " + err.Error())
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("review-output-schema.json", schemaDocument); err != nil {
		return reject("admitted output schema is not compilable: " + err.Error())
	}
	schema, err := compiler.Compile("review-output-schema.json")
	if err != nil {
		return reject("admitted output schema is not compilable: " + err.Error())
	}
	if err := schema.Validate(document); err != nil {
		return reject("candidate violates the admitted output schema: " + firstLine(err.Error()))
	}
	var review reviewDocument
	decoder := json.NewDecoder(bytes.NewReader(candidateBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil {
		return reject("candidate does not decode into the review contract: " + err.Error())
	}
	summary.Verdict = review.OverallCorrectness
	summary.FindingCount = len(review.Findings)
	allowed := changedLines(diff)
	valid := true
	for index, finding := range review.Findings {
		if finding.Priority >= 0 && finding.Priority <= 3 {
			summary.Priorities[finding.Priority]++
			summary.Measure += policy.Weights[finding.Priority]
		}
		if reason := anchorFailure(finding.CodeLocation, repoRoot, allowed); reason != "" {
			valid = false
			summary.InvalidReasons = append(summary.InvalidReasons,
				fmt.Sprintf("finding %d (%q): %s", index, finding.Title, reason))
		}
	}
	summary.Valid = valid
	return summary
}

func firstLine(value string) string {
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		return value[:index]
	}
	return value
}

// anchorFailure checks one finding location against the allowed changed
// lines of the exact review diff. It ports the anchor discipline of the
// retired publish script: repository-relative normalized path, declared
// LEFT/RIGHT side, and every line of the range present on that side.
func anchorFailure(anchor reviewAnchor, repoRoot string, allowed map[string]map[lineKey]bool) string {
	normalized, ok := normalizeAnchorPath(anchor.AbsoluteFilePath, repoRoot)
	if !ok {
		return "location path does not normalize to a repository-relative path"
	}
	side, ok := allowed[anchor.Side]
	if !ok {
		return "location side is not LEFT or RIGHT"
	}
	start, end := anchor.LineRange.Start, anchor.LineRange.End
	if start > end {
		return "location line range is inverted"
	}
	for line := start; line <= end; line++ {
		if !side[lineKey{Path: normalized, Line: line}] {
			return fmt.Sprintf("line %s:%d (%s) is not part of the review diff", normalized, line, anchor.Side)
		}
	}
	return ""
}

func normalizeAnchorPath(value, repoRoot string) (string, bool) {
	candidate := strings.ReplaceAll(value, "\\", "/")
	root := strings.TrimRight(strings.ReplaceAll(repoRoot, "\\", "/"), "/")
	if root != "" && strings.HasPrefix(candidate, root+"/") {
		candidate = candidate[len(root)+1:]
	}
	for strings.HasPrefix(candidate, "./") {
		candidate = candidate[2:]
	}
	candidate = path.Clean(candidate)
	if candidate == "" || candidate == "." || strings.HasPrefix(candidate, "/") ||
		candidate == ".." || strings.HasPrefix(candidate, "../") || strings.Contains(candidate, "/../") {
		return "", false
	}
	return candidate, true
}

type lineKey struct {
	Path string
	Line int
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// changedLines parses a zero-context unified diff into the exact sets of
// (path, line) pairs a finding may anchor to, per diff side.
func changedLines(diff string) map[string]map[lineKey]bool {
	allowed := map[string]map[lineKey]bool{
		"LEFT":  {},
		"RIGHT": {},
	}
	var oldPath, newPath string
	var oldLine, newLine int
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			oldPath, newPath = "", ""
			inHunk = false
		case !inHunk && strings.HasPrefix(line, "--- "):
			oldPath = headerPath(line)
		case !inHunk && strings.HasPrefix(line, "+++ "):
			newPath = headerPath(line)
		default:
			if match := hunkHeader.FindStringSubmatch(line); match != nil {
				oldLine = mustInt(match[1])
				newLine = mustInt(match[3])
				inHunk = true
				continue
			}
			if !inHunk || strings.HasPrefix(line, "\\") {
				continue
			}
			switch {
			case strings.HasPrefix(line, "-"):
				if oldPath != "" {
					allowed["LEFT"][lineKey{Path: oldPath, Line: oldLine}] = true
				}
				oldLine++
			case strings.HasPrefix(line, "+"):
				if newPath != "" {
					allowed["RIGHT"][lineKey{Path: newPath, Line: newLine}] = true
				}
				newLine++
			case strings.HasPrefix(line, " "):
				oldLine++
				newLine++
			default:
				inHunk = false
			}
		}
	}
	return allowed
}

func headerPath(line string) string {
	value := line[4:]
	if tab := strings.IndexByte(value, '\t'); tab >= 0 {
		value = value[:tab]
	}
	if value == "/dev/null" {
		return ""
	}
	value = strings.TrimPrefix(value, "a/")
	value = strings.TrimPrefix(value, "b/")
	return value
}

func mustInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

// stalled reports whether recording this candidate would extend the trailing
// run of submissions without measure improvement to the policy stall window.
// The run length counts the submissions themselves: with a window of three,
// a third consecutive submission that fails to decrease the measure below
// its predecessor escalates instead of recording another round.
func stalled(policy Policy, recorded []int, candidateMeasure int) bool {
	if len(recorded) == 0 {
		return false
	}
	history := append(append([]int(nil), recorded...), candidateMeasure)
	runLength := 1
	for index := len(history) - 1; index >= 1; index-- {
		if history[index] < history[index-1] {
			break
		}
		runLength++
		if runLength >= policy.StallWindow {
			return true
		}
	}
	return false
}
