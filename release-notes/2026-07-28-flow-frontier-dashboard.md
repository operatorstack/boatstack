### One command now shows every feature, its position, and whose move it is

`flow frontier` renders a read-only dashboard across all of your managed deliveries: each row names the feature, its observed position (building, awaiting review, PR checks failing, eligible to merge, complete), and the actor who owes the next step — you or the agent — with the exact prescribed command when one exists. Earlier slices that are published with a still-open pull request appear as their own rows, so a red check on an already-published slice is visible while a later slice builds.

The dashboard is a pure report: it performs no writes at all, one unverifiable delivery becomes one blocked row instead of hiding your healthy work, and a row's owner always matches what `flow next` would say for the same feature. Before this, reconstructing "where is everything and what is waiting on me" required running status per feature and reading each answer.
