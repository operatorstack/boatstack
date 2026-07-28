### Status now shows where your published PR actually stands

After you publish, `next-status` and `recovery-status` observe the live pull request and report its position: checks still running, checks failing (with the failing check names), changes requested, a required review still owed, or clean and eligible to merge. Before this, a published feature reported only open/merged/closed, and you had to open GitHub to learn why an open PR was not moving.

The new detail comes from the same single read-only GitHub lookup Boatstack already performed, and it is never stored: anything the observation cannot classify with certainty is reported as unknown and left for you, and your delivery records are written only when the PR reaches a terminal merged or closed state, exactly as before. If your `gh` version does not support the richer lookup, status falls back to the previous behavior unchanged.
