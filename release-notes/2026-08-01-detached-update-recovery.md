### Detached update recovery

Boatstack updates can now recover from an invalid local development install lock by using the repository's committed stable pin as the prior release identity. Detached operation receipts and shared runtimes carry their external ownership boundary through validation, preventing the updater from rejecting its own controller state as a repository escape. Symlink and mixed-ownership checks remain fail closed.
