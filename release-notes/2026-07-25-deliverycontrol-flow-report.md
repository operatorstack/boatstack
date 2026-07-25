### `boatstack flow report` — per-session flow regret and coding effort

A new read-only `flow report` subcommand renders the current session's delivery-flow navigation from
the shadow logs: how many moves were taken, the observed navigation cost (J_flow), the oracle's cost for
the same start and goal (J_flow*), and the regret between them — with coding effort (J_coding) shown as
a separate figure and never folded into the regret. When the oracle cannot place the session's start
against the goal, the regret line is withheld rather than fabricated.

The report reads only the append-only logs and never fails on an empty session. Its `--json` form is a
stable, public-safe surface suitable for a downstream retro to consume when attributing where a
session's regret concentrated — closing the observe → derive → compare → advise → control loop with a
measurement an operator can actually read.
