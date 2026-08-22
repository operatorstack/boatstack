// Skill workflow: run one round of the Boatstack supervisory-control
// self-review for the current branch and report the verdict.
//
// The workflow never changes code: the agent performs the review read-only,
// the boatstack-reviewer admits or refuses the candidate, and the recorded
// verdict is the result. Yield owns the order and the observed command
// results; the reviewer owns admissibility, freshness, and receipts.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/operatorstack/yield/sdk/yield"
)

// prelude prepares every command's execution context. Commands run with the
// skill directory as the working directory. When the test sentinel exists
// (created by fixtures/setup.sh under yskill's fixture runner), commands
// operate on the scratch repository so tests never touch real review state.
//
// The reviewer is rebuilt in every command, not once per run: the Go build
// cache makes an unchanged rebuild subsecond, and a run whose commits change
// the reviewer's own code (or the branch under review) always drives the
// binary compiled from the current tree instead of a snapshot from run start.
const prelude = `set -eu
root="$(git rev-parse --show-toplevel)"
if [ -f fixtures/tmp/active ]; then repo="$PWD/fixtures/tmp/repo"; base=main; else repo="$root"; base=origin/main; fi
tmp="${TMPDIR:-/tmp}/boatstack-self-review"
mkdir -p "$tmp"
reviewer="$tmp/boatstack-reviewer"
go build -C "$root/boatstack" -o "$reviewer" ./cmd/boatstack-reviewer
`

const actor = "yield-self-review"

// reportOnlyContract is the skill's whole scope: it records the round and
// reports the verdict. Sealing, committing, and pushing are separate
// decisions that belong to the user (or the self-review-solve skill).
const reportOnlyContract = "report this verdict in the conversation and stop; " +
	"do not seal, commit, or push anything unless the user asks"

type resolveOutput struct {
	Instance string `json:"instance"`
	State    struct {
		Mode string `json:"mode"`
	} `json:"state"`
	Observation struct {
		WorktreeDirty bool `json:"worktree_dirty"`
		Rounds        []struct {
			Index        int    `json:"index"`
			Verdict      string `json:"verdict"`
			Measure      int    `json:"measure"`
			ReviewedTree string `json:"reviewed_tree"`
		} `json:"rounds"`
	} `json:"observation"`
	Instructions struct {
		PromptPath   string `json:"prompt_path"`
		ReviewRange  string `json:"review_range"`
		ReviewedTree string `json:"reviewed_tree"`
		SchemaPath   string `json:"output_schema_path"`
	} `json:"instructions"`
}

type showOutput struct {
	Mode  string `json:"mode"`
	Round struct {
		Index           int    `json:"index"`
		Verdict         string `json:"verdict"`
		Measure         int    `json:"measure"`
		BlockingMeasure int    `json:"blocking_measure"`
		FindingCount    int    `json:"finding_count"`
	} `json:"round"`
	Review json.RawMessage `json:"review"`
}

