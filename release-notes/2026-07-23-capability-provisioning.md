### Boatstack helps repositories that cannot yet produce evidence

When a feature needs visual evidence but the repository has no command to produce
it, Boatstack now guides you to set one up instead of silently leaving a gap. A
new `provision-capability` operation detects the repository's frontend stack
(framework, package manager, and whether Playwright, Cypress, or Storybook are
present) and returns a context-aware guide: the framework-agnostic contract the
in-repository capture harness must satisfy, plus stack-tailored steps to build
it. Boatstack ships the contract and the guide, never the harness itself.

`capability-register` then records the repository-owned command in one step,
keeping the canonical `.boatstack-project.json` source and every generated file
in sync (or round-tripping the generated configuration alone when there is no
source). Once registered, provisioning reports the capability as available and
`capture-evidence` can run.

Planning is aware of this too: when a visual scenario is relevant but no capture
command resolves, auto-plan surfaces a material provisioning decision with tiered
paths — provision now as its own delivery slice, bundle the harness into the
feature slice, or record the gap and defer. It remains a surfaced choice;
Boatstack never imposes a frontend framework.
