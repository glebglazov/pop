---
name: research
description: Investigate a question against high-trust primary sources and capture the findings as a Markdown file in the repo. Use when the user wants a topic researched, docs or API facts gathered, or reading legwork delegated to a background agent.
---

<!--
base: mattpocock/skills engineering/research@9603c1cc8118d08bc1b3bf34cf714f62178dea3b

This file is a verbatim copy of upstream engineering/research/SKILL.md at the
pinned ref above. Pop inlines rather than delegating to Matt's skills, per
ADR-0009 (skills are embedded in the binary and ship to machines without them
installed). There is no POP OVERLAY region. To review upstream drift, diff
against engineering/research@<newref>.
-->

Spin up a **background agent** to do the research, so you keep working while it reads.

Its job:

1. Investigate the question against **primary sources** — official docs, source code, specs, first-party APIs — not a secondary write-up of them. Follow every claim back to the source that owns it.
2. Write the findings to a single Markdown file, citing each claim's source.
3. Save it where the repo already keeps such notes; match the existing convention, and if there is none, put it somewhere sensible and say where.
