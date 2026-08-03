---
status: accepted
relates: "narrowed by [ADR-0181](0181-registration-default-routes-on-checkout-locality.md) — this default is now the trunk case; from a non-trunk worktree the set binds in place"
---

# `to-tasks` defaults to `managed auto-drain`; `no-drain` opts out

## Context

[ADR-0136](0136-planning-skills-publish-through-a-work-store-seam.md) gave
`to-tasks` two keyword arguments over `pop tasks register`, stated as "both
default off, are independent, and may be combined": `managed`/`isolated` →
`--managed`, `auto-drain`/`drain` → `--auto-drain`. With neither keyword, a
set registers plain.

In practice nobody drains a registered set by typing the keywords every time
— the default is what most invocations get, and it is the one combination
that currently does nothing. Flipping the default to include auto-drain
raises a real hazard, though: `--auto-drain` alone lets the Queue daemon
start draining the set **unattended**, in whatever checkout `register` binds
to. Plain `register` binds to the **current** checkout — the operator's live
working tree. Making auto-drain the default without also defaulting to
`--managed` would mean the moment a set registers, an unattended agent starts
mutating the operator's own tree with no second gate.

## Decision

- **The default becomes `managed auto-drain`** (`--managed --auto-drain`),
  not `auto-drain` alone. Pairing the two means the new unattended default is
  also the isolated one: draining always happens in a worktree forked from
  Trunk, never in the checkout the operator is sitting in.
- **The opt-out keyword is `no-drain`, with `manual` as an accepted
  synonym**, mirroring the existing keyword-pair style (a literal word plus a
  reads-naturally synonym). Alone, `no-drain`/`manual` registers plain (no
  flags) — the full old default. Combined with `managed`/`isolated`, it
  registers `--managed` only: an isolated worktree provisioned immediately,
  without the Queue daemon touching it unattended. `managed`/`isolated` and
  `auto-drain`/`drain` stay valid keywords and keep mapping straight to their
  flags — they remain meaningful as **explicit affirmations** even though
  typing them alone no longer changes anything (the default already grants
  both). There is no keyword combination left for "auto-drain without
  managed" — that is exactly the hazardous combination this ADR retires as a
  reachable default *or* explicit choice.
- **Trunk-less fallback stays a fallback, not a hard refusal, on the default
  path.** `--managed` already refuses registration when no trunk resolves
  (documented today, unchanged). If the *default* invocation (no keywords at
  all) hits that refusal, the skill retries as plain registration and warns
  the user, rather than retrying with `--auto-drain` alone — which would
  reproduce the unattended-drain-in-place hazard this ADR exists to avoid.
  An **explicit** `managed` keyword keeps today's behavior: refuse and point
  at `--trunk <path>`, since the operator asked for it by name and silently
  downgrading their explicit request would be a surprise, not a courtesy.
- **Non-pop stores are unaffected.** All four keywords remain pop-store-only;
  against a resolved non-pop store the skill still warns, ignores every one
  of them (including `no-drain`/`manual`), and publishes to the configured
  tracker.
- **No Go change.** `pop tasks register --managed --auto-drain` keep their
  existing, independent semantics, including the trunk refusal. Only which
  flags `to-tasks` supplies by default moves; this amends ADR-0136's default,
  not the flags themselves.

## Considered Options

- **Flip only the auto-drain default, leave `managed` opt-in.** Rejected:
  this is the exact hazard above — the default becomes unattended draining of
  the operator's live checkout the moment a set registers.
- **Make `no-drain`/`manual` also always drop `managed`** (no way to reach
  "managed, not auto-drained" without the old opt-in spelling). Rejected: it
  is a legitimate, cheap-to-keep state — provision the isolated worktree now,
  decide later whether to drain it — and `managed`/`isolated` staying
  meaningful as an affirmation is what makes the combination reachable.
- **On trunk-less fallback, drop `managed` but keep `--auto-drain`.**
  Rejected: this reproduces "drains the current checkout unattended" through
  the back door of a failed default, which is the specific failure mode the
  paired default is meant to close off.
- **A single unified opt-out spelling shared with `managed`/`isolated`
  (e.g. reusing `plain`).** Rejected: `plain` was never a keyword before —
  introducing it means teaching a fifth token where the existing pair-style
  (`no-drain`/`manual`) already reads naturally as auto-drain's opposite.

## Consequences

- `to-tasks`'s Arguments section states the new default, the retired
  auto-drain-alone combination, and the `no-drain`/`manual` opt-out (plain
  or `--managed`-only depending on whether `managed`/`isolated` is also
  present).
- The issue tracker doc's "Register the set" section restates the same
  mapping — plain registration is now reached via `no-drain`/`manual` or the
  trunk-less default fallback, never via a bare invocation with no keywords.
- No prior text stating "both default off" survives anywhere describing
  `to-tasks`'s arguments; ADR-0136's summary line is superseded by this
  decision for that one detail.
