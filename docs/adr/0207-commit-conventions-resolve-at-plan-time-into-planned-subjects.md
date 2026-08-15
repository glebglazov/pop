---
status: superseded by ADR-0211
---

# Commit conventions resolve at plan time into planned subjects

> **Superseded by [ADR-0211](0211-a-repo-convention-resolves-through-a-composed-four-layer-stack.md):** the plan-time shape survives — the convention resolves once per Task set, renders into Planned commit subjects, and the executor commits them verbatim — but the *resolution rule* recorded here is entirely replaced. A convention no longer comes from `docs/commit-format.md` falling back to a log sample described in skill prose; it resolves through a composed four-layer Convention stack that `pop repo conventions get commits` emits, and the repository's document moves to `docs/agents/commits.md` with no legacy alias.

Pop's implementation commits carry a pop-controlled subject (`tasks(<slug>): <task>`), which reads as a foreign body in a team repository with its own commit convention. Rendering a message under a convention is agentic work — picking a type, writing a summary — but pop's executor commits algorithmically, after the agent has exited, and should stay that way.

The decision: the convention is resolved **once per Task set, at plan time**, where an agent with full task context is already running. `/to-tasks` reads `docs/commit-format.md` (the Commit format doc) when it exists; otherwise it infers the grammar from the last five commits, first discarding pop-generated ones so pop never learns its own accent from a repo it already worked in. It then writes onto each task its final, literal subject line — the Planned commit subject — and records the convention text itself in the set manifest. At commit time the executor uses the planned subject **verbatim** (body stays the agent summary); commit time gains no agent call, no template language, and no rendering. Remediation tasks, born mid-drain, get their planned subject rendered by the Verifier at spawn time from the manifest's convention text — the one moment an agent that knows the finding is present. A task with no planned subject, and any set where nothing resolves, falls back to pop's existing format, which is thereby demoted from special-cased code to the built-in default convention.

A rejected alternative worth remembering: rendering at commit time from a set-level format spec. It keeps the manifest smaller but re-imports agentic work (or a template micro-language) into the executor, and hides the future subjects from the human who reviews the manifest before the set runs — pre-rendered subjects are inspectable and deletable before they ever reach history.

## Consequences

- **Verify-range detection must stop grepping subjects.** The Verifier finds a set's commit range by searching history for the `tasks(<slug>):` prefix (`tasks/verify.go`), which planned subjects no longer carry. Replacement, layered: the executor records the parent of the set's first implementation commit (the Set base commit) in the manifest — recorded at first-commit time, not set-creation time, so commits landing between planning and draining stay outside the range. Range is `base..HEAD` when the base **and every recorded task-commit SHA** are still reachable; task SHAs are the rewrite detector, because a rebase onto a newer trunk keeps the old base an ancestor while making `base..HEAD` swallow other people's commits. On rewrite, fall back to searching for the manifest's own planned subjects (rebases keep subjects by default); if those are gone too, park NEEDS-HUMAN — the Verifier never guesses a range.
- The dirty-runtime checkpoint commit keeps pop's default format for now (deliberately deferred).
- Code review is out of scope; if wanted later, it is a final task `/to-tasks` appends to the set, not a new pipeline mechanism.
