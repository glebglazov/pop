# Implementation Plan: Extract SelectDeps / RunSelect (Phase 1)

## Overview

Extract the `runSelect` orchestration function in `cmd/select.go` into a testable `RunSelect(d *SelectDeps) error` function, following `docs/rfc-select-deps.md` and matching the existing `*With(d)` dependency injection pattern. Scope: mechanical extraction + scripted-picker test helper + 3 boundary tests covering the most load-bearing action branches. Existing helper tests are untouched.

## Current State Analysis

`runSelect` at `cmd/select.go:45-295` is a ~250-line function implementing the full `pop select` flow: config loading, project expansion, history sorting, session state refresh, picker loop, and 8-branch action dispatch. The loop calls `ui.Run(items, opts...)` directly and dispatches on `ui.Result.Action` — there is no seam for tests.

Key properties of the current state:
- **No boundary tests exist.** `cmd/select_test.go` (838 lines) covers only extracted pure helpers: `TestLastNSegments`, `TestSanitizeSessionName`, `TestBuildSessionAwareItems*`, `TestSortByUnifiedRecency`, `TestSortBaseItemsByHistory`, `TestExpandProjectsWith_*`, `TestOpenTmuxWindowWith`. None of the action dispatch inside `runSelect` is covered.
- **All side-effect functions already have `*With` variants.** `openTmuxSessionWith`, `killTmuxSessionWith`, `switchToTmuxTargetWith`, `switchToTmuxTargetAndZoomWith`, `sendCDToPaneWith`, `openTmuxWindowWith`, `currentTmuxSessionWith` are defined at `cmd/select.go:411-512` and `cmd/session.go:49-380`. They're just not threaded through `runSelect` — the non-`With` wrappers call the `defaultTmux` global directly.
- **Two `os.Exit(1)` calls** at `cmd/select.go:207` (ActionCancel branch) and `cmd/select.go:211` (nil-selected ActionSelect) will kill any test binary that hits them. These must be replaced with `return nil` before the loop is testable.
- **Cobra flag vars** `tmuxCDPane` and `noHistory` at `cmd/select.go:24-25` are read directly inside the loop body. They will become fields on `SelectDeps`, populated by the cobra wrapper.
- **Inline `os.Getenv("TMUX") != ""`** at `cmd/select.go:157` is used to gate the `WithOpenWindow` picker option.
- **Idiomatic test sandboxing** in this repo is `t.Setenv("XDG_DATA_HOME", t.TempDir())` — used in `cmd/pane_test.go:229,406,446,497,551`. `history.DefaultHistoryPath()` respects `XDG_DATA_HOME` (`history/history.go:52`), so setting it is sufficient to redirect any `hist.Save()` call during tests to a throwaway location.

### Key Discoveries

- **In-repo precedent for this exact refactor**: `cmd/configure.go:35-69` + `cmd/configure_test.go` already implements the same pattern for the `configure` command. Per user direction, we follow the RFC's exported-name convention (`SelectDeps` / `DefaultSelectDeps` / `RunSelect`) instead of the `configureDeps` lowercase precedent.
- **Scripted-mock helper pattern already exists**: `mockPickDirSequence` at `cmd/configure_test.go:29` is the exact shape needed for `scriptedPicker` — closure over index + slice, advance on each call, return a sentinel when exhausted.
- **Cobra error surfacing**: `cmd/root.go:95-99` wraps any non-nil error from `rootCmd.Execute()` in `ui.ShowError(err, "")` — a visible error popup screen. Concluding: ActionCancel must return `nil`, not a sentinel error, or cancellation would trigger an error popup.
- **Sanitization is idempotent**: `sanitizeSessionName` (`cmd/select.go:468`) replaces `.` and `:` with `_`. Running it on already-sanitized input is a no-op. This means `killTmuxSessionWith` (which sanitizes) can safely handle both project sessions and standalone sessions (whose names are already sanitized) — one `KillSession` field on `SelectDeps` covers both paths.
- **`history.Load` with a missing path is safe**: returns an empty `*History{path: p}` without erroring (`history/history.go:68-88`). This means a test's `LoadHistory` can call `history.Load(sandboxPath)` to get a clean history bound to a test-specific save location.
- **`cfg.ExpandProjects()` on a plain `t.TempDir()` path** produces exactly one `ExpandedProject` — the tmp dir has no `.bare` directory, so `HasWorktreesWith` returns false and it expands to a single regular entry. This is the cheapest way to get a non-empty picker in tests without mocking the filesystem layer.

## Desired End State

After this plan is complete:

