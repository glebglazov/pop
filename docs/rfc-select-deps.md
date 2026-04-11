# RFC: Extract SelectDeps to make runSelect testable

## Problem

`cmd/select.go`'s `runSelect()` is a 250-line function that orchestrates the entire `pop select` flow: config loading, project expansion, history sorting, session enrichment, picker loop, and 8-branch action dispatch. None of this orchestration is tested — the 838-line test file only covers extracted helpers (`buildSessionAwareItemsWith`, `sortByUnifiedRecency`, `expandProjectsWith`, `sanitizeSessionName`).

The real bugs hide in the loop:
- Does KillSession restore the cursor index on the next picker iteration?
- Does Reset re-sort base items after removing a history entry?
- Does `--tmux-cd` mode send cd instead of switching session?
- Does SwitchToPane resolve the correct history path for worktree sessions?

These questions are unanswerable without running a full Bubbletea TUI, because `ui.Run` is called directly and the side-effect functions (`openTmuxSession`, `killTmuxSession`, etc.) use the `defaultTmux` global.

The modules involved are shallow and tightly coupled:
- **cmd/select.go** — orchestration + project expansion helpers
- **cmd/session.go** — tmux operations behind `defaultTmux` global + `*With` variants
- **history**, **config**, **project**, **monitor** — each called via their `*With` pattern but wired together only inside `runSelect`

## Proposed Interface

A `SelectDeps` struct with function fields, and a top-level `RunSelect(d *SelectDeps) error` function — matching the existing `*With(d *Deps)` pattern used throughout the codebase.

```go
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

    // Side effects (take deps.Tmux as first arg to match existing *With signatures)
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

    // CLI flags
    TMuxCDPane string
    NoHistory  bool
}
```

Usage — the cobra handler becomes:

```go
func runSelect(cmd *cobra.Command, args []string) error {
    d := DefaultSelectDeps()
    d.TMuxCDPane = tmuxCDPane
    d.NoHistory = noHistory
    return RunSelect(d)
}
```

Test usage — override only what matters:

```go
func TestRunSelect_SelectRecordsHistory(t *testing.T) {
    hist := &history.History{}
    d := TestSelectDeps()
    d.LoadHistory = func() (*history.History, error) { return hist, nil }
    d.RunPicker = func(items []ui.Item, _ ...ui.PickerOption) (ui.Result, error) {
        return ui.Result{Action: ui.ActionSelect, Selected: &items[0]}, nil
    }

    RunSelect(d)

    if len(hist.Entries) != 1 { t.Fatal("expected history entry") }
}
```

## Dependency Strategy

**Category: Local-substitutable.** All external dependencies (tmux, filesystem, git) already have mock implementations in `internal/deps/`. The new `SelectDeps` struct uses function fields that wrap the existing `*With` functions — no new interfaces or adapters needed.

- `deps.Tmux` (already an interface) is reused directly
- `*project.Deps` and `*monitor.Deps` are reused directly
- Side-effect functions take `deps.Tmux` as first arg, matching existing signatures like `openTmuxSessionWith(tmux, item)`
- `RunPicker` wraps `ui.Run` — this is the critical seam that makes the loop testable

`DefaultSelectDeps()` wires everything to real implementations. `TestSelectDeps()` returns safe no-op defaults so tests only override 2-3 fields.

## Testing Strategy

### New boundary tests to write

Test the full orchestration loop through `RunSelect(d)` with `RunPicker` returning scripted `ui.Result` sequences:

- **ActionSelect happy path**: picker returns Select -> verify history recorded, tmux session opened
- **ActionSelect with `--no-history`**: verify history NOT recorded
- **ActionSelect with `--tmux-cd`**: verify cd sent to pane instead of session switch
- **ActionSelect standalone session**: verify `SwitchToTarget` called (not `OpenSession`)
- **ActionKillSession continues loop**: picker returns KillSession then Cancel -> verify picker called twice, session killed, cursor index restored
- **ActionReset re-sorts items**: picker returns Reset then Cancel -> verify history entry removed, base items re-sorted
- **ActionOpenWindow**: verify `OpenWindow` called, history recorded
- **ActionSwitchToPane**: verify `SwitchAndZoom` called, correct history path resolved
- **ActionRefresh**: verify picker called again with same cursor index
- **ActionUserDefinedCommand with Exit=false**: verify command executed, loop continues
- **ActionUserDefinedCommand with Exit=true**: verify command executed, loop exits
- **Config missing triggers first-run setup**: `LoadConfig` returns `os.ErrNotExist` -> verify `RunConfigure` called, then config reloaded

### Old tests to evaluate for deletion

Once boundary tests cover the orchestration, these helper-level tests become redundant:

- `TestBuildSessionAwareItems` and `TestBuildSessionAwareItems_AttentionIndicator` — icon assignment and standalone session detection are now tested implicitly through the full flow
- `TestSortByUnifiedRecency` — sorting is tested implicitly when verifying item order in picker calls
- `TestSortBaseItemsByHistory` — tested implicitly through the ActionReset boundary test

Keep: `TestSanitizeSessionName` (pure utility), `TestExpandProjectsWith_*` (parallel expansion has its own complexity worth unit-testing), `TestOpenTmuxWindowWith` (tmux argument construction).

### Test environment needs

No special infrastructure — only the existing mock types from `internal/deps/` (`MockTmux`, `MockGit`, `MockFileSystem`). The `TestSelectDeps()` helper provides safe defaults so each test only specifies the fields it cares about.

## Implementation Recommendations

- **The module should own**: the picker loop lifecycle, action dispatch, cursor restoration between iterations, session-aware item building, picker option assembly, first-run config bootstrap, and history mutation timing
- **The module should hide**: which actions continue vs exit the loop, how icons are computed, how standalone sessions are detected and merged, how picker options are conditionally assembled from config
- **The module should expose**: `RunSelect(d *SelectDeps) error` as the single entry point, `DefaultSelectDeps()` for production, `TestSelectDeps()` for tests
- **Migration path**: extract `RunSelect` as a new function, make the existing `runSelect` cobra handler call it with `DefaultSelectDeps()`, existing tests continue working unchanged. Add boundary tests incrementally — start with ActionSelect and ActionKillSession paths, which cover the most important behaviors (history recording and loop continuation)
