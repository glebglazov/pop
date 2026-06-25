---
fragment: 7bc9c046
generation: 0009
branch: master
---

+ In Progress
  A presentational refinement of the **Ready** display label, shown when a Ready Task set already has at least one `done` task — signalling that draining has begun on the set. It is NOT a derived **Task set status**: schedulability is still Ready (drainable), and all scheduling and summary logic keys on the underlying Ready status, never on this label. Rendered blue to distinguish it from a fresh Ready set (cyan); applied identically in both the `pop tasks status` table and the queue dashboard.
  avoid: Started, Working, In-progress status, Active
  under: Tasks
