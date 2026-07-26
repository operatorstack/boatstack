### Internal design note: hidden J_flow and the oracle-as-advisor pattern

This change adds an internal design note under the lab `notes/` directory. The note names a
class of problem: a subproblem where the tool already owns a deterministic check but exposes
it only as a terminal accept-or-reject gate, so a coding agent re-discovers the answer by
trial and error. The note defines the class, a regret metric, and the "oracle-as-advisor"
pattern that turns a checker into a constructive guide.

There is no change to any shipped command, gate, runtime, or generated file. The `notes/`
directory is not part of the Boatstack distribution. This fragment records the addition to
keep the append-only release-note history complete.
