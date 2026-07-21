### Prevent hallucinated approver names like Sam or Eve in plan approvals

Added explicit anti-hallucination prompt rules to Boatstack skill generators and reference files. When deterministic identity retrieval (e.g., `gh api user`) is unavailable or fails, Boatstack now strictly forbids the AI from inventing placeholder names (like Sam or Eve) and instead requires asking the human developer for their identity.
