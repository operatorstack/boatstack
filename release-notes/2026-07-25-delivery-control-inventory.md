### Internal design note: delivery transitions inventoried toward a flow-navigation meter

A source-cited inventory of Boatstack's delivery state machine now records every delivery-state
state, the operations that change it, and — for each — its guard, authority, receipt, recovery path,
and cost class. It is the faithful map the earlier costed-graph note pointed to: the single source of
truth a future flow-navigation cost meter will mirror, and the baseline a conformance check will keep
honest as the workflow evolves. This is design research staged before any implementation; it is not
part of the generated distribution and changes no command, gate, or file that Boatstack produces.