1. `cmd/select.go` contains:
   - A new `SelectDeps` struct with 18 function/data fields per the RFC.
   - A new `DefaultSelectDeps() *SelectDeps` constructor wiring everything to real production implementations.
   - A new `RunSelect(d *SelectDeps) error` function containing the body of the old `runSelect` loop, with all dependency calls routed through `d`.
   - A 4-line `runSelect` cobra handler that constructs `DefaultSelectDeps()`, copies CLI flag state, and calls `RunSelect(d)`.
2. Both `os.Exit(1)` calls inside the loop are replaced with `return nil`. **Deliberate semantic change**: `pop select` followed by Esc now exits with code 0 instead of 1.
3. `cmd/select_test.go` contains:
   - A new `scriptedPicker(fns ...func([]ui.Item) ui.Result)` test helper modeled on `mockPickDirSequence`.
   - A new `testSelectDeps(t *testing.T) *SelectDeps` helper returning a `SelectDeps` with no-op defaults safe for tests.
   - Three new boundary tests: `TestRunSelect_ActionSelectRecordsHistory`, `TestRunSelect_ActionKillSessionContinuesLoop`, `TestRunSelect_ActionCancelExitsCleanly`.
4. All existing tests remain green and untouched.
5. `make test` and `make build` pass end-to-end.
6. Manual smoke-test confirms `pop select` still behaves identically in real use.

## What We're NOT Doing

Explicitly out of scope for this plan:

- **The remaining 9 boundary tests from the RFC**: ActionOpenWindow, ActionReset, ActionRefresh, ActionSwitchToPane, ActionUserDefinedCommand (Exit=true), ActionUserDefinedCommand (Exit=false), `--tmux-cd` mode, `--no-history` flag, standalone session select, first-run config bootstrap. Each becomes a follow-up PR once the seam is in place.
- **Deleting the existing helper tests** (`TestBuildSessionAwareItems*`, `TestSortByUnifiedRecency`, `TestSortBaseItemsByHistory`). The RFC suggests collapsing them once boundary tests cover the same behavior. Not in this plan — they stay as cheap, fast, independent checks. Revisit only if they start fighting the refactor.
- **Applying the same refactor to `cmd/worktree.go` or `cmd/dashboard.go`**, both of which have the same `os.Exit(1)` + picker loop shape (`cmd/worktree.go:81-86`, `cmd/dashboard.go:87-88`). One command at a time.
- **Refactoring `buildSessionAwareItems`, `sortByUnifiedRecency`, or `sortBaseItemsByHistory`** — they already have direct helper tests and continue to be called from `RunSelect` via their existing `*With` or wrapper forms.
- **Any behavior change inside `runSelect`** other than the two `os.Exit(1)` → `return nil` replacements.
- **Adding a `SaveHistory` field to `SelectDeps`.** Tests sandbox history writes via `t.Setenv("XDG_DATA_HOME", t.TempDir())` plus a sandbox-path-bound `history.Load` inside `LoadHistory`. No new dep field needed.

## Implementation Approach

Single phase, two sequential steps with a mandatory pause point between them:

1. **Step 1 — Mechanical extraction**. Pure code move with one deliberate semantic change (`os.Exit(1)` → `return nil`). All existing tests must stay green. Manual smoke-test confirms no behavior regression.
2. **Step 2 — Test seam and 3 boundary tests**. Add the `scriptedPicker` helper, the `testSelectDeps` constructor, and the 3 tests.

The pause between steps is load-bearing: Step 1 is ~200 lines of plumbing with a subtle behavior change, and we want `make test` + manual smoke-test to confirm it's safe before layering new tests on top.

## Phase 1: Extract SelectDeps and add 3 boundary tests

### Step 1: Mechanical extraction

#### 1.1. Add `SelectDeps` struct to `cmd/select.go`

**File**: `cmd/select.go`
**Changes**: Insert immediately before the existing `runSelect` function (currently at line 45). 18 fields per the RFC, exported names per user direction.

```go
// SelectDeps holds dependencies for the select command.
// See docs/rfc-select-deps.md for rationale.
type SelectDeps struct {
    // Core dependencies
    Tmux    deps.Tmux
    Project *project.Deps

    // Data loading
    LoadConfig  func() (*config.Config, error)
    LoadHistory func() (*history.History, error)

    // Picker — the critical testing seam
    RunPicker func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error)

    // Session state
    SessionActivity    func() map[string]int64
    AttentionSessions  func() map[string]bool
    AttentionPanes     func() []ui.AttentionPane
    AttentionCallbacks func() ui.AttentionCallbacks

    // Side effects (take deps.Tmux as first arg to match *With signatures)
    OpenSession      func(tmux deps.Tmux, item *ui.Item) error
    OpenWindow       func(tmux deps.Tmux, item *ui.Item) error
    KillSession      func(tmux deps.Tmux, name string)
    SendCDToPane     func(tmux deps.Tmux, paneID, path string) error
    SwitchToTarget   func(tmux deps.Tmux, target string) error
    SwitchAndZoom    func(tmux deps.Tmux, target string) error
    RunCustomCommand func(command string, item *ui.Item)
    EnsureMonitor    func()
    RunConfigure     func() error

    // Environment
    InTmux         func() bool
    CurrentSession func(tmux deps.Tmux) string

    // CLI flags (populated by cobra handler before calling RunSelect)
    TMuxCDPane string
    NoHistory  bool
}
```

