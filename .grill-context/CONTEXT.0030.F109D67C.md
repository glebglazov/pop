---
fragment: F109D67C
generation: 0030
branch: master
---

~ Work view preset
  A named, self-contained answer to "which rows, in what order" on the Work read
  surfaces — selected one at a time from `[[work.dashboard.tasks.presets]]` (or the
  shipped roster when undeclared). Declares optional `label`, `status`, `unfolded`,
  `archived`, `muted`, `created_within`, `sort` (`created_desc` | `created_asc` |
  `status`, defaulting to `created_desc` when unset), `unanswered` (`admit` |
  `drop`, defaulting to `admit` — see **Unanswered filter field**), `pin` (boolean,
  defaulting to false — the sole grant of the **Pane pin**; roster position grants
  nothing, so a user roster keeps the pin only by declaring it), and one `hide`
  clause. Of the shipped roster only `active` declares `pin = true`. The first
  resolved entry is the default; positions `1`–`9` are digit shortcuts in the
  **Work dashboard filter menu**. Session-only on the dashboard;
  `pop work status --preset <name>` names one by name.
  avoid: view filter preset, inclusion preset, dashboard filter preset
  was: The same term without `pin` in the declared field list, so every preset
    was pinned alike.

~ Pane pin
  The Work dashboard lifting the rows its launching pane is attributed to out of
  the ordered list and rendering them above it, below only the **Selection area**
  when one is open, marked `▸` in the prefix column the cursor already shares.
  Granted only by the active **Work view preset**'s `pin = true` — of the shipped
  roster, `active` alone — never by roster position; under any other preset the
  attributed rows keep their sorted position, unmarked. **Pane work attribution**
  itself is preset-independent and still computed either way, so the first-render
  cursor landing and the status-line naming survive where the lift does not.
  Applied after the sort resolves rather than as a sort term, so it can raise a
  Map row above the whole task-set block — which no comparator can do, rows never
  being ordered across kinds — while leaving every ordering rule beneath it
  untouched. Re-derived on every rebuild from pane facts read once at launch.
  `pop work status` builds with empty pane facts and never pins.
  was: The same lift applied unconditionally under every preset — no preset field
    gated it.
