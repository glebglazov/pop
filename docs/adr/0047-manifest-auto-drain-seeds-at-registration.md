---
status: accepted
---

# Task manifest auto_drain seeds Task state at first registration

## Status

accepted

## Context

**Auto-drain** is a per-set consent bit in **Task state** (ADR-0041): it defaults off and the **Queue dashboard** toggle is the runtime control surface. Planning workflows authoring a Task set had no way to express "enqueue this for unattended draining when it first appears" without a separate dashboard step after registration.

## Decision

Add an optional top-level `"auto_drain": true` boolean to the **Task manifest** (`index.json`). Pop reads it **once at first registration** — lazy discovery, import, or any path that creates the registration entry — and seeds the **Auto-drain** bit in Task state accordingly. Absent or `false` seeds off. The key is never re-applied on refresh; the dashboard toggle remains authoritative after registration. A non-boolean value is a contract fault that makes the Task set **Malformed**. When pop seeds true, the registration line gains an `(auto-drain)` suffix. Planning skills such as **to-tasks** write the key only when the human explicitly requests it in that session.

## Consequences

- Authoring intent and runtime consent stay separate: the manifest records intent at plan time; Task state records live consent the human can revoke from the dashboard.
- Editing a manifest after registration does not change an already-registered set's Auto-drain bit — by design, not oversight.
- Import must seed through the same path as discovery (`registerImportedTaskSet` today omits AutoDrain entirely and needs updating).

## Considered options

- **Re-sync from manifest on every refresh.** Rejected: would fight manual dashboard toggles and blur the Task state vs manifest boundary already drawn for priority and archive.
- **Silently ignore invalid `auto_drain` values.** Rejected: inconsistent with other manifest contract faults such as unknown `effort` tokens; failures should surface at discovery.
