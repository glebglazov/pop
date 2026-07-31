---
status: superseded by ADR-0148
supersedes: [ADR-0030, ADR-0051]
---

# Worktree-set integration is removed; merging is the human's own concern

> **Superseded by [ADR-0148](0148-fold-lands-a-finished-set-and-releases-its-checkout.md):** merging returns as a single explicit verb, `pop tasks fold <set>`, filling the hole this ADR names — the Done-set clean-up reminder that never had a verb behind it. This ADR's actual objection is upheld, not reversed: fold keeps no backlog, computes no mergeability verdict, and adds no status suffix, so no background second source of truth returns. ADR-0148 carries forward the retirement of the Integration target in both roles, and `pop integrate <agent>` keeps its unrelated name.