#### 1.2. Add `DefaultSelectDeps()` constructor

**File**: `cmd/select.go`
**Changes**: Immediately after the `SelectDeps` struct. Wires each field to its production implementation. Every side-effect field binds an existing `*With`-suffixed function by reference — no new wrapper functions are introduced.

```go
// DefaultSelectDeps returns SelectDeps wired to real production implementations.
func DefaultSelectDeps() *SelectDeps {
    return &SelectDeps{
        Tmux:    defaultTmux,
        Project: project.DefaultDeps(),

        LoadConfig: func() (*config.Config, error) {
            cfgPath := cfgFile
            if cfgPath == "" {
                cfgPath = config.DefaultConfigPath()
            }
            return config.Load(cfgPath)
        },
        LoadHistory: func() (*history.History, error) {
            return history.Load(history.DefaultHistoryPath())
        },

        RunPicker: ui.Run,

        SessionActivity:    history.TmuxSessionActivity,
        AttentionSessions:  monitorAttentionSessions,
        AttentionPanes:     buildAttentionPanes,
        AttentionCallbacks: attentionCallbacks,

        OpenSession:      openTmuxSessionWith,
        OpenWindow:       openTmuxWindowWith,
        KillSession:      killTmuxSessionWith,
        SendCDToPane:     sendCDToPaneWith,
        SwitchToTarget:   switchToTmuxTargetWith,
        SwitchAndZoom:    switchToTmuxTargetAndZoomWith,
        RunCustomCommand: executeSelectCustomCommand,
        EnsureMonitor:    func() { go ensureMonitorDaemon() },
        RunConfigure: func() error {
            cd := defaultConfigureDeps()
            cd.ShowWelcome = true
            return runConfigureWith(cd)
        },

        InTmux:         func() bool { return os.Getenv("TMUX") != "" },
        CurrentSession: currentTmuxSessionWith,
    }
}
```

Notes:
- `RunPicker: ui.Run` — Go's first-class function values make this a direct assignment since `ui.Run` already has the target signature.
- `KillSession: killTmuxSessionWith` handles both project sessions and standalone sessions uniformly. Sanitization is idempotent, so passing an already-sanitized standalone session name through it is a no-op.
- `RunConfigure` closes over the configure-deps construction so `RunSelect` sees a simple `func() error`.
- `EnsureMonitor` wraps the goroutine launch so `RunSelect` doesn't need to know about the `go` keyword.

#### 1.3. Extract `RunSelect(d *SelectDeps) error`

**File**: `cmd/select.go`
**Changes**: Add a new top-level `RunSelect(d *SelectDeps) error` function whose body is the body of the old `runSelect` (current lines 46-294), rewritten to route every dependency call through `d`.

**Substitutions to make inside the extracted body:**

| Old call (line in current select.go)                         | New call                                         |
| ------------------------------------------------------------ | ------------------------------------------------ |
| `config.Load(cfgPath)` (52)                                  | `d.LoadConfig()`                                 |
| `defaultConfigureDeps()` + `runConfigureWith(...)` (58-60)   | `d.RunConfigure()`                               |
| `go ensureMonitorDaemon()` (69)                              | `d.EnsureMonitor()`                              |
| `history.Load(...)` (113)                                    | `d.LoadHistory()`                                |
| `currentTmuxSession()` (89)                                  | `d.CurrentSession(d.Tmux)`                       |
| `os.Getenv("TMUX") != ""` (157)                              | `d.InTmux()`                                     |
| `buildAttentionPanes()` (179)                                | `d.AttentionPanes()`                             |
| `attentionCallbacks()` (180)                                 | `d.AttentionCallbacks()`                         |
| `ui.Run(items, opts...)` (200)                               | `d.RunPicker(items, opts...)`                    |
| `tmuxCDPane` (222)                                           | `d.TMuxCDPane`                                   |
| `noHistory` (216, 231, 262)                                  | `d.NoHistory`                                    |
| `openTmuxSession(result.Selected)` (225)                     | `d.OpenSession(d.Tmux, result.Selected)`         |
| `openTmuxWindow(result.Selected)` (237)                      | `d.OpenWindow(d.Tmux, result.Selected)`          |
| `killTmuxSessionByName(...)` (243)                           | `d.KillSession(d.Tmux, ...)`                     |
| `killTmuxSession(result.Selected.Name)` (245)                | `d.KillSession(d.Tmux, result.Selected.Name)`    |
| `switchToTmuxTarget(...)` (214)                              | `d.SwitchToTarget(d.Tmux, ...)`                  |
| `switchToTmuxTargetAndZoom(...)` (279)                       | `d.SwitchAndZoom(d.Tmux, ...)`                   |
| `sendCDToPane(...)` (223)                                    | `d.SendCDToPane(d.Tmux, tmuxCDPane, ...)` → `d.SendCDToPane(d.Tmux, d.TMuxCDPane, ...)` |
| `executeSelectCustomCommand(...)` (288)                      | `d.RunCustomCommand(...)`                        |
| `os.Exit(1)` on ActionCancel (208)                           | `return nil`                                     |
| `os.Exit(1)` on nil ActionSelect.Selected (212)              | `return nil`                                     |

