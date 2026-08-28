---
status: accepted
---

# The dashboard reads truth through one guarded reload

## Context

The Work dashboard occasionally showed stale rows right after a batch status
change — a read-your-own-writes violation that healed itself within one poll.
The investigation found the refresh strategy itself sound: a full snapshot
rebuild from manifests and the store every poll tick, content-keyed memos, and
an immediate rebuild after every write verb's `OutcomeRefresh`. Every stale
read traced to state that had **escaped** that loop, never to the poll being
slow:

- Two reloads can be in flight at once (tick and post-write), and
  `dashboardRowsMsg` applied whichever finished last — so a tick rebuild that
  *started* before a write could land after the post-write rebuild and
  overwrite fresh rows with pre-write rows. This is the observed bug.
- The auto-drain toggle was the one write verb that skipped the reload,
  optimistically patching `snap.Containers` — the filtered view — while
  `allRows`, the documented source of truth, kept the old value; any view
  re-derivation reverted the cell.
- `m.kinds` was built once at dashboard open around `deps.MemoGit`, whose own
  contract limits it to one load — so every verb, menu and artifact listing
  resolved git facts frozen at open time, for the whole session.
- The Document peek loaded its file text once and never re-read it.
- Config was frozen for the session outside the config modal's own post-write
  re-read ([ADR-0202](0202-config-overrides-are-a-top-ranked-layer-edited-by-one-component.md)
  decision 14), so a `pop config` write from another pane never appeared.
- The on-disk glob cache (`~/.cache/pop/glob_cache.json`) was the only cache
  shared across processes and restarts, validated by directory mtime with an
  empty-map entry counting as permanently valid, depth-limited mtime coverage,
  and unlocked last-writer-wins JSON.

## Decision

There is one refresh primitive — the **Dashboard reload** — and nothing on the
dashboard updates outside it.

1. **Reloads are sequence-stamped when they start, and a result older than the
   newest one applied is dropped.** This makes the existing
   write-then-reload path actually deliver read-your-own-writes under
   overlapping rebuilds, with no single-flighting and no skipped rebuilds.
2. **The refresh model stays poll-based, with no file watcher.** The
   dashboard's own writes reload immediately (unchanged); another process's
   writes wait at most one poll interval. A watcher would add a failure class
   (missed events, platform quirks) to shave under two seconds off a path that
   was never the problem.
3. **No write verb patches the view optimistically.** The auto-drain toggle
   goes through the reload like every other verb; a private update path is how
   this bug family starts.
4. **`m.kinds` is renewed from every reload's own freshly-built kind list**, so
   verbs, menus and artifact listings honour the git memo's one-load lifetime
   instead of acting on open-time facts.
5. **Config hot-reloads on file change**: the shell stats its config files each
   poll and, on an mtime change, re-reads through the same reconciliation path
   the config modal already uses. This amends
   [ADR-0202](0202-config-overrides-are-a-top-ranked-layer-edited-by-one-component.md)
   decision 14 from "the modal's post-write re-read is the only hot reload" to
   "the modal and file-change detection".
6. **The Document peek re-reads its file each poll while open**, and its render
   cache invalidates on content, not just width and appearance.
7. **The glob cache is deleted, not hardened.** Measured on a real project set,
   expansion took ~0.30 s with the cache and ~0.30 s without it — zero benefit
   against a persistent, cross-process correctness surface. If expansion ever
   gets slow, the fix starts from measurement, not from resurrecting this
   cache.

## Considered options

- **A file watcher (fsnotify/FSEvents) instead of the poll.** Rejected: every
  stale read found was state escaping the rebuild, not poll latency; a watcher
  fixes the wrong layer and adds missed-event failure modes the poll cannot
  have.
- **Single-flighting reloads (suppress the tick while one is in flight).**
  Rejected: it coalesces work but can still deliver a stale result that started
  pre-write; the sequence guard is smaller and covers every overlap.
- **Keeping the auto-drain optimistic patch but applying it to `allRows` too.**
  Rejected: uniformity is worth more than ~200 ms of optimism, and one verb
  with a private update path invites the next one.
- **Fixing the glob cache's validation holes.** Rejected on measurement: there
  is nothing to buy with the added correctness surface.
- **A cross-process change nudge (touch-file or store version counter) so CLI
  writes appear instantly.** Deferred, not needed: the complaint was about the
  dashboard's own writes, and one poll interval is an acceptable bound for
  another process's.
