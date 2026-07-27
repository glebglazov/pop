---
status: accepted
---

# Package-internal test parallelism rides per-test Deps, not process env

## Context

After [ADR-0144](0144-behavior-tests-live-at-the-domain-contract-and-real-io-sits-behind-seams.md)'s pass, cold `go test ./...` is ~103s and the shape of the remaining cost is known: packages already run concurrently, so wall time is the slowest package — `tasks` (861 tests, 93.7s isolated), then `cmd` (533, 52.1s), then `queue` (240, 29.0s). Within every package the tests run serially; `t.Parallel()` is essentially unused. ADR-0144 deferred unblocking it, citing 264 `t.Setenv` calls, ~80 `Chdir` uses, and 84 hook stubs.

Measurement for this ADR shrank that picture. The blockers are: 247 `t.Setenv` calls of which 187 are `XDG_DATA_HOME` routing (tests pointing the store at a `t.TempDir()`), 84 `os.Chdir` calls concentrated in `cmd`-driving tests, and only **three** real function-typed hook vars in `tasks` (`retryDelayWaitHook`, `interruptGateExit`, `newRunID`) — the "84 stubs" count was mostly benign restore idioms. Crucially, the production seams already exist: `tasks` resolves the data dir through `deps.FileSystem.Getenv` (`popDataDirWith`/`DrainStorePathWith`), and repo/cwd resolution already takes explicit `cwd` params that fall back to `Getwd` only on `""`. Tests reach for `t.Setenv`/`os.Chdir` only because they exercise call paths that use the package-global `defaultDeps` (which reads the real process env) or `cmd` entrypoints that pass `""`. The store layer is already parallel-safe (per-test template copy, ADR-0144 slice 04); ports are ephemeral; tmux is fully faked.

## Decision

- **Tests get isolation from a per-test `Deps`, never from process-global state.** A single `newTestDeps(t)` helper in `tasks` builds a `Deps` whose fake `FileSystem` maps `XDG_DATA_HOME` (and friends) to `t.TempDir()`, with the fake runner, template store, and fast recovery intervals pre-wired. Tests migrate to it and to the existing `...With(d)` variants; `t.Setenv`/`os.Chdir` disappear from migrated tests, and `t.Parallel()` goes on as each family is cleared.
- **The existing `FS.Getenv` seam shape is kept.** No refactor to resolved-path `Deps` fields; the codebase already chose env-func-through-`FileSystem`, and the win comes from tests injecting a fake `FileSystem`, not from a new production seam.
- **Cwd is passed explicitly, not entered.** Callers (tests and the `cmd` layer) pass an explicit dir instead of relying on the `"" → Getwd` fallback; all 84 `os.Chdir` sites in seamed packages die. (`t.Chdir` is no escape hatch — it also panics under `t.Parallel`.)
- **`defaultDeps` stays, for production only.** It carries the ADR-0140 process-cached store handle and remains the production edge; tests simply never route through it.
- **`retryDelayWaitHook` folds into `tasks.Deps`** (it is a time seam, joining the recovery-cadence fields). Tests stubbing `interruptGateExit`, `newRunID`, or `PATH` (fake binaries on the path in `tasks`, `internal/tmux`, `internal/deps`) **stay serial deliberately** — a handful of serial tests among 861 costs nothing, and seaming `PATH` is not worth it.
- **Rollout is in critical-path order with measure gates**: `tasks` first, re-measure; then `cmd` (threading explicit dir + deps through verb entrypoints kills its 46 chdir + 92 `XDG_DATA_HOME` setenv; its TMUX/env-heavy tests stay serial); then `queue`. The `tasks/main_test.go` `TestMain` env-set stays as a safety net during migration (set-once-before-tests is parallel-safe, and `guardTestStorePath` backs it) and is deleted in the final `tasks` slice.
- **Stop rule: 15s is the stretch, not the gate.** Parallelize the current ceiling package; stop when cold `make test` is ≤ ~20s or the next package's seam cost clearly exceeds its wall-time win.
- **Race detection lives in a separate `make test-race`** (`go test -race -shuffle=on ./...`), run manually as a habit and wired as a release gate in `release.sh`. The main `make test` stays fast. New tests are expected to be parallel by convention; no linter enforces it.

## Considered Options

- **Resolved-path fields on `Deps` (data dir, config dir) instead of `FS.Getenv`** — rejected: a production refactor against the shape the codebase already has, for no additional test isolation.
- **Kill the `defaultDeps` global and thread `Deps` everywhere** — rejected: contradicts ADR-0140's process-cached store handle and touches production for a problem that only exists in tests.
- **Opportunistic parallelism only (mark clean tests, leave `t.Setenv` tests serial)** — rejected: the blockers sit across `tasks`' big test families, not in a corner; the 93s floor would barely move.
- **`-race` in the main `make test`** — rejected: 2-5× slowdown fights the very target; a separate target plus release gate catches races where it is mandatory.
- **A `paralleltest`-style linter** — rejected: solo repo; deliberate-serial tests would need noise-y suppressions; ADR + convention suffice.

## Consequences

- Expected cold `make test`: ~103s → ~15-20s (tasks ~93.7s → ~10s at 12-way parallelism, then cmd and queue each drop below the new ceiling in turn). Each package slice re-measures before the next starts, and the stop rule can cancel remaining slices.
- A future reader finding serial tests (PATH stubs, `interruptGateExit`, `newRunID`, TMUX-env `cmd` tests) should not "fix" them by seaming — serial is the chosen trade for those families.
- Races introduced by new parallel tests surface in `make test-race` at release, not in the fast loop; a flaky-under-shuffle failure there is a real bug, not test noise.
- No glossary changes: like ADR-0144, test-suite parallelism is architecture, not domain language.

## Addendum: measured result (slices 01-07 landed)

Cold `-count=1` timings, measured isolated per package (`go test -count=1 ./<pkg>/...`) plus one cold `go test -count=1 ./...` for the real `make test` total:

| Package | ADR-0144 baseline | After ADR-0145 | Note |
|---|---|---|---|
| `tasks` | 93.7s | 35.2s | slices 01–03 migrated to per-test `Deps` + `t.Parallel()`; deliberate-serial PATH/`interruptGateExit`/`newRunID` families remain |
| `cmd` | 52.1s | 16.0s | slices 04–05 migrated; TMUX/env-behavior tests deliberately serial |
| `queue` | 29.0s | 15.4s | slice 06 **skipped** (stop rule) — no intra-package migration; timing reflects package-level `./...` concurrency only |
| **cold `make test` total** (`go test -count=1 ./...`) | **103.2s** | **48.0s** | wall time; `tasks` (~35s) is still the critical path |

Result: cold total dropped ~103s → ~48s (~53% cut), moving toward but not reaching the ADR's ~15–20s stop-rule target. `tasks` at ~35s isolated remains the suite ceiling — within-package parallelism cut it from 93.7s, but deliberate-serial families and less-than-full saturation on an 8-core machine leave headroom above the ~10s projection.

**Slice 06 (`queue`) skipped:** after slices 01–05, cold `./...` total was ~54s with `tasks` (~48s in the concurrent run) as the wall-time ceiling; queue (~29s) could not shrink total further, and the ~19 `XDG_DATA_HOME` setenv migration cost exceeded the marginal suite win — per the stop rule.

**Slice 07 (`make test-race` release gate):** no timing impact; `make test-race` (`go test -race -shuffle=on ./...`) passes at ~211s wall time.

No package regressed against its ADR-0144 baseline.
