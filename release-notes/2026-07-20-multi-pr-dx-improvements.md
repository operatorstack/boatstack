### Improve developer experience for multi-PR features

Boatstack now surfaces slice progression in multi-PR features by indicating the current slice relative to the total slices (e.g., `(PR 1 of 4)`). This context is shown in the terminal when interacting with the feature and is embedded directly within generated PR descriptions. Additionally, Boatstack update notices are suppressed during intermediate slice builds to ensure developers aren't distracted until the final PR of the feature is published.
