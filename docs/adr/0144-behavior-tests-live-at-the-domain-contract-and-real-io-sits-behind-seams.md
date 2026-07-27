---
status: accepted
---

# Behavior tests live at the domain contract; real subprocesses and clocks sit behind seams; cmd keeps a smoke layer

## Context

A cold `make test` takes ~260s of wall time, dominated by `tasks` (~200-260s), `cmd` (~60-90s), and `queue` (~30-45s). Measurement showed a broad tax, not outliers: 100+ `TestRunTask*`/`TestRunTaskSet*` tests each spawn real `/bin/sh` agent shims through `RealCommandRunner` and init real git repos (~1s each); every store open writes an on-disk `pop.db` with WAL and full forward migrations (no `:memory:` anywhere); a handful of tests block on real timers (`recovery.go`'s 2s `fastCheckInterval` ticker ≈20s across the quota family, a 2s retry sleep in `verify_test.go` costing 8s in one test). Separately, ~2,500 test lines re-cover domain behavior through a second layer: `cmd/tasks_test.go` and the `cmd/*_batch_test.go` files re-drive real `tasks` apply logic already unit-tested in `tasks/`, and a cluster in `queue/dashboard_test.go` re-asserts `work/` derivation extracted by [ADR-0143](0143-the-work-dashboard-data-core-lives-in-a-work-package.md).

## Decision

- **The stable domain contract is where behavior tests live.** Following Khorikov's resistance-to-refactoring criterion: when the same observable behavior is asserted both at a domain package's exported surface (`tasks`, `work`) and again through a higher layer (`cmd`, `queue` rendering), the domain-level test is the canonical one and the higher-layer duplicate is deleted. Altitude alone does not win — API stability does.
- **`cmd` keeps a deliberate smoke layer, not full e2e coverage.** Roughly one end-to-end test per verb proves wiring (flags → handler → persisted state); the deleted breadth is intentional, not a coverage gap. Wiring/dispatch tests that stub the domain stay as-is.
- **Real subprocess execution is opt-in, not the default.** The drain-orchestration family converts to an in-process fake `CommandRunner` (the seam already exists); a small named set (~5) of real-shim smoke tests keeps the real-shell path honest. Real `git` stays only where git behavior itself is under test.
- **Wall-clock time is injected through `tasks.Deps`** — the one ADR-sized production seam in this pass: `recovery.go`'s fast-check/poll intervals and the verify retry delays become injectable so tests advance time without waiting.
- **Test stores come from a pre-migrated template** (one migrated `pop.db` copied per test) instead of paying migration on every open.
- **Package-internal parallelism (`t.Parallel()`) is deliberately deferred.** It is the biggest theoretical lever (~2,600 serial tests), but it is structurally blocked today: 264 `t.Setenv` calls (panics under `t.Parallel`), ~80 `Chdir` uses, and 84 package-level hook/var stubs. Unblocking it means moving env/cwd/hooks into `Deps` across ~66 files — its own future ADR, not a rider on this one.

## Considered Options

- **Keep the higher-level (cmd) duplicates and delete domain tests** — rejected: the cmd surface re-drives domain logic through slower real-store e2e runs, and the domain packages' exported ops are the more stable API; deleting the fast layer would keep the expensive one.
- **Do the parallel-enablement refactor now** — rejected as scope: touches 66+ files of test setup plus production seams; sequenced after this pass so its win lands on an already-cheap suite.
- **Shared-cache `:memory:` sqlite instead of a template file** — either is acceptable; template-copy chosen as least invasive to `store.Open`'s contract.

## Consequences

- Expected cold `make test`: ~260s → roughly 50-80s (packages run concurrently; `tasks` remains the critical path at ~40-70s). The <15s target explicitly waits on the deferred parallelism ADR.
- A future engineer seeing thin `cmd` coverage or the fake runner should not "fix" it by adding e2e breadth — that shape is deliberate.
- `queue/dashboard_test.go` keeps only TUI-specific assertions; derivation assertions live solely in `work/` per ADR-0143.
- The intra-`tasks` verify-cache triple coverage (verify/drain-verify/invalidate files) is noted but out of scope for the deletion pass.
- No glossary changes: test-suite layering is architecture, not domain language.
