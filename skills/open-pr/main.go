// Skill workflow: open the pull request for the current branch with a
// Boatstack-structured description.
//
// Opening a pull request is a governed boundary, not a formatting step: the
// workflow refuses unless the committed review attestation verifies for the
// exact head tree (the same check CI performs), so a pull request can only
// be opened for a head whose self-review converged and was sealed. The
// description is assembled deterministically from agent-drafted sections
// plus facts the workflow gathers itself: the commit list, the attestation
// binding, and any residual (non-blocking) findings of the converged review.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/operatorstack/yield/sdk/yield"
)

// prelude prepares every command's execution context. Commands run with the
// skill directory as the working directory. When the test sentinel exists
// (created by fixtures/setup.sh under yskill's fixture runner), commands
// operate on a scratch repository with a local bare remote and a stub `gh`,
// so tests never push anywhere real and never open a real pull request.
//
// The reviewer is rebuilt in every command from the current tree, matching
// the self-review skills.
const prelude = `set -eu
root="$(git rev-parse --show-toplevel)"
if [ -f fixtures/tmp/active ]; then
  repo="$PWD/fixtures/tmp/repo"; base=main; gh="$PWD/fixtures/tmp/bin/gh"
else
  repo="$root"; base=origin/main; gh=gh
fi
tmp="${TMPDIR:-/tmp}/boatstack-open-pr"
mkdir -p "$tmp"
reviewer="$tmp/boatstack-reviewer"
go build -C "$root/boatstack" -o "$reviewer" ./cmd/boatstack-reviewer
`

const draftSchema = `{
  "type": "object",
  "required": ["title", "boundary", "transition", "evidence"],
  "properties": {
    "title": {"type": "string", "minLength": 8, "maxLength": 72},
    "boundary": {"type": "string", "minLength": 1},
    "transition": {"type": "string", "minLength": 1},
    "evidence": {"type": "string", "minLength": 1}
  },
  "additionalProperties": false
}`

type verifyOutput struct {
	Verified           bool     `json:"verified"`
	ReceiptPath        string   `json:"receipt_path"`
	ProgramFingerprint string   `json:"program_fingerprint"`
	Failures           []string `json:"failures"`
}

type attestation struct {
	ReviewedTree       string `json:"reviewed_tree"`
	ProgramFingerprint string `json:"program_fingerprint"`
}

type showOutput struct {
	Mode   string          `json:"mode"`
	Review json.RawMessage `json:"review"`
}

