### Diagnose malformed coding-host events without blaming the command

Boatstack now distinguishes an undecodable coding-host hook event from a detected irreversible operation. Agents retry once, then stop repeated shell attempts, preserve edits, and use a canonical external hook diagnostic that separates healthy Boatstack installation state from a broken live host payload without exposing commands or secrets.
