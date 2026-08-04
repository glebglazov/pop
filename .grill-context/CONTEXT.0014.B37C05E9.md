+ Authoring guide
  A read-only command that prints how to hand-author one Work kind's files —
  storage layout, every file template, the manifest's fields with their allowed
  values, **and the kind's judgment rules** (for a Task set: HITL/AFK typing
  including the split-the-slice rule and the two legitimate HITL positions, the
  effort heuristic, the vertical-slice framing, the Orientation rule). Its
  enums, filename patterns and marker strings are generated from the same
  constants the validator reads, so the printed rules cannot drift from the
  enforced ones — asserted by test, not assumed. It is **authoritative**: the
  **Work store doc** deletes what the guide covers and keeps only what no
  generated text can carry, and `register`'s `-h` stays a flag reference that
  points at the guide. One verb per kind — `pop map authoring-guide`,
  `pop tasks authoring-guide` — never a `pop work` umbrella. Describes the
  artifact, not a workflow, so it serves initial authoring, `to-spec`, and
  mid-drain `index.json` edits alike.
  avoid: schema command, doctrine flag, manifest help, authoring API
  under: Work store

~ Work store doc
  `integrate/issue-tracker.md`, embedded in the binary and seeded to the
  user-level issue-tracker doc, resolved two-layer by planning skills (a repo's
  `docs/agents/issue-tracker.md` wins when present). After the authoring guides
  land it carries **behavioural rules only** — store resolution, `pop tasks
  register`'s flag and keyword semantics, the artifacts-must-be-committed rule,
  claiming, resolution and its ticket-type overrides, handoff, and the
  Map-sourced-set minting obligation — plus a pointer at each kind's **Authoring
  guide**. The two-layer override therefore governs *store choice and
  behavioural conventions*, not authoring shape: a repo doc redefining manifest
  fields or enums is a no-op, because the validator enforces the binary's
  version regardless.
  was: the document describing how planning skills publish to pop's Work store,
  covering storage layout, file templates, manifest fields and behavioural rules
  alike.
  under: Work store

~ Task-set manifest validation
  `validateManifest` (`tasks/manifest.go`) reports the whole fix list at once:
  empty tasks array, missing or duplicate id, non-root or duplicate file,
  missing markdown, a missing/duplicated/checkbox-less acceptance-criteria
  section, invalid type, invalid effort (empty defaults to `standard`), missing
  or invalid status, a persisted `in_progress`, an unresolved blocker — and a
  markdown file in the set folder with **no manifest entry**, excluding
  `spec.md`. That last check mirrors the Map validator's and catches the silent
  failure where an author writes a slice file and forgets its manifest entry,
  leaving a set that registers `READY` with an invisible task. Its enums are the
  ones the **Authoring guide** prints, from the same constants.
  was: `validateManifest` (`tasks/manifest.go`) reports the whole fix list at
  once: empty tasks array, missing or duplicate id, non-root or duplicate file,
  missing markdown, a missing/duplicated/checkbox-less acceptance-criteria
  section, invalid type, invalid effort, missing or invalid status, a persisted
  `in_progress`, and an unresolved blocker. Unlike the Map validator it does not
  detect an orphan markdown file, because a set folder is flat and holds
  `spec.md` alongside the task files.
  under: Task set