func main() {
	yield.Main(func(ctx *yield.Context) (yield.Outcome, error) {
		build := ctx.RunCommand("build-reviewer",
			prelude+`true`, 600)
		ctx.Require(build.ExitCode == 0, "boatstack-reviewer builds from the current tree", build)

		// Preconditions: a named branch, a clean tracked worktree, and
		// commits ahead of the base. Each violation names itself.
		preconditions := ctx.RunCommand("preconditions",
			prelude+`branch="$(git -C "$repo" symbolic-ref --short HEAD)"
[ "$branch" != main ] && [ "$branch" != master ] || { echo "the base branch itself cannot become a pull request" >&2; exit 1; }
[ -z "$(git -C "$repo" status --porcelain --untracked-files=no)" ] || { echo "the worktree has uncommitted tracked changes; commit or stash them first" >&2; exit 1; }
ahead="$(git -C "$repo" rev-list --count "$base..HEAD")"
[ "$ahead" -gt 0 ] || { echo "the branch has no commits ahead of $base" >&2; exit 1; }
printf '%s\n' "$branch"`, 60)
		if preconditions.ExitCode != 0 {
			return yield.Outcome{}, ctx.Refused(strings.TrimSpace(preconditions.Stderr))
		}
		branch := strings.TrimSpace(preconditions.Stdout)

		// Gate: the committed attestation must verify for the exact head
		// tree — the same deterministic check CI performs. An unverified
		// head has no business becoming a pull request.
		verify := ctx.RunCommand("verify-attestation",
			prelude+`"$reviewer" verify --repo "$repo" --dir "$repo/.github/reviews" --base "$base" --head HEAD`, 120)
		if verify.ExitCode != 0 {
			return yield.Outcome{}, ctx.Refused(
				"the review attestation does not verify for this head; run skills/self-review-solve to converge and seal, commit the attestation, then retry: " +
					strings.TrimSpace(verify.Stderr))
		}
		var verified verifyOutput
		if err := json.Unmarshal([]byte(verify.Stdout), &verified); err != nil {
			return yield.Outcome{}, err
		}
		ctx.Require(verified.Verified, "the committed attestation verifies for the head tree", verified)

		receipt := ctx.RunCommand("read-attestation",
			prelude+`cat `+shellQuote(verified.ReceiptPath), 30)
		ctx.Require(receipt.ExitCode == 0, "the committed attestation is readable", receipt)
		var attested attestation
		if err := json.Unmarshal([]byte(receipt.Stdout), &attested); err != nil {
			return yield.Outcome{}, err
		}

		// Evidence the description is assembled from: the exact commit
		// list, the change shape, and any release notes the branch adds.
		commits := ctx.RunCommand("commits",
			prelude+`git -C "$repo" log --reverse --format='- %s' "$base..HEAD"`, 60)
		ctx.Require(commits.ExitCode == 0, "the branch commit list is readable", commits)
		diffstat := ctx.RunCommand("diffstat",
			prelude+`git -C "$repo" diff --stat "$base..HEAD" | tail -30`, 60)
		ctx.Require(diffstat.ExitCode == 0, "the branch diffstat is readable", diffstat)
		notes := ctx.RunCommand("release-notes",
			prelude+`git -C "$repo" diff --name-only --diff-filter=A "$base..HEAD" -- release-notes/ || true`, 60)

		// Residuals of the converged review are part of the story the
		// reviewer of the pull request should see. Best effort: a missing
		// or non-converged local instance simply contributes none.
		var residuals []string
		if shown := ctx.RunCommand("residuals",
			prelude+`"$reviewer" show --repo "$repo" --base "$base" || true`, 60); shown.ExitCode == 0 {
			var out showOutput
			if err := json.Unmarshal([]byte(shown.Stdout), &out); err == nil && out.Mode == "converged" {
				residuals = residualTitles(out.Review)
			}
		}

		instruction := "Draft the pull request description sections for this branch. " +
			"boundary: one short paragraph naming the durable control boundary this change crosses and why. " +
			"transition: what the system refused or allowed before versus after, stated as behavior, not file names. " +
			"evidence: the checks that establish the change works — suites run, regression tests added, and what each proves. " +
			"title: imperative, at most 72 characters. Write for a reviewer who has not followed the work."
		draft := ctx.AgentTask("draft", instruction,
			map[string]any{
				"branch":            branch,
				"commits":           strings.TrimSpace(commits.Stdout),
				"diffstat":          strings.TrimSpace(diffstat.Stdout),
				"release_notes":     strings.TrimSpace(notes.Stdout),
				"attestation":       attested,
				"residual_findings": residuals,
			},
			json.RawMessage(draftSchema))
		var sections struct {
			Title      string `json:"title"`
			Boundary   string `json:"boundary"`
			Transition string `json:"transition"`
			Evidence   string `json:"evidence"`
		}
		if err := json.Unmarshal(draft, &sections); err != nil {
			return yield.Outcome{}, err
		}

		body := assembleBody(sections.Boundary, sections.Transition, sections.Evidence,
			strings.TrimSpace(commits.Stdout), attested, residuals)
		writeChunked(ctx, "title", "$tmp/pr-title.txt", []byte(sections.Title))
		writeChunked(ctx, "body", "$tmp/pr-body.md", []byte(body))

		push := ctx.RunCommand("push",
			prelude+`git -C "$repo" push -u origin `+shellQuote(branch), 300)
		if push.ExitCode != 0 {
			return yield.Outcome{}, ctx.Blocked("the branch did not push: " + strings.TrimSpace(push.Stderr))
		}

		create := ctx.RunCommand("create-pr",
			prelude+`cd "$repo" && "$gh" pr create --title "$(cat "$tmp/pr-title.txt")" --body-file "$tmp/pr-body.md"`, 300)
		if create.ExitCode != 0 {
			return yield.Outcome{}, ctx.Blocked("the pull request was not created: " + strings.TrimSpace(create.Stderr))
		}

		return ctx.Complete(map[string]any{
			"pull_request":        strings.TrimSpace(create.Stdout),
			"title":               sections.Title,
			"branch":              branch,
			"reviewed_tree":       attested.ReviewedTree,
			"program_fingerprint": attested.ProgramFingerprint,
			"residual_findings":   residuals,
		})
	})
}

// assembleBody renders the fixed pull-request structure: the agent owns the
// prose of each section, the workflow owns the facts.
func assembleBody(boundary, transition, evidence, commits string, attested attestation, residuals []string) string {
	var b strings.Builder
	b.WriteString("## Boundary\n\n" + strings.TrimSpace(boundary) + "\n\n")
	b.WriteString("## Transition\n\n" + strings.TrimSpace(transition) + "\n\n")
	b.WriteString("## Evidence\n\n" + strings.TrimSpace(evidence) + "\n\n")
	if commits != "" {
		b.WriteString("### Commits\n\n" + commits + "\n\n")
	}
	b.WriteString("### Self-review attestation\n\n")
	b.WriteString("- reviewed tree: `" + attested.ReviewedTree + "`\n")
	b.WriteString("- program fingerprint: `" + attested.ProgramFingerprint + "`\n")
	b.WriteString("- the review-verified CI job re-derives both facts deterministically from the base-admitted policy\n")
	if len(residuals) > 0 {
		b.WriteString("\n### Residual findings (non-blocking, recorded by the converged review)\n\n")
		for _, residual := range residuals {
			b.WriteString("- " + residual + "\n")
		}
	}
	return b.String()
}

// residualTitles lists the non-blocking (P2/P3) finding titles of a review.
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

// writeChunked stages content through bounded base64 chunks so no single
// command string approaches the platform's per-argument size cap, then
// decodes it to the target path (a $tmp-relative shell expression).
func writeChunked(ctx *yield.Context, idPrefix, target string, content []byte) {
	encoded := base64.StdEncoding.EncodeToString(content)
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
			prelude+`printf '%s' '`+encoded[i:end]+`' `+redirect+` "`+target+`.b64"`, 60)
		ctx.Require(written.ExitCode == 0, "the content chunk is staged", written)
	}
	decoded := ctx.RunCommand(idPrefix+"-decode",
		prelude+`base64 -d < "`+target+`.b64" > "`+target+`"`, 60)
	ctx.Require(decoded.ExitCode == 0, "the staged content decodes to its file", decoded)
}

// shellQuote single-quotes a value for safe interpolation into a command.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
