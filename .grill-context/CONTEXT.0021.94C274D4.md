---
fragment: 94C274D4
generation: 0021
branch: master
---

+ Artifact view
  The second list a **Task set detail view** shows in place of its task list, toggled with `v` — the readable subset of the set's **Task artifact**s, one row per document, newest first. Rows carry an artifact type (`review`, `spec`, `progress`), the bare filename, and the instant the document was written: a review takes it from the instant in its own name, everything else from its modification time, so one total order covers a family whose members do not all timestamp themselves. Every prior **Review artifact** is a row of its own, because retaining review history is only worth anything if it is reachable. It is a closed known list, not a directory dump — the manifest is the detail view itself rendered, task markdown is the other list, and captured runs are gzipped JSONL that `pop tasks stream` already serves. It is offered only when the container has at least one artifact, which is what makes the seam a silent no-op for a **Work kind** that publishes none.
  avoid: artifact page, artifact tab, artifact facet, detail view lens, artifact list
  under: Work dashboard

+ Artifact
  One document a **Work kind** publishes about a **Work container** that a human reads rather than acts on: its type, its name, its absolute path, and when it was written. It is deliberately not a **Work item** — an item's status is the token its kind keys verbs off, and an artifact has no status to key on, so an artifact carried as an item would mean a blank status column and a special case in every `ItemActions`. Kinds publish artifacts through a seam of their own and offer their own verbs on them; a kind with nothing to publish returns none.
  avoid: work artifact (that names the file family, not the row), detail item, document row

+ Document peek
  A read-only nested view over any absolute file path a detail row carries — a task's markdown, a **Routine**'s last report, an **Artifact view** row — opened with `l` or Enter, scrolled Vim-style (`j`/`k`, `ctrl-d`/`ctrl-u`, `gg`/`G`), and dismissed with `h`/left/`esc` without changing anything. The view reads whatever path the row hands it, which is why every row that names a file carries an absolute one.
  avoid: task modal, preview pane, task editor, file peek
  under: Work dashboard

~ Task set detail view
  The full-screen interactive drill-down entered with `l` or Enter from the **Work dashboard**, replacing the table until dismissed with `h`/left/`esc`. It shows the focused **Task set**'s **Detail sections** above one of two lists — its tasks, or its **Artifact view** — and `v` switches between them, the same letter that switches **Work page**s one level up and free here because the shell withholds its own toggle while a detail view is open. Both lists support Vim-style movement including top and bottom (`gg`/`G`) and open a **Document peek** on the cursored row with `l` or Enter. Row verbs live in the row's own menu behind `a`, asked of the owning **Work kind**: a task offers **Complete task**, **Open task** and **Skip** in place; a task or an artifact offers copy-name and copy-path, from the menu and from inside the peek alike.
~ Task artifact
  A machine-local planning document, task markdown file, task manifest, progress record, **Review artifact**, or captured attempt stream within **Task storage**. Task artifacts live outside the repository tree, so they can never enter implementation commits and require no ignore configuration. The readable ones — reviews, the spec, the progress record — are the rows of a set's **Artifact view**.
  was: "A machine-local planning document, task markdown file, task manifest, progress record, or captured attempt stream within **Task storage**. Task artifacts live outside the repository tree, so they can never enter implementation commits and require no ignore configuration."
