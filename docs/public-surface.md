# Public-surface contract

## Outcome

A new user should understand three facts before implementation detail:

1. Boatstack is one delivery controller, not a collection of host prompts.
2. Managed effects need exact evidence and authority.
3. Boatstack is a flag-day replacement with no V1 compatibility path.

## Required public evidence

Every material claim names:

- the user problem;
- the controlling boundary;
- the implementation path;
- an executable verification path;
- the evidence status.

`docs/public-claims.json` is machine-checked. A verified claim may reference
only paths that exist at the current head. Formal Locus results remain advisory
or theorem-only unless executable code evidence discharges their assumptions.

## Writing

Lead with the result. Use short, direct sentences. Keep historical V1 behavior
in release notes or the explicit authority inventory, not in current operating
guides. Never include private consumer identities, paths, logs, transcripts, or
screenshots.

## Accessibility

Public SVG assets keep a title, description, and `role="img"`. Command examples
must use registered Boatstack verbs and flags. Links and JSON examples must validate in
the repository contract.
