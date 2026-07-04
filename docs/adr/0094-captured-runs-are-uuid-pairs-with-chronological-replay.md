---
status: accepted
supersedes:
  - 0093
---

# Captured runs are UUID pairs with chronological replay

Structured agent telemetry — implement **Task attempt**s and **Verifier** invocations — is stored as **Captured run** pairs under `streams/runs/`: `<uuid>.meta.json` (sortable index fields) plus `<uuid>.events.jsonl.gz` (timestamped raw events). Each new structured adapter-mode invocation gets a fresh random `run_id` (the uuid filename); persistence is best-effort and never blocks implement or verify. The **Verify verdict** stays in the drain store (State); runs are Telemetry only — no path link from verdict to run.

**Attempt stream replay** (`pop tasks stream <set>`) lists every run meta (new files plus **legacy** layouts synthesized in memory), sorts by `start_time` into one chronological timeline across implement and verify, then replays paired events. `pop tasks stream <set>/<task>.md` filters to implement runs for that task. At equal timestamps, implement sorts before verify. `--last` at set scope selects the single most recent run overall.

Verifier runs use the same pair shape (`phase: verify`, `work_sha`, optional `verdict` on meta when parsed). Every structured invocation in a verify walk is recorded — quota-paused fall-through included — not only the invocation whose text was parsed.

## Legacy layout (read, do not migrate)

Existing **Captured attempt stream** files (`streams/<task-stem>/attempt-NNN.jsonl.gz`, single gzip with inline header/events/footer) remain on disk unchanged. Readers synthesize a virtual meta (`run_id` stable as `legacy:<task-stem>:attempt-NNN`, `phase: implement`, times/outcome from the inline header/footer) so they participate in the unified timeline. **New writes** go only to `streams/runs/`. No bulk migration or import-merge logic in this slice — uuid pairs merely enable cross-machine merge later as a byproduct.

## Considered Options

- **`streams/_verify/<sha>/attempt-NNN` (ADR-0093).** Superseded: separate verify tree and `_verify` replay target fought a unified analysis timeline.
- **`streams/catalog.jsonl` index.** Rejected: `.meta.json` per run is the index; a catalog would duplicate it.
- **Per-run `.tar.gz` bundles.** Rejected for live writes: tar overhead and rewrite cost; whole-set export already tarballs the directory.
- **Import-time merge/dedupe by `run_id`.** Deferred: out of scope; chronological replay is the goal of this slice.
- **One-time migration of legacy gzips into `runs/`.** Deferred: dual-read is enough; optional migrate command can land later.

## Consequences

- `persist` path writes meta + events; `stream_tracer`, digest carry-forward, and timings readers gain a shared `listRunMetas` over new + legacy sources.
- ADR-0016's event envelope moves payload to `.events.jsonl.gz`; metadata lives in `.meta.json` instead of inline JSONL header (legacy inline header remains for old files only).
- Relates to ADR-0086 (verifier + verdict cache), ADR-0090 (single replay lens).
