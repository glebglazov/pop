---
fragment: CDC4735E
generation: 0048
branch: master
---

~ Top-level menu
  A **Work dashboard** menu opened directly rather than from inside another menu. Every menu on the surface is one: four openers — `r` **Run menu**, `s` **Status menu**, `y` **Copy menu**, `m` **Mute menu** — and no menu contains another. `s`, `y` and `m` answer on the row list and in a detail view alike, and in a detail view they act on the **Work container** the view is open over, so the keyboard an operator learns above is the keyboard that answers below; `r` is the one key that changes subject with the level, opening the **Detail item menu** instead. A menu whose target offers it nothing is not a dead key: it flashes what the kind cannot do, because a top-level key that appears broken is worse than a verb that was simply absent from a list.
  avoid: submenu, nested menu, second-level menu
  was: A **Work dashboard** menu opened directly from the row list rather than from inside another menu. Every menu on the surface is one: the dashboard has four openers — `r` **Run menu**, `s` **Status menu**, `y` **Copy menu**, `m` **Mute menu** — and no menu contains another. A menu whose target offers it nothing is not a dead key: it flashes what the kind cannot do, because a top-level key that appears broken is worse than a verb that was simply absent from a list.

+ Detail item menu
  The one menu a detail view opens over a **Work item** rather than over its **Work container**, on `r`, holding that item's verbs whole: a task's **Complete task**, **Open task** and **Skip** beside its copy-name and copy-path, a Map ticket's grilling handoffs. It is undifferentiated by design — a task's item verbs are nothing but status writes and copies, so splitting them the way a container's list is split would leave `r` opening an empty menu on the very kind the view was built for. It is the single place a detail view's keyboard changes subject: every other opener there acts on the container.
  avoid: item action menu, task menu, item run menu
  under: The Work dashboard

~ Copy-name verb
  The copy verb every **Work kind** offers on every **Work dashboard** level, copying an identifier via **Clipboard copy** and always reporting a transient status confirmation. Payload follows the level: a bare **Task set identifier** on a task-set row, the map id on a Map row, a **Task target reference** (`<task-set>/<file>.md`) on a task, a bare ticket id on a Map ticket. It is reached through a menu everywhere and bound as a direct keypress nowhere — `n` in a container's **Copy menu**, `y` in a **Detail item menu** — so the flat `y` that copied outright in the **Task set detail view** and **Document peek** is gone, and `y` means the same opener at both levels.
  avoid: yank verb, copy id, clipboard verb
  was: The `y` verb on every **Work dashboard** level, copying the cursored row's identifier via **Clipboard copy** and always reporting a transient status confirmation. Payload follows the level: a bare **Task set identifier** on a task-set table row, the map id on a Wayfinder Map row, a **Task target reference** (`<task-set>/<file>.md`) in the **Task set detail view** and **Document peek**, and a bare ticket id on a Map ticket. Bound both as a direct keypress and as an action-menu entry, so it is discoverable without being slow to reach.

~ Task set detail view
  The full-screen interactive drill-down entered with `l` or Enter from the **Work dashboard**, replacing the table until dismissed with `h`/left/`esc`. It shows the focused **Task set**'s **Detail sections** above one of two lists — its tasks, or its **Artifact view** — and `v` switches between them, the same letter that switches **Work page**s one level up and free here because the shell withholds its own toggle while a detail view is open. Both lists support Vim-style movement including top and bottom (`gg`/`G`), narrow to a **Work dashboard search** on `/`, and open a **Document peek** on the cursored row with `l` or Enter. Its keyboard is the row list's: `s`, `y`, `m` and the flat `I` act on the set itself, exactly as they do on the row it was opened from, so the set is reachable from inside its own detail instead of only from the table. `r` alone changes subject, opening the **Detail item menu** over the cursored task or artifact — from the list and from inside the peek alike.

~ Map detail view
  The drill-down entered with Enter/`l` from a Work dashboard Map row — mirror of the **Task set detail view**, keyboard included: the Map's Decision tickets with the frontier highlighted, `s`/`y`/`m` acting on the Map itself, and `r` opening the ticket's **Detail item menu**, from which `I`/Enter spawns an attended wayfinder session for that specific ticket.
  was: The drill-down entered with Enter/`l` from a Work dashboard Map row — mirror of the Task set detail view: the Map's Decision tickets with the frontier highlighted; `i`/Enter on a frontier ticket spawns an attended wayfinder session for that specific ticket.
