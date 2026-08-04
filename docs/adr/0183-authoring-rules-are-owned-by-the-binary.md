---
status: accepted
relates: "narrows [ADR-0169](0169-the-issue-tracker-doc-resolves-at-a-vendor-neutral-user-path.md) — the seeded doc keeps behavioural rules and loses authoring shape; [ADR-0184](0184-map-assist-and-the-authoring-contract.md) splits Map assist's rules across this seam"
---

# Authoring rules are owned by the binary, not the installed doc

## Context

pop's Work store has two kinds a planning skill hand-authors on disk: Task sets
(`index.json` plus numbered task markdown) and Maps (`map.md`, `issues/NN-*.md`,
`index.json`). Neither has an authoring API — the skill writes the files itself
and `pop tasks register` / `pop map register` validate them.

The rules for writing those files have lived in one place:
`integrate/issue-tracker.md`, embedded in the binary and seeded to the user-level
issue-tracker doc, which a skill resolves two-layer (a repo's
`docs/agents/issue-tracker.md` wins when present). That document is ~520 lines,
of which roughly half is storage layout, file templates, manifest field lists and
enums — text that exists only so a session can copy it — and the other half is
behavioural prose: claiming, resolution, handoff, registration flags, the
artifacts-must-be-committed rule.

The templates and enums are the same rules `validateManifest` and the Map
manifest validator enforce, written a second time in prose. Two copies of one
rule drift, and the drift is silent: a stale seeded doc against a newer binary
produces a MALFORMED set with no indication which of the two was wrong. Charting
and breakdown sessions have also read Go source to rediscover shapes the doc was
supposed to carry.

An authoring API — JSON verbs that write the files — was considered and rejected
separately: the validators already catch every structural failure such verbs
would prevent, and report the whole fix list rather than the first item. The
reported harm was rediscovery cost, not malformed output. So the question is not
who *writes* the files, but who *owns the rules* for writing them.

## Decision

**The binary owns authoring rules; the installed doc owns behavioural rules.**

Each Work kind has one read-only guide verb — `pop map authoring-guide`,
`pop tasks authoring-guide` — that prints how to hand-author that kind: storage
layout, every file template, and the manifest's fields with their allowed values.
Enums, filename patterns and marker strings are **generated from the same
constants the validator reads**, so the printed rules cannot drift from the
enforced ones. Per kind, not a `pop work` umbrella: discovery follows the command
family a session is already using, and the two guides share no body to factor
out.

The Task-set guide carries **judgment prose too**, not only mechanics — HITL/AFK
typing including the split-the-slice rule and the two legitimate HITL positions,
the effort heuristic, the vertical-slice framing and the Orientation rule.
Unenforceability is an argument about validation, not about where text lives, and
splitting mechanics-in-binary from judgment-in-doc would make a skill read two
surfaces to author one artifact — reintroducing the drift the change exists to
kill, between the two halves instead of against the validator.

The guide is **authoritative**, not a summary. The doc deletes what the guide
covers and keeps what no generated text can carry: store resolution, the
`register` flag and keyword semantics, artifacts-must-be-committed, claiming,
resolution and its ticket-type overrides, handoff, and the Map-sourced-set
minting obligation.

The guide describes **the artifact, not a workflow**, so it serves every writer
of a set — initial authoring, `to-spec`'s handoff, and the assist agent that
edits `index.json` mid-drain — rather than just the first one.

`-h` stays a flag reference: flags, defaults, the keyword mapping and the
MALFORMED fix loop, plus a pointer at the guide. A human hits `-h` constantly; a
machine reads the guide once per authoring session.

**Per-repo override narrows accordingly.** A repo's `docs/agents/issue-tracker.md`
can still choose a different store or change behavioural conventions. It can no
longer redefine pop-store authoring shape. The surviving doc says so explicitly,
because a repo doc that tried would be a no-op with a confusing failure mode.

## Considered Options

**Keep everything in the doc, just maintain it better.** Rejected: the drift
vector is not carelessness. The doc is *installed* to the data directory, so a
machine can run a new binary against an older seeded copy. Text compiled into the
binary is versioned with the validator by construction; text on disk is versioned
with whenever integrate last ran.

**Put the rules in `--help` on `register`.** Rejected: `-h` is hit constantly by
humans wanting flag syntax, and would re-pay ~200 lines of authoring doctrine
every time. The guide is read once per authoring session by a machine; the two
audiences want different documents.

**Mechanics in the binary, judgment left in the doc.** Rejected as above: two
surfaces per artifact, and a new drift seam between them.

**A guide that summarises, with the doc still authoritative.** Rejected: that is
by definition two copies, which is the thing being removed. Only authoritative
generated text kills the drift.

**A `pop work authoring-guide <kind>` umbrella.** Rejected: a menu in front of
two unrelated documents, discovered further from the family a session is already
in.

**JSON authoring verbs.** Rejected on the Map side and inherited here: the
validators already report every structural failure such verbs would prevent, and
the create-then-wire two-pass convention they lean on does not apply to pop —
that describes a tracker where a *server* mints ids, whereas in files the agent
picks the ids and writes everything in one pass.

## Consequences

- Two read-only verbs, each generated from its kind's constants. A constant
  changing updates the guide with no doc edit.
- `integrate/issue-tracker.md` loses its layout and template sections for both
  kinds — roughly half its length — and becomes store resolution plus behavioural
  prose plus two pointers.
- Skills that author Work run the guide verb before writing files. That is one
  extra command per authoring session, against the source-reading it replaces.
- A repo can no longer fork pop-store authoring shape. In practice it never could
  — the validator overrode any doc that tried — so this makes an existing
  authority visible rather than removing a working capability.
- The generated text is tested against the constants it claims to print: each
  printed enum is asserted equal to the validator's set, and each printed
  template is put back through the parser and the manifest validator. Without
  that the guarantee would be a convention rather than a guarantee.
- Behavioural prose stays exposed to the stale-seed problem. Accepted: it is not
  checkable by any validator, so a drifted copy fails loudly in conversation
  rather than silently at registration.
