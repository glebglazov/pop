---
status: superseded
superseded_by: ADR-0083
---

> **Superseded by [ADR-0083](0083-repo-config-is-one-shared-schema-for-pop-toml-and-repo-blocks.md).**

# In-tree repo config resolves git-agnostically from two anchors

Tombstoned. The surviving kernel — resolving repo-scope in-tree config at two
anchors (this worktree + the Trunk worktree, identity-root fallback for bare
repos, presence decides) — is retained and now recorded in ADR-0083. The
git-agnostic base+override rationale this ADR was written for (a gitignored
`.pop.local.toml` that git would not copy into new worktrees) was dropped when
the design narrowed, so the original body is removed to avoid misleading a reader
into building the abandoned engine.
