### You can now wait for a pull request to move without polling it yourself

`flow watch` observes your delivery frontier on an interval and exits the moment something changes: checks finish or fail, a review lands, a merge happens. It also exits immediately when nothing can move, and with a distinct exit code when its timeout passes with no change, so a script or an agent loop can tell "something happened" from "still waiting". Defaults are a 30-second interval and a 30-minute timeout, both adjustable.

The watch only observes: it performs no writes and never runs an operation on your behalf. When it exits, run `next-status` and continue from the fresh state. Before this, waiting on CI meant either re-running status by hand or asking your agent to poll GitHub in prose.
