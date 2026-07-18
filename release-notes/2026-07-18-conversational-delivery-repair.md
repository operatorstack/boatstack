### Conversational delivery repair

Boatstack now keeps the delivery connected when a developer spots a defect, missing check, visual issue, or changed requirement after Build has started.

- Ordinary chat can route changes into the active managed delivery without requiring the developer to reconstruct context or learn a separate repair process.
- Each observation is recorded outside the conversation with its classification, evidence, repair attempt, and earliest safe resume stage.
- Existing test and review gates are rerunnable; stale receipts are superseded rather than erased.
- Material requirement changes return to human approval and receive a new plan lock.
- Published deliveries remain immutable; later corrections use a linked child delivery with independent gates.

The public README now explains this repair loop with a compact decision table and lifecycle diagram.
