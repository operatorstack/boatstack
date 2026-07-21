### Prevent E2E architecture grounding failures during auto-plan

Added explicit ZCA projection validation to ensure architecture facts like missing routes are verified by host evidence. The schema has been updated and a new execution boundary state, `PLAN_INVALID`, immediately exits repair loops caused by false planning premises.
