---
fragment: 0E4ED182
generation: 0030
branch: master
---

+ Read-whole notice
  The one line pop prints above each convention it renders to a shell: the
  document's line count and the obligation to read all of it. It exists because
  an agent that reads a long command output habitually truncates it — `head`,
  `tail`, `sed -n`, a grep — and a prefix read of a resolved convention drops
  rules the agent is nonetheless bound by. A *header* rather than a footer, and
  unconditional rather than length-gated, for one reason each: a header is the
  only part of the output a prefix read cannot drop, and a guard that appears
  only above some threshold teaches the reader it is optional. Printed by the
  command path alone — never by the **Config dashboard** preview, which is
  scrolled, nor by the prose pop injects into a prompt, which arrives whole and
  would otherwise carry an instruction to re-run a command the reading agent
  never ran.
  avoid: length banner, truncation guard, header, preamble
  under: Conventions

~ Convention consumption shape
  Whether a **Convention kind** reaches an agent as a prompt *body* or as a
  labelled *block*, declared by the kind and honoured at every call site
  (ADR-0227). A **role-driving** kind — `verification`, `code-review` — is an
  agent's entire mandate, so the convention is the body and pop supplies only a
  **Role preamble** and a **Response contract** around it; there is then exactly
  one voice on what to check, where a convention that merely supplemented pop's
  own prompt would leave the team's answer arguing with pop's and no rule for
  which wins. A **step-informing** kind — `commits`, `issue-tracker` — is a fact
  a prompt about something else needs, having no output contract to protect.
  The shape says nothing about *delivery*, and the two are not the same axis: a
  convention reaches an agent by three paths, and pop injects it on only two of
  them — the body of a role-driving prompt, and the `commits` block pop projects
  into a **Task set**'s manifest. On the third the agent runs
  `pop conventions get` itself and reads the output, which is how every planning
  skill reaches the `issue-tracker` document and how any agent may reach any
  kind. That third path is the only one a reader can truncate, which is what the
  **Read-whole notice** guards.
  was: Whether a Convention kind reaches an agent as a prompt *body* or as a
  labelled *block*, declared by the kind and honoured at every call site
  (ADR-0227). A role-driving kind — `verification`, `code-review` — is an
  agent's entire mandate, so the convention is the body and pop supplies only a
  Role preamble and a Response contract around it; there is then exactly one
  voice on what to check, where a convention that merely supplemented pop's own
  prompt would leave the team's answer arguing with pop's and no rule for which
  wins. A step-informing kind — `commits`, `issue-tracker` — is a fact a prompt
  about something else needs, so it stays a block inside pop's prompt, having no
  output contract to protect.
