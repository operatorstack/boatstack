### Enforce durable state facet ownership

Boatstack now classifies durable state as installation, program, control, or
product state. Every native, repository-program, and recovery commit fails
closed if it changes a facet outside the kernel-owned transition policy, and
committed receipts record the exact changed facets.
