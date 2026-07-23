### Canonical update ownership supervision

- Reuse one semantic ownership projection across update admission, mutation verification, staging, and preview, including marker-bounded Cursor, Claude, and Gemini interceptors.
- Validate branch and workspace preconditions before creating a durable operation so rejected setup cannot consume retry budget or collide with the corrected attempt.
- Preserve the underlying rollback reason in the operation receipt and cover the reported stale-interceptor upgrade end to end.
