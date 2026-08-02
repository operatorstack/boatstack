### Detached workflows use one controller root

Detached Boatstack workflows now read, write, and verify generated feature state under the same external controller root.

Use `boatstack-helper attach --repo . --force` once to import a valid older embedded open-feature package. Boatstack verifies fingerprints and copies it atomically. It stops on conflicting packages or stale receipts and does not delete the embedded source.
