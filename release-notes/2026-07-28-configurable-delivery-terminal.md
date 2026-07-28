### You can now tell Boatstack the goal is a merged PR, not just an open one

A new `delivery.terminal` setting names the state a delivery pursues before the flow reports nothing left to do. The default, `published`, keeps today's behavior exactly: the flow ends when your pull request is open. Setting `merged` tells the read-only flow advisors to keep reporting the standing goal until the pull request is observed merged; the prescribed post-publish steps arrive in the next update.

The goal a delivery starts under is saved with that delivery, so changing the setting mid-flight never silently changes an in-progress delivery's goal, and a fresh session hydrates the goal from your repository instead of you restating it. Invalid or unreadable values always resolve to the narrower `published` goal, and Boatstack itself never merges a pull request under any setting.
