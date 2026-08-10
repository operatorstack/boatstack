### Allow verified detached Boatstack transitions

Detached Boatstack commands now pass the ambient safety hook when the exact repository-bound helper owns the current workflow transition. Direct edits to controller state, spoofed helpers, cross-repository paths, and stage-invalid transitions remain denied.
