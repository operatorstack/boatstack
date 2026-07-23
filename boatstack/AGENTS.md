# Agent guide — Boatstack product-engineering-loop

Read this before opening a PR that touches anything under
`labs/12-product-engineering-loop/`.

## Every PR that changes this lab REQUIRES a new release note

CI runs `scripts/release_notes.py check-policy` (the **Generated distribution**
check). It fails the PR if the diff touches **any** file under
`labs/12-product-engineering-loop/` — source, tests, docs, scripts, anything —
without **adding** a new release-note fragment. This check is **not** part of
`go test ./...`, so a green local test run does **not** mean you are done. Skipping
the note costs a full CI round trip (fail → add note → push → re-run).

### Add the note in the same commit as your change

Create one new file per PR:

```
labs/12-product-engineering-loop/boatstack-distribution/release-notes/YYYY-MM-DD-<slug>.md
```

Contract (enforced by `validate_release_note`):

- **Name:** `YYYY-MM-DD-<slug>.md`, slug lowercase `[a-z0-9]` words joined by `-`.
- **First line:** a level-three Markdown heading — `### <Title>` (no leading blank line).
- **Body:** at least one non-empty line after the heading describing **user impact**
  (what changed for someone using Boatstack, not the code mechanics).
- **Encoding/EOL:** UTF-8, and the file must end with a trailing newline.
- **Append-only:** never edit or delete an existing note. To correct a shipped
  note, add a new correction fragment. Only added (`A`) files under
  `release-notes/` are allowed in the diff.

### Verify locally before you push — avoid the CI round trip

Commit your change **and** the note, then run the same policy CI runs:

```
# format check on the notes directory
python3 labs/12-product-engineering-loop/scripts/release_notes.py \
  validate --root labs/12-product-engineering-loop/boatstack-distribution/release-notes

# append-only + "note present for lab changes" against origin/main (needs a clean,
# committed tree — it inspects the committed PR diff, not the working tree)
python3 labs/12-product-engineering-loop/scripts/release_notes.py \
  preflight --repo . --base-branch main
```

`preflight` fetches `origin/main` and checks the committed diff. `PASS` means the
**Generated distribution** check will pass; `BLOCKED` prints exactly what to fix.

## Other checks that are not in `go test`

- **Repository conformance** and **Runtime** (windows/macos/ubuntu) run in CI.
  Locally, always run `go build ./...`, `go vet ./...`, and `go test ./...` from
  `product-engineering-loop`, plus `python3 -m unittest tests.test_product_loop`
  from `labs/12-product-engineering-loop` for the Python surface.

## PR body honesty

Do not write "no release note required" for a change under this lab — a note is
always required. State which note you added.
