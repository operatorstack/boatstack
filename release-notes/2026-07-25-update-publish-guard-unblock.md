### You can publish a Boatstack version update from inside a session

The version-update publisher now runs inside a guarded session. Before this fix, the safety
hook denied `boatstack-helper publish-update-pr` every time. It reported a direct
delivery-state mutation. The publish then only worked from outside the session.

The cause was a false match. The publisher reads the update preview file. That file lives
under `.git/boatstack/`. The tamper guard saw the path in the command and blocked the whole
command, even though the path is a read argument to a trusted command.

The fix adds a narrow exemption for the sanctioned publisher, the same way `publish-pr` is
already trusted. The exemption covers only `boatstack-helper publish-update-pr`. It is
anchored end to end, so you cannot chain a second command after it. A direct write to any
`.git/boatstack/` path, such as `rm` of the delivery state, is still denied.

The denial message is also clearer. If the guard blocks a real write, it names the command
that owns the state and points to `boatstack-helper diagnose-hook` for a false positive.
