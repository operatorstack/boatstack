### The merged-goal pursuit now has explicit limits, and stops when it hits one

With `delivery.terminal: merged`, the flow pursues your pull request only inside a clear contract: at most three recorded post-publish fix cycles, no reviewer asking for changes, and a branch that merges cleanly. When any of those ends — the budget is spent, changes are requested, the base conflicts — the pursuit pauses: the step comes back to you, nothing further is prescribed, and the pause is remembered so it still holds tomorrow in a fresh session, even offline.

Recording the next correction is the explicit reset that starts a fresh cycle. The status reason always tells you why the pursuit paused. With the default `published` goal, none of this bookkeeping is evaluated or written.
