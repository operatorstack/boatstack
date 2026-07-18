### Updates migrate healthy host hooks safely

Boatstack updates now validate Claude, Codex, and Cursor safety hooks against the committed fragment from the installed release before applying the incoming template. Intentional hook wording or structure changes can migrate normally, while genuine local hook edits still block replacement for review.
