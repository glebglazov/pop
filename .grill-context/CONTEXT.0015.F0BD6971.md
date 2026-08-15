---
fragment: F0BD6971
generation: 0015
branch: master
---

+ Repo convention
  The prose answer to "how does this repository do X" for one Convention kind — commit grammar, issue-tracker behaviour — resolved for the repository the current checkout belongs to. It is never a single document: what a caller gets is the Convention stack, and "the convention" means that stack reconciled.
  avoid: project convention, house style, commit style
  under: Conventions

+ Convention kind
  One member of the closed set of things pop can hold a Repo convention for — `commits` and `issue-tracker` at first, with `code-review` and `verification` as the shape it was built for. Closed because each kind's Convention recipe is kind-specific work pop must know how to do; an unknown kind is refused with the list of the ones that exist, as `pop config repo set` refuses an unknown key.
  avoid: convention type, convention name
  under: Conventions

+ Convention stack
  The four layers a Convention kind resolves through, lowest rank first: `~/.agents/docs/<kind>.md` (the human's defaults, every repository), the Convention memory (this repository, this machine), `docs/agents/<kind>.md` (the repository's committed document), and the Convention overlay. They **compose** — `pop repo conventions get` emits every layer that exists, labelled with its origin, in rank order, and the reading agent reconciles them; pop orders and labels, it never merges prose. Rank decides only direct contradictions.
  avoid: convention precedence, convention merge order, winner tier

+ Convention memory
  The pop-written layer of the Convention stack: one Markdown file per kind under the repository's **Task storage** directory, keyed by **Repository identity** so every worktree of a repository reads one value. Written by `pop repo conventions set` from stdin, removed by `unset`, and carrying frontmatter recording what the convention was derived from and when — the provenance the disclosure line quotes. It is where a derived convention lands; a convention a human states in session should be offered to the repository's document instead, because the team should own that in version control.
  avoid: remembered convention, convention cache, repo settings

+ Convention overlay
  `~/.agents/docs/<kind>.overlay.md` — the human's top-ranked layer, above even the repository's committed document, mirroring how `config.override.toml` outranks a hand-authored `config.toml`. It holds constraints that must survive any repository ("never add a Co-Authored-By trailer"), where the same human's bottom layer holds preferences a team is entitled to overrule. Named overlay, not override, because pop already spends "override" on the **Repo override** and its runtime layer, and because pop's overlay rules already mean "layered on top, beats what is underneath". User-global only: a repository's own top slot is its document.
  avoid: convention override, merge doc, common rules

+ Convention recipe
  Pop's built-in, per-kind instruction for producing a Repo convention when the Convention stack is empty — embedded in the conventions package, printed by `pop repo conventions recipe <kind>` and by `get` when it exits 1. Distinct in kind from a convention: the stack returns an answer, the recipe returns the method for getting one, and conflating them would force every caller to branch on which it received. The `commits` recipe carries the derivation that was skill prose until now, including the discard-pop-generated-commits guard, plus where to write the result.
  avoid: convention default, fallback convention, derivation instructions

~ Commit convention
  The `commits` Convention kind: the commit-message grammar a repository's team writes history in — types, scopes, subject style, and body style, which are one document and one sample rather than two kinds. Resolved through the Convention stack, whose repository layer is `docs/agents/commits.md`. Planning still resolves it once per Task set and records the resolved text on the set, so a draining set's grammar cannot change underfoot; what changed is that the resolution rule is the stack rather than a doc-then-log ladder in skill prose.
  was: The commit-message grammar a repository's team writes history in — types, scopes, subject style. Planning resolves it once per Task set: the Commit format doc wins when present; otherwise it is inferred from recent history, skipping pop-generated commits so pop never learns its own accent. The resolved convention is recorded on the Task set; when nothing resolves, pop's task-derived format remains the fallback.
  avoid: commit style, house style

- Commit format doc
