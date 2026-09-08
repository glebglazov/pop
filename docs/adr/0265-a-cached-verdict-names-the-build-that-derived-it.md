---
status: accepted
relates: "amends [ADR-0243](0243-the-manifest-memo-persists-and-the-verdict-loop-forks-in-parallel.md) decision 1's content key and the property its Consequences asserted; the failure it repairs is the one [ADR-0242](0242-the-dashboard-reads-truth-through-one-guarded-reload.md) decision 7 deleted a cache for"
---

# A cached verdict names the build that derived it

## Context

ADR-0243 persisted the **Manifest memo** to the **Cache database** and asserted
the property that separates it from the glob cache ADR-0242 deleted: the content
key is recomputed and compared on every serve, so "a stale entry is not unlikely,
it is unrepresentable."

That was true of the inputs the key names, and the key names the wrong set of
inputs. `manifestContentKey` fingerprints the manifest's bytes and every dirent's
`{name, kind, size, mtime}` — the whole of `LoadManifest`'s *file* inputs, and
nothing else. But the cached value is not a function of the files alone. It is a
function of the files **and the validation rules of the build that ran them**.

The gap is not theoretical; it has already fired. `ae8a4e5` added
`exploration.md` to `unlistedSetMarkdown`, exempting it from the orphan-markdown
check. A set whose `exploration.md` was already on disk had been validated by an
earlier build and stored as MALFORMED. No file in that folder moved a byte or an
mtime across the rule change, so the key still matched, and every later run — the
binary carrying the fix included — was served the pre-fix verdict. The only
repair available was `rm cache.db`, which requires a human to first suspect the
cache, on the strength of a diagnostic that no build in the tree can still print.

Two things about that incident decide this ADR. The stale row survived a *fix*,
so waiting does not help. And `ae8a4e5` touched eight files across five packages
without coming near `tasks/manifest_memo.go`, because editing a map literal in
`tasks/manifest.go` does not look like a cache change — it looks like what it is,
a rule change.

## Decision

**1. A derivation stamp joins the content key.** `manifestContentKey` hashes, ahead
of every file input, a **Derivation stamp**: the running executable's path, size
and mtime, plus the ldflags-injected build version. The key's question widens from
"is this folder as it stands already validated?" to "is this folder as it stands
already validated *by this build*?" — which is the question the cached value was
always the answer to.

**2. The stamp is the binary's own file fingerprint, not a hand-maintained
constant.** A `derivationVersion` bumped whenever a validation rule changes would
keep unrelated rebuilds warm, and pays for it with a discipline whose failure is
silent and indistinguishable from the bug being fixed: the one instance of that
bug is also an instance of an author, who knew the cache existed, not recognising
their commit as a cache change. Nothing about the fingerprint has to be
remembered while editing a rule.

Path, size and mtime rather than `vcs.revision` or the version string alone,
because those collapse every dirty rebuild of one commit into a single value —
exactly the developer's inner loop, where rules change fastest. The build version
rides along so the stamp is legible when read out of the database.

**3. It is resolved once per process.** A daemon that stat-ed its executable per
access would adopt a newly installed build's stamp and start serving rows written
under rules it does not run. Resolved at first use and held, an old daemon and a
fresh CLI simply occupy different stamps: each serves only what its own build
derived. A stamp that cannot be resolved at all becomes a per-process random
value, so every lookup misses — cache failure is a miss, per ADR-0243 decision 4.

**4. Nothing about the schema or the row moves.** The stamp rides *inside* the
content key, so the table stays keyed by set directory with the content key as a
column, an upgrade overwrites each row in place, and ADR-0243 decision 2's
inventory bound holds with no pruning policy. The `warmed` bookkeeping makes an
upgrade window self-limiting: each process offers a key once, so an old daemon
and a new CLI overwrite one another at most once per set, not on every poll.

**5. `pop cache clear` makes the manual repair a supported verb.** Deleting the
file was always valid and remains so; the verb resolves the path the way the code
does, so nobody has to know what XDG did with it.

**6. This is the Cache database's contract, not the manifest table's.** Every
entry in `cache.db` is derived by some build. ADR-0243's rule — an entry that
cannot be cheaply re-validated against its source does not belong here — is
extended: its source includes the build, and any second tenant names the stamp in
its key too.

## Consequences

**Every rebuild invalidates every set on the machine, deliberately.** The first
`pop work dashboard` after each `make install` pays the cold walk ADR-0243
measured at ~170–280ms, including installs that touched only the TUI. This is
accepted rather than mitigated: it is one paint, below ADR-0189's threshold for
caring, and it is the entire price of the property being a fact about the design
rather than something a person keeps true.

**ADR-0243's separation from the deleted glob cache is repaired on its own
terms.** That ADR was not wrong about what makes a persisted cache honest — it
was wrong about how many inputs its key had. A stale serve is unrepresentable
again, and now for the reason ADR-0243 gave.

**A rejected option worth not re-proposing: aging the rows out.** A `written_at`
column and a TTL is the only listed option needing no discipline at all, and the
only one that concedes a staleness window — it answers "stale for at most N",
where the whole claim of this cache is "never". Once the key names the deriving
build there is nothing left for a TTL to catch.
