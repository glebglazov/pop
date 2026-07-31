---
status: superseded by ADR-0147
---

# Worktree directive is Queue-only; foreground implement binds the current checkout

> **Superseded by [ADR-0147](0147-managed-worktrees-are-provisioned-eagerly-at-the-operator-s-request.md):** managed worktrees are now provisioned eagerly, at `register --managed` / `bind-worktree --managed`, rather than lazily at the first unbound Queue drain — the lazy window left sets registered-but-unplaced, and every binding-to-runtime-path consumer silently substituted the trunk for them, deadlocking dispatch. ADR-0147 carries forward this ADR's surviving law unchanged: foreground implement runs in the current checkout (with `--in-worktree` forking from current HEAD), the Queue forks from trunk and never invents a checkout.