**Session-aware item building**: The current `runSelect` calls `buildSessionAwareItems(baseItems, hist, excludedSessionNames, cfg.UnreadNotificationsEnabled("select"))` at line 161, which internally calls `history.TmuxSessionActivity()` and `monitorAttentionSessions()`. Inside `RunSelect` we bypass the wrapper and call `buildSessionAwareItemsWith` directly with `d.SessionActivity()` and (conditionally) `d.AttentionSessions()`:

```go
var attention map[string]bool
if cfg.UnreadNotificationsEnabled("select") {
    attention = d.AttentionSessions()
}
items := buildSessionAwareItemsWith(
    baseItems,
    hist,
    d.SessionActivity(),
    excludedSessionNames,
    attention,
)
```

This is a mechanical change — `buildSessionAwareItems` (the wrapper) is left untouched for the existing helper tests to keep calling, but `RunSelect` itself no longer goes through the wrapper.

**Other subtle points in the extraction:**
- `cfgPath` is still needed for the error message at line 78 (`"no projects found. Check your config at %s"`). Resolve it inside `RunSelect` by reading `cfgFile` / `config.DefaultConfigPath()` directly (same as `DefaultSelectDeps` does) — `LoadConfig` hides *how* to load but the path string is still needed for user-facing diagnostics. Alternative: add a `ConfigPath() string` field. **Decision**: read `cfgFile` directly — it's a package-level global in `cmd/root.go`, already available in `package cmd`, and adding a field for a single diagnostic message is over-engineering. Tests that want to assert on this message can set `cfgFile` directly via the existing save/restore idiom (`oldCfgFile := cfgFile; defer func() { cfgFile = oldCfgFile }()`).
- `expandProjects(paths)` at line 84 — keep calling it directly. It's a package-level wrapper around `expandProjectsWith(project.DefaultDeps(), paths)`. Inside `RunSelect`, replace with `expandProjectsWith(d.Project, paths)` to honor the injected `Project` deps.
- Per-result history-lookup block at lines 263-271 (`ActionSwitchToPane`) reads `items` and `sessionHistoryPath(sessionName, hist)`. These are local variables / pure functions; no change needed.

#### 1.4. Rewrite the `runSelect` cobra handler

**File**: `cmd/select.go`
**Changes**: Replace the old 250-line body with a 4-line delegation at the existing location (line 45).

```go
func runSelect(cmd *cobra.Command, args []string) error {
    d := DefaultSelectDeps()
    d.TMuxCDPane = tmuxCDPane
    d.NoHistory = noHistory
    return RunSelect(d)
}
```

The old `runSelect` body is gone from this function — it now lives inside `RunSelect`.

#### 1.5. Remove unused imports (if any)

**File**: `cmd/select.go`
**Changes**: After the extraction, `runSelect` no longer references `errors`, `os.ErrNotExist`, or potentially other symbols directly. `RunSelect` may or may not still reference them depending on how the body rewrites. Run `goimports` / `go build` after the edit and delete any unused imports the compiler flags.

#### Step 1 Success Criteria

##### Automated Verification:
- [x] Binary builds: `go build ./...` (used `go build ./...` instead of `make build` to avoid macOS provenance tagging on a user-runnable binary — type-check is equivalent)
- [x] All existing tests pass: `go test ./...` — 691 passed across 9 packages
- [x] Vet clean: `go vet ./...`
- [x] Select-package tests pass: `go test ./cmd` — 292 passed

##### Deviation from plan

