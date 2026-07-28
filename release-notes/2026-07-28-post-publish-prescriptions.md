### With a merged goal, the flow now walks your PR from open to merged

When `delivery.terminal` is `merged`, the flow advisors keep naming the next step after you publish, from the live pull-request observation: checks still running prescribes `flow watch`; failing checks prescribe the exact correction command with the failing check names attached; a merge-eligible PR (green checks, satisfied reviews, clean merge state) prescribes the exact `gh pr merge` command. Each of these is the agent's step, so a status reply hands it over with one key instead of assigning you the waiting and the fixing.

Boatstack itself never merges: the merge command is prescribe-only, refused categorically by the execute driver, and runs only under your host's own permissions. A required review approval, a changes-requested verdict, a closed PR, or an unverifiable position always comes back to you. With the default `published` goal, nothing changes at all.
