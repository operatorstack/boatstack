### Phased plans now preserve every delivery gate

Boatstack now treats ordinary plan phases as internal work unless the plan explicitly declares PR-sized delivery slices. Each declared slice must independently pass its build, test, review, and confirmed ship flow, preventing plan approval from authorizing mid-build pull requests for later phases.
