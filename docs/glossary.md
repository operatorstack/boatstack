# Glossary

These terms describe the Boatstack model. Exact representations belong in the
reference documentation.

| Term | Definition |
| --- | --- |
| Admission | Final recheck that an exact prescribed operation remains permitted before its effect. |
| Actor | Concrete human subject recorded at an approval boundary. An actor string alone is not authority. |
| Authority | Trusted evidence that permits admission under a declared control boundary. |
| Authority receipt | Time- and subject-bound evidence exposing exact capabilities. It is distinct from a transition receipt. |
| Capability | Permission exposed by authority at an enforceable boundary. |
| Control law | Deterministic relation deciding which state transitions may be selected and committed. |
| Control Program | Complete canonical executable control law: transitions, targets, entries, authority, invocation, verification, and recovery contracts. |
| Control state | Durable supervisory state committed by the general kernel. |
| Delegation | Run-scoped grant produced by a trusted mechanism for a declared authority class. |
| Domain | Implementation supplying observations, admissibility, operators, effects, and verification outside the general kernel. |
| Effect | Bounded state-changing facts or mutations produced by an operator. |
| Entry | Named invocation surface selecting one target and its inputs. |
| Evidence | Observed facts used by a verifier or admission boundary. Evidence is not automatically authority. |
| Flow | Product-facing name for a complete Control Program authored for a domain. |
| Foreground work | Bounded, resumable production of candidate artifacts under a declared contract. |
| Freshness | Equality of the state, program, observation, objective, authority, and context bound by a prescription. |
| Host | Runtime invocation surface enabled by configuration. |
| Identity descriptor | Trusted description used by a host to resolve and present a proposed actor. |
| Identity role | Flow-selected functional name resolved by project configuration. It is not a person or approval. |
| Invocation | Materialized entry, target, program, inputs, repository, and run lineage. |
| Marked state | Program-defined accepted completion state. |
| Objective | External versioned intent being controlled. Only an exact binding enters control state. |
| Objective binding | Identity, revision, and fingerprint of the exact objective retained in control state. |
| Observation | Canonical report of current domain state. It is not durable supervisory state. |
| Operator | Component that realizes one admitted operation. |
| Parameter producer | One admissible source for a required operator parameter. |
| Prescription | Content-bound proposal to apply one selected transition under exact freshness inputs. It carries no authority. |
| Projection | Generated host-native presentation of a Control Program entry. |
| Receipt | Immutable transition fact emitted only after verification and atomic commit. |
| Reconciliation | Controlled operation that resolves known drift or uncertain external settlement. |
| Recovery | Explicit supervisory state entered when an effect may have happened but safe settlement is unknown. |
| Run | One exact program, entry, target, input, repository, and execution lineage. |
| State | Context-dependent term; use *control state* or *domain state* when the owner matters. |
| Supervisor | Mechanism selecting admissible transitions and accepting verified results. |
| Surface | CLI, RPC, MCP, SDK, or host adapter through which the same controller is invoked. |
| Target | Marked predicate defining accepted completion for an entry. |
| Transition | Candidate relation between current/observed state and an accepted next state. |
| Verification | Fresh post-effect check deciding whether a candidate consequence may commit. |

Common distinctions are expanded in the concept documents: [authority and
capability](concepts/authority-identity-and-delegation.md), [objective, target,
observation, and state](concepts/state-observation-objectives-and-targets.md),
[transition, operator, and effect](concepts/transitions-operators-effects-and-capabilities.md),
and [evidence, verification, receipt, recovery, and reconciliation](concepts/prescriptions-verification-receipts-and-recovery.md).
