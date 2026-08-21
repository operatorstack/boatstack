// Skill workflow: drive the Boatstack supervisory-control self-review to
// convergence and seal the receipt.
//
// The workflow decides from the committed control state what is needed:
// open findings are fixed in code and committed, an unreviewed tree gets a
// fresh review, an escalated loop asks the human before reopening, and a
// converged loop is sealed. The loop is bounded; if it does not converge
// within the attempt budget the run blocks honestly instead of guessing.
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
const prelude = `set -eu
root="$(git rev-parse --show-toplevel)"
if [ -f fixtures/tmp/active ]; then repo="$PWD/fixtures/tmp/repo"; base=main; else repo="$root"; base=origin/main; fi
tmp="${TMPDIR:-/tmp}/boatstack-self-review-solve"
mkdir -p "$tmp"
reviewer="$tmp/boatstack-reviewer"
`

const (
	actor       = "yield-self-review-solve"
	maxAttempts = 3
)

const fixReportSchema = `{
  "type": "object",
  "required": ["summary", "committed"],
  "properties": {
    "summary": {"type": "string", "minLength": 1},
    "committed": {"type": "boolean"}
  },
  "additionalProperties": false
}`

type statusOutput struct {
	Instance string `json:"instance"`
	State    struct {
		Mode string `json:"mode"`
	} `json:"state"`
	ProgramStale bool `json:"program_stale"`
	Observation  struct {
		WorktreeDirty bool   `json:"worktree_dirty"`
		ReviewedTree  string `json:"reviewed_tree"`
		Rounds        []struct {
			Index        int    `json:"index"`
			Verdict      string `json:"verdict"`
			Measure      int    `json:"measure"`
			ReviewedTree string `json:"reviewed_tree"`
		} `json:"rounds"`
	} `json:"observation"`
}

type resolveOutput struct {
	Instructions struct {
		PromptPath  string `json:"prompt_path"`
		ReviewRange string `json:"review_range"`
		SchemaPath  string `json:"output_schema_path"`
	} `json:"instructions"`
}

type showOutput struct {
	Mode  string `json:"mode"`
	Round struct {
		Index   int    `json:"index"`
		Verdict string `json:"verdict"`
		Measure int    `json:"measure"`
	} `json:"round"`
	Review json.RawMessage `json:"review"`
}