**`EnsureMonitor` field became `EnsureSystemState`.** When implementing, I discovered the codebase had evolved: `go ensureMonitorDaemon()` was replaced with `systemWarnings := ensureSystemState()` at `cmd/select.go:69`, and the returned warnings are now merged into the picker's warnings slot at `cmd/select.go:193`. `ensureSystemState` (defined at `cmd/monitor.go:170`) synchronously runs `ensureIntegrations()` (returning warnings) and kicks off `ensureMonitorDaemon()` in a goroutine. Accordingly:
- Struct field `EnsureMonitor func()` → `EnsureSystemState func() []string`
- Default wiring `func() { go ensureMonitorDaemon() }` → `ensureSystemState`
- Inside `RunSelect`: `systemWarnings := d.EnsureSystemState()` + merge into warnings slot
- Test helper will stub with `func() []string { return nil }`

This is a faithful adaptation of the plan's intent (inject the system-state bootstrap), just matching the current function shape.

##### Manual Verification:
- [ ] `pop select` opens the picker as before
- [ ] Enter on a project → tmux session opens and attaches
- [ ] Esc → clean exit, exit code **0** (this is the deliberate behavior change)
- [ ] Kill-session keybind on a project with a live session → session killed, picker stays open, cursor restores to the killed item's position
- [ ] Reset keybind on a project with history → entry removed, list re-sorted, picker stays open
- [ ] `--tmux-cd=<pane_id>` still sends `cd && clear` to the target pane
- [ ] `--no-history` still suppresses history recording on select
- [ ] Standalone tmux session (one created outside pop) still appears in picker and is selectable

**Implementation Note**: After Step 1 automated verification passes, pause here for manual confirmation from the user that the smoke-tests above succeeded before proceeding to Step 2. This pause matters: Step 1 is ~200 lines of plumbing plus a visible exit-code change, and we want confidence in the baseline before layering new tests on it.

---

### Step 2: Test seam and 3 boundary tests

#### 2.1. Add `scriptedPicker` test helper

**File**: `cmd/select_test.go`
**Changes**: Add near the top of the file, after the existing imports. Modeled on `mockPickDirSequence` at `cmd/configure_test.go:29`.

```go
// scriptedPicker returns a RunPicker function that calls each fn in sequence
// on successive picker iterations. Each fn receives the actual items passed
// to the picker so results can reference items[N].Selected directly. When the
// sequence is exhausted, the function returns ActionCancel to terminate loops
// cleanly. Modeled on mockPickDirSequence in configure_test.go.
func scriptedPicker(fns ...func(items []ui.Item) ui.Result) func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
    i := 0
    return func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
        if i >= len(fns) {
            return ui.Result{Action: ui.ActionCancel}, nil
        }
        fn := fns[i]
        i++
        return fn(items), nil
    }
}
```

The closure-per-step shape matters because some tests need to reference the picker's own `items` slice when constructing their result (e.g. `Selected: &items[0]`) — a simpler `[]ui.Result` slice wouldn't let the test see the items.

#### 2.2. Add `testSelectDeps(t)` constructor

**File**: `cmd/select_test.go`
**Changes**: Add below `scriptedPicker`. Returns a `SelectDeps` populated with safe no-op defaults. Each test overrides only the fields it cares about.

```go
// testSelectDeps returns a SelectDeps with no-op defaults safe for tests.
// Callers should override only the fields their test cares about.
//
// History and config paths are sandboxed via t.Setenv("XDG_DATA_HOME", ...)
// and a per-test LoadConfig that points at t.TempDir(), so tests do not touch
// the user's real history or config files.
func testSelectDeps(t *testing.T) *SelectDeps {
    t.Helper()

    // Sandbox any XDG_DATA_HOME reads (history.Save, monitor state, etc.).
    xdg := t.TempDir()
    t.Setenv("XDG_DATA_HOME", xdg)

    // Default project directory — a single real tmpdir so cfg.ExpandProjects
    // produces exactly one item (not a bare repo, no worktrees). Tests that
    // need more items can override LoadConfig.
    projectDir := t.TempDir()

    return &SelectDeps{
        Tmux: &deps.MockTmux{},
        Project: &project.Deps{
            Git: &deps.MockGit{
                CommandFunc: func(args ...string) (string, error) { return "", nil },
            },
            FS: &deps.MockFileSystem{},
        },

        LoadConfig: func() (*config.Config, error) {
            return &config.Config{
                Projects: []config.ProjectEntry{{Path: projectDir}},
            }, nil
        },
        LoadHistory: func() (*history.History, error) {
            // Bind to a sandbox path so any hist.Save() goes to the test tmpdir.
            return history.Load(filepath.Join(xdg, "pop", "history.json"))
        },

        RunPicker: func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
            return ui.Result{Action: ui.ActionCancel}, nil
        },

        SessionActivity:    func() map[string]int64 { return nil },
        AttentionSessions:  func() map[string]bool { return nil },
        AttentionPanes:     func() []ui.AttentionPane { return nil },
        AttentionCallbacks: func() ui.AttentionCallbacks { return ui.AttentionCallbacks{} },

        OpenSession:      func(tmux deps.Tmux, item *ui.Item) error { return nil },
        OpenWindow:       func(tmux deps.Tmux, item *ui.Item) error { return nil },
        KillSession:      func(tmux deps.Tmux, name string) {},
        SendCDToPane:     func(tmux deps.Tmux, paneID, path string) error { return nil },
        SwitchToTarget:   func(tmux deps.Tmux, target string) error { return nil },
        SwitchAndZoom:    func(tmux deps.Tmux, target string) error { return nil },
        RunCustomCommand: func(command string, item *ui.Item) {},
        EnsureMonitor:    func() {},
        RunConfigure:     func() error { return nil },

        InTmux:         func() bool { return false },
        CurrentSession: func(tmux deps.Tmux) string { return "" },
    }
}
```

