### Upstream sync now defers merge eligibility to protected checks

Boatstack's generated upstream workflow now asks GitHub for native auto-merge as
the publisher App instead of treating workflow code as the merge-policy engine.
The request fails closed unless `main` has required status checks, so a missing
branch-protection rule cannot turn a newly opened projection PR into an
unchecked merge.

Concurrent mutation and operation locks also now recognize Windows'
`Access is denied` response as normal contention only when the lock file is
present. Real ACL failures still stop immediately, while duplicate workers wait
and converge on the same receipt as they do on Unix.
