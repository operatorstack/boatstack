### Compose software-delivery Flows without repeated wiring

Repositories can use the new `softwareDelivery` helper to derive canonical domain wiring while keeping lifecycle steps, priorities, work, targets, entries, authority, and delegation explicit. Additional work is bound by contract ID on the repository-selected lifecycle step, and unknown or unreferenced contracts fail before compilation.
