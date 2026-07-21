### Introduce deterministic DecisionOperator at plan boundary

Replaced the hardcoded validation errors for architecture facts with a deterministic `PlanDecisionOperator` primitive (`Infer`, `Query`, `Verify`, `Reject`, `Escalate`). The `validateArchitectureGrounding` boundary now evaluates repository evidence and human intent against a strict policy matrix before routing the decision. This optimizes the query architecture to only block or query when evidence is insufficient, making the supervisor's decisions formal, recordable, and explainable.
