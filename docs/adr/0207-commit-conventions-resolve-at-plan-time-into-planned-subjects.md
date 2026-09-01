---
status: superseded by ADR-0211
---

# Commit conventions resolve at plan time into planned subjects

> **Superseded by [ADR-0211](0211-a-repo-convention-resolves-through-a-composed-four-layer-stack.md):** the plan-time shape survives — the convention resolves once per Task set, renders into Planned commit subjects, and the executor commits them verbatim — but the *resolution rule* recorded here is entirely replaced. A convention no longer comes from `docs/commit-format.md` falling back to a log sample described in skill prose; it resolves through a composed four-layer Convention stack that `pop repo conventions get commits` emits, and the repository's document moves to `docs/agents/commits.md` with no legacy alias.
