---
fragment: 0B1FC35F
generation: 0004
branch: master
---

- Work store doc

+ Issue tracker doc
  The per-operation document that adapts a planning skill's publish step to one **Work store** — sections such as publishing a spec, publishing tickets, and wayfinding operations, including any store-specific drafting vocabulary (e.g. effort and HITL/AFK for the pop store). Resolution is two-layer and vendor-neutral: the repo-level `docs/agents/issue-tracker.md` wins when present; otherwise skills read the user-level `~/.agents/docs/issue-tracker.md`. Neither present is an error a skill reports, never a silent fallback. Pop's own adapter doc is a **Shipped asset** at `~/.local/share/pop/agents/docs/issue-tracker.md`; the user-level path is a symlink to it, created by Integration refresh only when nothing already occupies that path — so a hand-authored file or a link to another store's doc always wins.
  avoid: work store doc, tracker doc
  under: Language

~ Work store
  The destination where planning skills publish their artifacts — task sets, specs, wayfinder maps and tickets, and future artifact kinds such as prototype data — together with that destination's vocabulary for expressing blocking edges and grabbing work. A repository resolves to exactly one Work store; pop's own **Task storage** backs the built-in default, and real trackers (GitHub, GitLab, local markdown, freeform) are alternative Work stores a repository may configure. Distinct from **Agent adapter** (the bridge to an agent CLI) and narrower than it sounds from "tracker": a Work store need not track anything, only hold published work. "Issue tracker" stays avoided as the abstraction's name, but is the sanctioned name of the document that adapts it (**Issue tracker doc**) and of that document's filename at both scopes.
  was: (identical, minus the final sentence) … narrower than it sounds from "tracker": a Work store need not track anything, only hold published work. _Avoid_: tracker, issue tracker (as the abstraction's name), task store, task storage adapter
