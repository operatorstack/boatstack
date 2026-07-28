### `retro derive` turns your repeated instructions into reviewable proposals

The new `retro derive` command reads the transcript files you name — Claude Code sessions, plain-text logs, or a neutral event format — finds the instructions you keep giving across sessions, and classifies each one as a missing observation, verb, setpoint, or guard, with a suggested typed promotion. An instruction the classifier cannot place is still shown, marked unclassified, and generates no proposal.

The command only proposes: it writes no file, changes no state, and runs nothing, and it reads only the transcripts you explicitly pass — Boatstack never scans for transcripts on its own. Promote a proposal by hand through the normal reviewed delivery flow. The idea behind it: an instruction you keep repeating is evidence your system is missing a typed control, and the fix is to add that control — not to save the prompt.
