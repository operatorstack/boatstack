### Route post-publication failures into corrective delivery automatically

Boatstack now distinguishes a published PR from a verified merged feature. CI
failures, review findings, ordinary correction requests, and denied publication
attempts resolve against the current branch and recorded PR, then prepare an
independently approved corrective child without requiring users to know a repair
command. Safety denials identify the blocking delivery and never recommend that
the user repeat the denied push manually.
