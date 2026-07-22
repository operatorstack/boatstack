### Verify the shipped repository layout before sync

Boatstack's source checks now recognize both the canonical lab layout and the projected public-document layout without weakening documentation coverage. The projection suite also runs the generated repository's Go tests before publication, so source-only filesystem assumptions fail in Intelligence Flow instead of surfacing later in the separate Boatstack sync pull request.
