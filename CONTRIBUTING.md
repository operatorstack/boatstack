<!-- Generated from operatorstack/intelligence-flow. -->

# Contributing

Boatstack is a generated content distribution. Propose changes to workflow semantics, templates, evidence rules, or generated presentation in [Intelligence Flow](https://github.com/operatorstack/intelligence-flow/tree/d37975f8c3960b03c701f029d4815f6d6814ed7c/examples/12-product-engineering-loop).

The Boatstack repository receives product/runtime changes through a generated pull request. Review the PR's `UPSTREAM.json`, tests, adapter diff, and context-size change; do not hand-edit generated output on `main`. `.github/workflows` is the exception: it is Boatstack's executable control plane, excluded from scheduled projection and changed only through a separate manually reviewed Boatstack PR.

Repository-specific examples and outcome reports can be proposed upstream as new evidence. A failure becomes a durable move only after its mechanism and non-regression gate are documented.

## Public-facing changes

Any user-facing upgrade must state the user problem, supporting observation or requirement, current evidence status, and the README or guide it changes. If no public document changes, explain why the behavior is internal. Material public claims must appear in `docs/public-claims.json` and link to a readable explanation.

Use Huashu Design for README and beginner-guide review when it is installed. The portable requirements remain in [the public-surface contract](docs/public-surface.md): plain outcomes first, one dominant product journey, progressive disclosure, accessible assets, no invented proof, and explicit separation between verified behavior and outcomes still being evaluated.