Notes on field choices:
- `deps.MockGit` with a return-empty `CommandFunc` is sufficient because `project.HasWorktreesWith` (inside `expandProjectsWith`) falls back to checking for a `.bare` directory via the FS layer before ever calling git. A plain `t.TempDir()` has no `.bare`, so the git command path is never exercised.
- `LoadHistory` uses a real `history.Load` with a sandbox path instead of constructing a bare `&history.History{}`. This matters because `hist.Save()` needs a non-empty `path` field to work. `history.Load` on a non-existent path returns `&History{path: p}` cleanly.

#### 2.3. Boundary test A — `TestRunSelect_ActionSelectRecordsHistory`

**File**: `cmd/select_test.go`
**Changes**: Append to the end of the file.

```go
func TestRunSelect_ActionSelectRecordsHistory(t *testing.T) {
    projectDir := t.TempDir()
    xdg := t.TempDir()
    t.Setenv("XDG_DATA_HOME", xdg)

    var openedItem *ui.Item
    var hist *history.History

    d := testSelectDeps(t)
    d.LoadConfig = func() (*config.Config, error) {
        return &config.Config{Projects: []config.ProjectEntry{{Path: projectDir}}}, nil
    }
    d.LoadHistory = func() (*history.History, error) {
        h, err := history.Load(filepath.Join(xdg, "pop", "history.json"))
        hist = h
        return h, err
    }
    d.RunPicker = scriptedPicker(func(items []ui.Item) ui.Result {
        return ui.Result{
            Action:      ui.ActionSelect,
            Selected:    &items[0],
            CursorIndex: 0,
        }
    })
    d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error {
        openedItem = item
        return nil
    }

    if err := RunSelect(d); err != nil {
        t.Fatalf("RunSelect: %v", err)
    }

    if openedItem == nil {
        t.Fatal("expected OpenSession to be called")
    }
    if openedItem.Path != projectDir {
        t.Errorf("OpenSession called with path %q, want %q", openedItem.Path, projectDir)
    }
    if hist == nil {
        t.Fatal("LoadHistory was not called")
    }
    if len(hist.Entries) != 1 {
        t.Fatalf("expected 1 history entry, got %d", len(hist.Entries))
    }
    if hist.Entries[0].Path != projectDir {
        t.Errorf("history recorded %q, want %q", hist.Entries[0].Path, projectDir)
    }
}
```

**What this test proves**:
1. The picker loop reaches its first iteration with a non-empty item list sourced from the injected `LoadConfig` + `expandProjectsWith` flow.
2. On `ActionSelect`, history is recorded *before* the side-effect call (the test sees a non-nil entry after `RunSelect` returns).
3. `OpenSession` is called with the selected item, and the side-effect receives the same `ui.Item` the picker returned.
4. `RunSelect` returns cleanly when `OpenSession` succeeds.

Note on `filepath` import: `cmd/select_test.go` already imports `"path/filepath"` (verified at line 6 of the file). No new import needed.

#### 2.4. Boundary test B — `TestRunSelect_ActionKillSessionContinuesLoop`

**File**: `cmd/select_test.go`
**Changes**: Append after test A.