// residualTitles lists the non-blocking (P2/P3) finding titles of a review.
// Blocking is P0/P1 under the admitted policy; residuals are recorded with
// the round but never drive another round.
func residualTitles(review json.RawMessage) []string {
	var parsed struct {
		Findings []struct {
			Title    string `json:"title"`
			Priority int    `json:"priority"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(review, &parsed); err != nil {
		return nil
	}
	var titles []string
	for _, finding := range parsed.Findings {
		if finding.Priority >= 2 {
			titles = append(titles, fmt.Sprintf("P%d: %s", finding.Priority, finding.Title))
		}
	}
	return titles
}

// writeCandidate stages the encoded candidate through bounded chunks so no
// single command string approaches the platform's per-argument size cap
// (MAX_ARG_STRLEN on Linux), which a large multi-finding review could
// otherwise exceed.
func writeCandidate(ctx *yield.Context, idPrefix string, review []byte) {
	encoded := base64.StdEncoding.EncodeToString(review)
	const chunkSize = 65536
	for i, part := 0, 1; i < len(encoded); i, part = i+chunkSize, part+1 {
		end := i + chunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		redirect := ">>"
		if i == 0 {
			redirect = ">"
		}
		written := ctx.RunCommand(fmt.Sprintf("%s-part-%d", idPrefix, part),
			prelude+`printf '%s' '`+encoded[i:end]+`' `+redirect+` "$tmp/candidate.b64"`, 60)
		ctx.Require(written.ExitCode == 0, "the candidate chunk is staged", written)
	}
}

func resolveState(ctx *yield.Context, id string) (resolveOutput, error) {
	result := ctx.RunCommand(id,
		prelude+`"$reviewer" resolve --repo "$repo" --base "$base" --actor `+actor, 120)
	ctx.Require(result.ExitCode == 0, "the review control state resolves", result)
	var resolved resolveOutput
	if err := json.Unmarshal([]byte(result.Stdout), &resolved); err != nil {
		return resolveOutput{}, err
	}
	return resolved, nil
}

func main() {
	yield.Main(func(ctx *yield.Context) (yield.Outcome, error) {
		build := ctx.RunCommand("build-reviewer",
			prelude+`go build -C "$root/boatstack" -o "$reviewer" ./cmd/boatstack-reviewer`, 600)
		ctx.Require(build.ExitCode == 0, "boatstack-reviewer builds from the current tree", build)

		// Track only tracked content, mirroring the reviewer's worktree law:
		// untracked files never affect what a review can bind.
		before := ctx.RunCommand("worktree-before",
			prelude+`git -C "$repo" status --porcelain --untracked-files=no`, 60)
		ctx.Require(before.ExitCode == 0, "the repository state is observable", before)

		resolved, err := resolveState(ctx, "resolve")
		if err != nil {
			return yield.Outcome{}, err
		}
		if resolved.State.Mode == "converged" {
			rounds := resolved.Observation.Rounds
			if len(rounds) > 0 && rounds[len(rounds)-1].ReviewedTree == resolved.Instructions.ReviewedTree {
				converged := ctx.RunCommand("show-converged",
					prelude+`"$reviewer" show --repo "$repo" --base "$base"`, 60)
				ctx.Require(converged.ExitCode == 0, "the converged round is shown from the store", converged)
				var shown showOutput
				if err := json.Unmarshal([]byte(converged.Stdout), &shown); err != nil {
					return yield.Outcome{}, err
				}
				result := map[string]any{
					"instance":         resolved.Instance,
					"mode":             resolved.State.Mode,
					"verdict":          shown.Round.Verdict,
					"blocking_measure": shown.Round.BlockingMeasure,
					"action":           reportOnlyContract,
				}
				if residuals := residualTitles(shown.Review); len(residuals) > 0 {
					result["residual_findings"] = residuals
				}
				return ctx.Complete(result)
			}
			// The instance converged for an older tree; new commits need a
			// fresh generation before a round can be recorded.
			reopen := ctx.RunCommand("reopen",
				prelude+`"$reviewer" reopen --repo "$repo" --base "$base" --actor `+actor, 120)
			ctx.Require(reopen.ExitCode == 0,
				"a fresh review generation is open for the moved tree", reopen)
			if resolved, err = resolveState(ctx, "resolve-after-reopen"); err != nil {
				return yield.Outcome{}, err
			}
		}
		if resolved.State.Mode == "escalated" {
			return yield.Outcome{}, ctx.Refused(
				"the review loop escalated; a human must decide, then boatstack-reviewer reopen")
		}
		if resolved.Observation.WorktreeDirty {
			return yield.Outcome{}, ctx.Refused(
				"the worktree has uncommitted tracked changes; commit first — a review binds only a committed tree")
		}

		schema := ctx.RunCommand("schema",
			prelude+`cat "$repo/`+resolved.Instructions.SchemaPath+`"`, 30)
		ctx.Require(schema.ExitCode == 0, "the admitted output schema is readable", schema)

		instruction := fmt.Sprintf(
			"Perform the code review described by %s (in the repository under review) over exactly the range %s. "+
				"Read the prompt file first and follow it precisely. Review only committed content; do not modify, create, or delete any file. "+
				"Anchor every finding to changed lines of that exact range and return only the JSON object required by the schema.",
			resolved.Instructions.PromptPath, resolved.Instructions.ReviewRange)
		review := ctx.AgentTask("review", instruction,
			map[string]any{
				"instance":      resolved.Instance,
				"mode":          resolved.State.Mode,
				"review_range":  resolved.Instructions.ReviewRange,
				"reviewed_tree": resolved.Instructions.ReviewedTree,
				"prompt_path":   resolved.Instructions.PromptPath,
			},
			json.RawMessage(schema.Stdout))

		writeCandidate(ctx, "candidate", review)
		submit := ctx.RunCommand("submit",
			prelude+`base64 -d < "$tmp/candidate.b64" > "$tmp/candidate.json"
"$reviewer" submit --repo "$repo" --base "$base" --findings "$tmp/candidate.json" --actor `+actor, 120)
		if submit.ExitCode != 0 {
			return yield.Outcome{}, ctx.Blocked(
				"the reviewer refused the candidate: " + submit.Stderr)
		}

		verdict := ctx.RunCommand("verdict",
			prelude+`"$reviewer" show --repo "$repo" --base "$base"`, 60)
		ctx.Require(verdict.ExitCode == 0, "the recorded round is shown from the store", verdict)
		var shown showOutput
		if err := json.Unmarshal([]byte(verdict.Stdout), &shown); err != nil {
			return yield.Outcome{}, err
		}

		after := ctx.RunCommand("worktree-after",
			prelude+`git -C "$repo" status --porcelain --untracked-files=no`, 60)
		ctx.Require(after.ExitCode == 0 && after.Stdout == before.Stdout,
			"the review changed no files in the repository", map[string]any{
				"before": before.Stdout, "after": after.Stdout,
			})

		result := map[string]any{
			"instance":         resolved.Instance,
			"mode":             shown.Mode,
			"round":            shown.Round.Index,
			"verdict":          shown.Round.Verdict,
			"measure":          shown.Round.Measure,
			"blocking_measure": shown.Round.BlockingMeasure,
			"finding_count":    shown.Round.FindingCount,
			"action":           reportOnlyContract,
		}
		if shown.Mode == "converged" {
			// A converged round may still carry residual (P2/P3) findings;
			// they are reported as titles for the user to weigh, not as
			// work the loop demands.
			if residuals := residualTitles(shown.Review); len(residuals) > 0 {
				result["residual_findings"] = residuals
			}
		} else {
			// Open blocking findings are the actionable content; the full
			// review carries them.
			result["review"] = json.RawMessage(shown.Review)
		}
		return ctx.Complete(result)
	})
}
