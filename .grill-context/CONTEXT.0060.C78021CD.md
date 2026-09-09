---
fragment: C78021CD
generation: 0060
branch: master
---

+ Half-removed checkout
  A directory left where a checkout's removal stopped part way: its `.git` file
  is gone, so git refuses every later `git worktree remove`, while the directory
  and some of its contents remain.
  avoid: orphan worktree, stale worktree, zombie worktree, broken worktree
  under: Language

+ Checkout removal
  Pop's own act of deleting a checkout: a recursive delete that does not stop at
  the first entry it cannot remove, then `git worktree prune`, then the checkout's
  **History** entry. It finishes a **Half-removed checkout** instead of refusing
  one, and it names what still holds the directory rather than refusing on its
  account.
  avoid: worktree delete, git worktree remove
  under: Language