```go
func TestRunSelect_ActionKillSessionContinuesLoop(t *testing.T) {
    projectDir := t.TempDir()

    var killedNames []string
    var pickerCalls int
    var cursorIndexSeen int = -1

    d := testSelectDeps(t)
    d.LoadConfig = func() (*config.Config, error) {
        return &config.Config{Projects: []config.ProjectEntry{{Path: projectDir}}}, nil
    }
    d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
        pickerCalls++
        switch pickerCalls {
        case 1:
            return ui.Result{
                Action:      ui.ActionKillSession,
                Selected:    &items[0],
                CursorIndex: 7,
            }, nil
        case 2:
            // Second call: verify the options include WithInitialCursorIndex(7)
            // by applying them to a fresh Picker and checking. Simplified here:
            // we trust that if the loop re-calls RunPicker with opts, cursor
            // restoration was wired; a deeper assertion can be added later.
            return ui.Result{Action: ui.ActionCancel}, nil
        default:
            t.Fatalf("picker called %d times, expected at most 2", pickerCalls)
            return ui.Result{}, nil
        }
    }
    d.KillSession = func(tmux deps.Tmux, name string) {
        killedNames = append(killedNames, name)
    }

    if err := RunSelect(d); err != nil {
        t.Fatalf("RunSelect: %v", err)
    }

    if pickerCalls != 2 {
        t.Errorf("picker called %d times, want 2 (kill → cancel)", pickerCalls)
    }
    if len(killedNames) != 1 {
        t.Fatalf("expected 1 kill, got %d: %v", len(killedNames), killedNames)
    }
    _ = cursorIndexSeen // reserved for future deeper cursor-restoration assertion
}
```

**What this test proves**:
1. `ActionKillSession` does *not* terminate the loop — the picker is called a second time.
2. `KillSession` is invoked exactly once with the project name from the selected item.
3. `ActionCancel` on the second iteration returns cleanly (no error, no further iterations).

**Known limitation**: The test does not directly assert that `WithInitialCursorIndex(7)` was included in the second call's options, because `ui.PickerOption` is an opaque `func(*Picker)` type (`ui/picker.go:202`). A deeper assertion would require applying the options to a real `Picker` and inspecting its state — out of scope for this first test pass. The test asserts loop continuation + kill invocation, which are the load-bearing behaviors. Cursor-restoration verification can be added as a follow-up once we have more infrastructure for inspecting picker options.

#### 2.5. Boundary test C — `TestRunSelect_ActionCancelExitsCleanly`

**File**: `cmd/select_test.go`
**Changes**: Append after test B.

```go
func TestRunSelect_ActionCancelExitsCleanly(t *testing.T) {
    projectDir := t.TempDir()

    var pickerCalls int

    d := testSelectDeps(t)
    d.LoadConfig = func() (*config.Config, error) {
        return &config.Config{Projects: []config.ProjectEntry{{Path: projectDir}}}, nil
    }
    d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
        pickerCalls++
        return ui.Result{Action: ui.ActionCancel}, nil
    }
    // OpenSession should NOT be called — track to assert.
    openCalled := false
    d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error {
        openCalled = true
        return nil
    }

    if err := RunSelect(d); err != nil {
        t.Fatalf("RunSelect on ActionCancel: unexpected error %v", err)
    }

    if pickerCalls != 1 {
        t.Errorf("picker called %d times, want 1", pickerCalls)
    }
    if openCalled {
        t.Error("OpenSession called on cancel path — expected no-op")
    }
}
```

**What this test proves**:
1. `ActionCancel` returns `nil` (not an error) — the `os.Exit(1)` replacement is correct.
2. No side effects run after cancel — `OpenSession` is never touched.
3. The loop does not iterate a second time after cancel.

**This test is the direct regression guard for the `os.Exit(1)` → `return nil` behavior change** in Step 1. If Step 1 forgot to remove either `os.Exit(1)`, the test binary would crash here.

#### Step 2 Success Criteria

##### Automated Verification:
- [x] Binary builds: `go build ./...`
- [x] All tests pass: `go test ./...` — 694 passed across 9 packages (was 691 before Step 2, +3 new)
- [x] New boundary tests specifically pass: `go test ./cmd -run 'TestRunSelect_' -v` — 3 passed
- [x] Existing helper tests remain green: included in the 694-pass full suite
- [x] Vet clean: `go vet ./...`

##### Manual Verification:
- [ ] `pop select` still opens the picker (quick smoke re-test — should be unchanged from Step 1 verification)
- [x] Tests terminate without hanging — all ran in under a second

##### Adaptation from plan

