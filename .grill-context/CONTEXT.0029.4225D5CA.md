---
fragment: 4225D5CA
generation: 0029
branch: master
---

~ Work view preset
  A named, self-contained answer to "which rows, in what order" on the Work read
  surfaces — selected one at a time from `[[work.dashboard.tasks.presets]]` (or the
  shipped roster when undeclared). Declares optional `label`, `status`, `unfolded`,
  `archived`, `muted`, `created_within`, `sort` (`created_desc` | `created_asc` |
  `status`, defaulting to `created_desc` when unset), `unanswered` (`admit` |
  `drop`, defaulting to `admit` — see **Unanswered filter field**), and one `hide`
  clause. The first resolved entry is the default; positions `1`–`9` are digit
  shortcuts in the **Work dashboard filter menu**. Session-only on the dashboard;
  `pop work status --preset <name>` names one by name.
  avoid: view filter preset, inclusion preset, dashboard filter preset
  was: The same term without `unanswered` in the declared field list.

+ Unanswered filter field
  A **Work view preset** field the row's own **Work kind** has no answer for —
  `unfolded` on a **Map**, or `created_within` against an identifier that carries no
  date. The kind states the absence by leaving the field unset rather than inventing
  a false answer, which is what a Map did for `unfolded` before this. A preset's
  `unanswered` mode decides what the absence means in a positive field: `admit`
  (the default) ignores it, so a preset judges every kind only on the fields that
  kind can answer; `drop` removes the row, which is what a preset whose whole
  question is Task-set vocabulary asks for — the shipped `unfolded` preset is the
  only one that declares it. Inside a `hide` clause the rule never varies: an
  unanswered field stops the whole clause from firing, because a subtraction must be
  proved on every field it names. One principle both ways round — an unanswered
  field never removes a row on its own.
  avoid: unanswerable field, missing fact, kind-blind filter, null answer
  under: Work
