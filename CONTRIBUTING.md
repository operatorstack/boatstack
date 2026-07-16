<!-- Generated from operatorstack/intelligence-flow. -->

# Contributing

Boatstack is a generated content distribution. Propose changes to workflow semantics, templates, evidence rules, or generated presentation in [Intelligence Flow](https://github.com/operatorstack/intelligence-flow/tree/46be4fd2d8ebbc00e28c10e78685b721b2c62fe8/examples/12-product-engineering-loop).

The Boatstack repository receives product/runtime changes through a generated pull request. Review the PR's `UPSTREAM.json`, tests, adapter diff, and context-size change; do not hand-edit generated output on `main`. `.github/workflows` is the exception: it is Boatstack's executable control plane, excluded from scheduled projection and changed only through a separate manually reviewed Boatstack PR.

Repository-specific examples and outcome reports can be proposed upstream as new evidence. A failure becomes a durable move only after its mechanism and non-regression gate are documented.