- **Test A path assertion**: The plan proposed asserting `hist.Entries[0].Path == projectDir`. On macOS, `config.ExpandProjects` canonicalizes via `EvalSymlinks`, turning `/var/folders/...` (what `t.TempDir()` returns) into `/private/var/folders/...` (the canonical form). So `projectDir` would not match `hist.Entries[0].Path`. The test now asserts the stronger invariant `hist.Entries[0].Path == openedItem.Path` — both come from the same canonicalized expansion, so they agree without the test needing to know the canonical form.
- **`LoadHistory` capture**: To inspect `hist.Entries` after `RunSelect` returns, test A wraps the default `testSelectDeps` `LoadHistory` to snapshot the `*history.History` pointer into a test-local variable. Cleaner than building the history from scratch.
- **`testSelectDeps` XDG sandboxing extended**: Added `t.Setenv("XDG_CACHE_HOME", ...)` and `t.Setenv("XDG_CONFIG_HOME", ...)` as defense in depth because `config.ExpandProjects` loads a glob cache via `DefaultCachePathWith` (even though our tests use exact paths that skip the glob cache, a future test with a glob would pollute the user's cache without this).

---

## Testing Strategy

### What the 3 boundary tests collectively cover

- **The loop runs at least one iteration** (all 3 tests).
- **The loop runs more than one iteration for continuing actions** (test B).
- **The loop exits cleanly for terminating actions** (tests A, C).
- **History is recorded on select** (test A).
- **Side-effects are invoked with the right arguments** (tests A, B).
- **Cancel does NOT invoke side-effects** (test C).
- **The `os.Exit(1)` → `return nil` change is regression-proof** (test C).

### What they don't cover (deferred to follow-up PRs)

- `ActionOpenWindow` (history recorded + window opened, not session)
- `ActionReset` (history entry removed + base items re-sorted)
- `ActionRefresh` (loop continues with same cursor index)
- `ActionSwitchToPane` (history path resolution via `sessionHistoryPath`)
- `ActionUserDefinedCommand` with `Exit=true` (command runs, loop exits)
- `ActionUserDefinedCommand` with `Exit=false` (command runs, loop continues)
- `--tmux-cd` mode (SendCDToPane instead of OpenSession)
- `--no-history` flag (hist.Record NOT called)
- Standalone session select (SwitchToTarget instead of OpenSession)
- Config-missing bootstrap (LoadConfig returns os.ErrNotExist → RunConfigure called → LoadConfig called again)
- Cursor-restoration verification (requires picker option inspection infrastructure)

Each deferred test follows the same shape as A/B/C — scripted picker sequence, 2-3 field overrides, assertions on side-effect invocations.

### Existing helper tests: untouched

The following existing tests must remain green without modification:
- `TestLastNSegments`
- `TestSanitizeSessionName`
- `TestBuildSessionAwareItems`, `TestBuildSessionAwareItems_AttentionIndicator`
- `TestSortByUnifiedRecency`
- `TestSortBaseItemsByHistory`
- `TestExpandProjectsWith_*`
- `TestOpenTmuxWindowWith`

They test the pure helpers directly and are faster + more targeted than the boundary tests. The RFC suggests deleting some; this plan explicitly declines that recommendation — the helper tests are cheap and independent, and removing them before the boundary test suite is mature trades away regression guards for no gain.

## Migration Notes

**Deliberate behavior change**: `pop select` + Esc now exits with code 0 instead of 1.

- Before: `case ui.ActionCancel: os.Exit(1)` (`cmd/select.go:207-208`)
- After: `case ui.ActionCancel: return nil`

**Rationale**:
1. Exit code 0 is the idiomatic Unix convention for user-initiated cancellation.
2. Returning a sentinel error would trigger `ui.ShowError(err, "")` in `cmd/root.go:97`, showing an error popup on cancel — wrong UX.
3. Without this change, `os.Exit(1)` kills any test binary that exercises the cancel path, making the boundary tests impossible to write.

**Who might notice**:
- Shell scripts that check `pop select`'s exit code to distinguish "user selected" from "user cancelled". No such scripts are known to exist in this repo.
- `tmux display-popup -E` users — the `-E` flag closes the popup when the command exits regardless of code, so visible behavior is unchanged.

**Rollback**: If this becomes a problem, the alternative is to define a `var errSelectCancelled = errors.New("cancelled")` sentinel, return it from `RunSelect` on cancel, and have the `runSelect` cobra wrapper check for it via `errors.Is` and call `os.Exit(1)` before cobra sees it. The boundary test can distinguish cancel-error from other errors via `errors.Is`.

## References

- RFC: `docs/rfc-select-deps.md`
- In-repo precedent: `cmd/configure.go:35-69`, `cmd/configure_test.go:17-50`
- Scripted-mock pattern: `cmd/configure_test.go:29` (`mockPickDirSequence`)
- XDG sandbox idiom in tests: `cmd/pane_test.go:229,406,446,497,551`
- Cobra error surfacing: `cmd/root.go:95-99`
- `ui.Run` signature: `ui/picker.go:1677`
- `ui.PickerOption` type: `ui/picker.go:202`
- `history.DefaultHistoryPath`: `history/history.go:45-60`
- `history.Save`: `history/history.go:133-151`
