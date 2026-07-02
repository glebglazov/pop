---
status: superseded
superseded_by: ADR-0083
---

> **Superseded by [ADR-0083](0083-repo-config-is-one-shared-schema-for-pop-toml-and-repo-blocks.md).**

# Config precedence is scope-first; config.toml breaks equal-scope ties

Tombstoned. The scope-first law (most-specific scope wins; hand-authored breaks
equal-scope ties, so a finer-scoped runtime value could beat a coarser
hand-authored one) is replaced by an **ownership/modality-first** law: user-written
config always beats runtime-generated config at any scope, and the user's central
`config.toml` outranks a repo's in-tree `.pop.toml`. The ADR-0065 integrations
behaviour it generalized is preserved under the new law (hand-authored beats
runtime). See ADR-0083 for the law and the full precedence ladder.
