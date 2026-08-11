### Pin each repository to an immutable runtime

Boatstack now records a portable version-and-checksum runtime identity in each
repository. A stable dispatcher verifies and executes only that exact artifact
from an immutable host store, so installing another release cannot silently
change an existing repository. Updates durably stage the candidate before the
Kernel atomically commits the new pin, and missing artifacts fail closed.
