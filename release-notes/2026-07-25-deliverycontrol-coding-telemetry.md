### Coding-effort telemetry, held separate from flow regret (J = J_flow + J_coding)

Delivery reports can now carry coding effort (J_coding) beside navigation cost (J_flow), recorded from
its own best-effort telemetry log rather than derived from the delivery graph. A recorded correction
marks one unit of coding effort; the two costs are stored separately and reported side by side.

The separation is enforced by construction: J_coding is never summed into J_flow, never enters the
regret figure, and is never modeled as a graph, optimized, or gated. Only navigation of the
deterministic delivery state machine is ever optimized; the work of writing a fix is measured, not
steered. The telemetry honors the same kill switch as the trajectory trace and never changes any
command's behavior or exit code.