func main() {
	yield.Main(func(ctx *yield.Context) (yield.Outcome, error) {
		build := ctx.RunCommand("build-reviewer",
			prelude+`go build -C "$root/boatstack" -o "$reviewer" ./cmd/boatstack-reviewer`, 600)
		ctx.Require(build.ExitCode == 0, "boatstack-reviewer builds from the current tree", build)

		observed := status(ctx, "status")
		mode, measures := observed.State.Mode, observed.measures()
		if mode == "converged" && observed.treeDrifted() {
			// The instance converged for an older tree; new commits need a
			// fresh generation. Invoking this skill is the decision to
			// re-review them, so reopen without asking.
			reopen := ctx.RunCommand("reopen-drift",
				prelude+`"$reviewer" reopen --repo "$repo" --base "$base" --actor `+actor, 120)
			ctx.Require(reopen.ExitCode == 0,
				"a fresh review generation is open for the moved tree", reopen)
			observed = status(ctx, "status-after-drift-reopen")
			mode, measures = observed.State.Mode, observed.measures()
		}
		if mode == "escalated" {
			answer := ctx.AskUser("reopen",
				"The review loop escalated (the convergence measure stalled or the round bound was reached). Reopen a fresh review generation?",
				yield.Option{Value: "yes", Label: "Reopen and continue"},
				yield.Option{Value: "no", Label: "Stop; a human will handle it"})
			if answer != "yes" {
				return yield.Outcome{}, ctx.Refused("the escalated loop stays with the human")
			}
			reopen := ctx.RunCommand("reopen",
				prelude+`"$reviewer" reopen --repo "$repo" --base "$base" --actor `+actor, 120)
			ctx.Require(reopen.ExitCode == 0, "a fresh review generation is open", reopen)
			observed = status(ctx, "status-after-reopen")
			mode, measures = observed.State.Mode, observed.measures()
		}

		for attempt := 1; mode != "converged" && attempt <= maxAttempts; attempt++ {
			tag := fmt.Sprintf("-%d", attempt)

			// Fix only when the open findings still describe the current
			// tree; if commits already landed since the round was recorded,
			// the fixes may exist and a fresh review is what decides.
			if mode == "findings-open" && !observed.treeDrifted() {
				shown := show(ctx, "open-findings"+tag)
				fixRaw := ctx.AgentTask("fix"+tag,
					"Fix every finding of this recorded review in the repository under review: edit the code, "+
						"run the relevant tests, and commit the fixes with a clear message. Do not touch .github/reviews "+
						"and do not weaken tests to make findings disappear. Report what you changed.",
					map[string]any{"round": shown.Round, "review": json.RawMessage(shown.Review)},
					json.RawMessage(fixReportSchema))
				var fix struct {
					Summary   string `json:"summary"`
					Committed bool   `json:"committed"`
				}
				if err := json.Unmarshal(fixRaw, &fix); err != nil {
					return yield.Outcome{}, err
				}
				ctx.Require(fix.Committed, "the fixes are committed", fix)
				// Mirror the reviewer's worktree law: untracked files never
				// affect what a review can bind, so only tracked changes
				// count as an uncommitted fix.
				clean := ctx.RunCommand("worktree"+tag,
					prelude+`git -C "$repo" status --porcelain --untracked-files=no`, 60)
				ctx.Require(clean.ExitCode == 0 && clean.Stdout == "",
					"the worktree is clean after the fix commit", clean)
			}

			resolve := ctx.RunCommand("resolve"+tag,
				prelude+`"$reviewer" resolve --repo "$repo" --base "$base" --actor `+actor, 120)
			ctx.Require(resolve.ExitCode == 0, "the review control state resolves", resolve)
			var resolved resolveOutput
			if err := json.Unmarshal([]byte(resolve.Stdout), &resolved); err != nil {
				return yield.Outcome{}, err
			}
			schema := ctx.RunCommand("schema"+tag,
				prelude+`cat "$repo/`+resolved.Instructions.SchemaPath+`"`, 30)
			ctx.Require(schema.ExitCode == 0, "the admitted output schema is readable", schema)

			review := ctx.AgentTask("review"+tag,
				fmt.Sprintf("Perform the code review described by %s (in the repository under review) over exactly the range %s. "+
					"Read the prompt file first and follow it precisely. Review only committed content; do not modify any file. "+
					"Anchor every finding to changed lines of that exact range and return only the JSON object required by the schema.",
					resolved.Instructions.PromptPath, resolved.Instructions.ReviewRange),
				map[string]any{"review_range": resolved.Instructions.ReviewRange},
				json.RawMessage(schema.Stdout))

			writeCandidate(ctx, "candidate"+tag, review)
			submit := ctx.RunCommand("submit"+tag,
				prelude+`base64 -d < "$tmp/candidate.b64" > "$tmp/candidate.json"
"$reviewer" submit --repo "$repo" --base "$base" --findings "$tmp/candidate.json" --actor `+actor, 120)
			if submit.ExitCode != 0 {
				return yield.Outcome{}, ctx.Blocked(
					"the reviewer refused the candidate: " + submit.Stderr)
			}

			observed = status(ctx, "status"+tag)
			mode, measures = observed.State.Mode, observed.measures()
			if mode == "escalated" {
				return yield.Outcome{}, ctx.Blocked(
					"the loop escalated during solving; a human must decide before reopening")
			}
		}

		if mode != "converged" {
			return yield.Outcome{}, ctx.Blocked(fmt.Sprintf(
				"the review did not converge within %d attempts (measures %v); the remaining findings need a human decision",
				maxAttempts, measures))
		}

		seal := ctx.RunCommand("seal",
			prelude+`"$reviewer" seal --repo "$repo" --base "$base"`, 120)
		ctx.Require(seal.ExitCode == 0, "the converged review seals a receipt", seal)
		var sealed struct {
			Sealed       string `json:"sealed"`
			Fingerprint  string `json:"fingerprint"`
			ReviewedTree string `json:"reviewed_tree"`
		}
		if err := json.Unmarshal([]byte(seal.Stdout), &sealed); err != nil {
			return yield.Outcome{}, err
		}

		commit := ctx.RunCommand("commit-receipt",
			prelude+`if [ -n "$(git -C "$repo" status --porcelain .github/reviews)" ]; then
  git -C "$repo" add .github/reviews
  git -C "$repo" commit -qm "Seal converged self-review receipt"
  echo committed
else
  echo unchanged
fi`, 60)
		ctx.Require(commit.ExitCode == 0, "the sealed receipt is committed with the change", commit)

		return ctx.Complete(map[string]any{
			"mode":          "converged",
			"receipt":       sealed.Sealed,
			"fingerprint":   sealed.Fingerprint,
			"reviewed_tree": sealed.ReviewedTree,
			"measures":      measures,
			"receipt_state": commit.Stdout,
			"guidance":      "push the branch; the review-verified CI job verifies the receipt deterministically",
		})
	})
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

func (o statusOutput) measures() []int {
	measures := make([]int, 0, len(o.Observation.Rounds))
	for _, round := range o.Observation.Rounds {
		measures = append(measures, round.Measure)
	}
	return measures
}

// treeDrifted reports whether the current reviewed tree moved past the last
// recorded round's tree — the condition under which a converged instance
// needs a fresh generation before sealing.
func (o statusOutput) treeDrifted() bool {
	rounds := o.Observation.Rounds
	if len(rounds) == 0 {
		return false
	}
	return rounds[len(rounds)-1].ReviewedTree != o.Observation.ReviewedTree
}

// status observes the committed control state; refusals here are conditions
// only a human can change.
func status(ctx *yield.Context, id string) statusOutput {
	result := ctx.RunCommand(id,
		prelude+`"$reviewer" status --repo "$repo" --base "$base"`, 120)
	ctx.Require(result.ExitCode == 0, "the review control state is observable", result)
	var observed statusOutput
	if err := json.Unmarshal([]byte(result.Stdout), &observed); err != nil {
		ctx.Require(false, "the status output decodes", map[string]any{"error": err.Error()})
	}
	ctx.Require(!observed.ProgramStale,
		"the committed state belongs to the active review program (reset required otherwise)", observed)
	ctx.Require(!observed.Observation.WorktreeDirty,
		"the worktree has no uncommitted tracked changes", observed)
	return observed
}

func show(ctx *yield.Context, id string) showOutput {
	result := ctx.RunCommand(id,
		prelude+`"$reviewer" show --repo "$repo" --base "$base"`, 60)
	ctx.Require(result.ExitCode == 0, "the latest recorded review is shown", result)
	var shown showOutput
	if err := json.Unmarshal([]byte(result.Stdout), &shown); err != nil {
		ctx.Require(false, "the show output decodes", map[string]any{"error": err.Error()})
	}
	return shown
}
