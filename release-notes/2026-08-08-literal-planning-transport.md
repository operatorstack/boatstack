### Planning documents now cross guarded shells safely

Boatstack now accepts a complete planning document through a literal Bash, Git Bash, or PowerShell envelope while treating the Markdown as data. PowerShell encoding markers are removed at the planning-input boundary. Missing input, truncated bodies, delimiter collisions, expansion-capable or compound forms, and leading or trailing commands stop before execution with a corrective message, so users are no longer asked to paste planning text into a shell or restart the task.
