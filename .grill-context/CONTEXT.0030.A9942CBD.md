---
fragment: A9942CBD
generation: 0030
branch: master
---

+ Terminal appearance
  Which of three palettes a pop surface prints in — *light*, *dark*, or *plain*
  where the terminal will not say what its background is. It is a property of
  the terminal a surface is running in, never of a repository or a project, and
  it may change while a surface is open. Plain is a full member rather than a
  failure: it is also what a document redirected somewhere that is not a
  terminal is rendered in.
  avoid: colour scheme, theme, dark mode
  under: Work dashboard

~ Document peek
  A read-only nested view over any absolute file path a detail row carries — a
  task's markdown, a **Routine**'s last report, an **Artifact view** row —
  opened with `l` or Enter, scrolled Vim-style (`j`/`k`, `ctrl-d`/`ctrl-u`,
  `gg`/`G`), and dismissed with `h`/left/`esc` without changing anything. A
  `.md` path is rendered as formatted markdown in the **Terminal appearance**
  in force, re-rendering when either that or the width changes; every other
  path is shown raw, because the peek's own non-markdown documents say so by
  extension and one of them, `progress.txt`, separates its records with `---`
  lines that a markdown renderer would turn into horizontal rules. The view
  reads whatever path the row hands it, which is why every row that names a
  file carries an absolute one.
  avoid: task editor, task modal, preview pane
  was: A read-only nested view over any absolute file path a detail row carries — a task's markdown, a **Routine**'s last report, an **Artifact view** row — opened with `l` or Enter, scrolled Vim-style (`j`/`k`, `ctrl-d`/`ctrl-u`, `gg`/`G`), and dismissed with `h`/left/`esc` without changing anything. A `.md` path is rendered as formatted markdown; every other path is shown raw, because the peek's own non-markdown documents say so by extension and one of them, `progress.txt`, separates its records with `---` lines that a markdown renderer would turn into horizontal rules. The view reads whatever path the row hands it, which is why every row that names a file carries an absolute one.
