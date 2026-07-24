### Boatstack can render visual evidence inline on a private pull request

The programmatic visual-evidence publisher could render screenshots inline only for a
public repository, because it committed the bytes to a public branch and served them
from `raw.githubusercontent.com` — a URL GitHub's image proxy cannot fetch for private
content. On a private repository the publisher declined and left the manual-attachment
fallback in force, so a private PR never got inline screenshots automatically.

A new opt-in mode closes that gap. Set `workflow.visual_evidence_publish.mode` to
`external-host` and Boatstack uploads the exact captured PNG bytes to an anonymous host
(`litterbox`, which auto-expires uploads after a chosen `1h`/`12h`/`24h`/`72h` window,
or permanent `catbox`) and posts the returned URLs inline in the same single,
idempotent Boatstack-owned comment — on a private repository too.

Because the bytes leave the repository to a third party, the mode is never selected
automatically: only that explicit config value turns it on, and the comment carries a
standing reminder naming the host and its expiry so reviewers know the images are
external and temporary. The default behavior is unchanged — a public repository still
gets durable inline evidence from a Boatstack-owned public branch, and any repository
without the opt-in keeps the manual-attachment fallback.
