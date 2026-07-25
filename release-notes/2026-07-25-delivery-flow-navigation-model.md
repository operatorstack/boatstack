### Internal design note: delivery-flow navigation modeled as a costed graph

An internal design note now models Boatstack's delivery workflow as a costed directed
graph — states are delivery-flow states, edges are the moves an agent makes, and each move
carries a running cost — so the cost of *navigating the flow* can be measured against a
deterministic shortest-path oracle over the workflow the tool already owns. This is design
research toward a future flow-navigation cost meter; it is not part of the generated
distribution and changes no command, gate, or file that Boatstack produces.
