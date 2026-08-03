+ Agent-loaded skill
  An embedded skill another skill's body tells the model to load, so it carries no
  `disable-model-invocation` — grill-with-map and batch-grill-me (loaded by a
  wayfinding ticket and by the grilling skills that compose the interview
  primitive), plus the Tool skills prototype and research. Its counterpart is a
  **human-opened** skill, which keeps the flag because a human decides when the
  session starts: grill-with-docs, grill-consolidate, setup-matt-pocock-skills,
  spend-audit, to-spec, to-tasks, wayfinder. The axis is *who loads it*, not
  whether it is session-shaped: grill-with-map is a whole session and still
  agent-loaded, because the only thing that ever opens it is a Decision ticket.
  Classification is a property of the skill, decided once per embedded skill and
  recorded in its overlay header when it contradicts upstream's frontmatter — never
  worked around by composing slash-command text in a pop verb.
  avoid: model-invoked skill, auto-triggered skill, manual-only skill
  under: Task planning skills

~ Workflow skill
  An embedded skill that is a session-shaped workflow someone opens deliberately —
  batch-grill-me, grill-with-docs, grill-with-map, grill-consolidate, to-spec,
  to-tasks, wayfinder. Session shape says nothing about who opens it: that is the
  separate **Agent-loaded skill** axis, on which batch-grill-me and grill-with-map
  are agent-loaded and the rest human-opened. The counterpart of a Tool skill; the
  two kinds together make up the Task planning skills.
  was: An embedded skill that is a session-shaped workflow the user opens
  deliberately: manual-invocation-only via `disable-model-invocation` —
  batch-grill-me, grill-with-docs, grill-with-map, grill-consolidate, to-spec,
  to-tasks, wayfinder. The counterpart of a Tool skill; the two kinds together
  make up the Task planning skills.
  under: Task planning skills

~ Tool skill
  An embedded skill that is a general-purpose instrument rather than a session
  workflow — prototype and research, adopted verbatim from upstream. Both are
  **Agent-loaded skills**, so they auto-trigger when the conversation shape
  matches, but the instrument/workflow distinction is about shape, not
  invocability. Callers such as the wayfinder skill compose tool skills by naming
  them; caller-side packaging rules (where the output lands — e.g. a Decision
  ticket's `## Answer`) live in the caller, never in the tool itself.
  was: An embedded skill that is a general-purpose instrument, not a session
  workflow: model-invoked (no `disable-model-invocation`), so it auto-triggers when
  the conversation shape matches — prototype and research, adopted verbatim from
  upstream. Callers such as the wayfinder skill compose tool skills by naming them;
  caller-side packaging rules (where the output lands — e.g. a Decision ticket's
  `## Answer`) live in the caller, never in the tool itself.
  under: Task planning skills
