# Implementation Plan: Auto-Update Integrations

## Overview

When a user rebuilds the pop binary, installed agent integrations (claude, pi, opencode) should automatically update to match the new binary's embedded skills and hooks — without the user re-running `pop integrate`. The user must still run `pop integrate <agent>` the first time; after that, updates are automatic.

## Design Decisions (from grilling session)

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Trigger | `ensureSystemState()` from select/worktree/dashboard | These are TUI entry points, not high-frequency background commands |
| Integration check | Synchronous (not goroutine) | Fast (one file read in common case), guarantees warnings visible on first render |
| Daemon restart | Goroutine (unchanged) | Slow (process management), non-critical for picker |
| Staleness detection | Build revision in `state.json` | More reliable than mtime; uses existing `vcs.revision` from `runtime/debug` |
| "Is installed?" detection | Filesystem — at least one pop artifact exists for the agent | No separate registry to diverge from reality |
| "Needs update?" detection | Dry-run: compare what would be written against what exists | Avoids unnecessary writes and formatting churn (especially `settings.json`) |
| Dry-run mechanism | `DryRun bool` + `changed bool` fields on `integrateDeps` | Write functions become comparators; install logic unchanged |
| Error handling | Surface failures as picker warnings; don't stamp `state.json` | Retry on next launch; user sees the error |
| Success output | Silent — no message on successful auto-update | Transparency; only surface failures |
| `pop integrate` interaction | Manual integrate does NOT touch `state.json` | Keeps concerns separate; one redundant dry-run on next launch is cheap |
| Agent list | Hardcoded `["claude", "pi", "opencode"]` | 3 agents, changes rarely; registry is overkill |

## Implementation Steps

### Step 1: Add `DryRun` support to `integrateDeps`

**File**: `cmd/integrate.go`

Add two fields to `integrateDeps`:

```go
type integrateDeps struct {
    userHomeDir func() (string, error)
    readFile    func(string) ([]byte, error)
    writeFile   func(string, []byte, os.FileMode) error
    mkdirAll    func(string, os.FileMode) error
    removeAll   func(string) error
    stdout      io.Writer

    // Dry-run support: when true, write operations compare content
    // instead of writing. `changed` is set to true if any write
    // would produce a different result. `installed` is set to true
    // if any target file already exists on disk.
    DryRun    bool
    changed   bool
    installed bool
}
```

### Step 2: Create dry-run aware deps constructor

**File**: `cmd/integrate.go`

Add a function that wraps a real `integrateDeps` with dry-run behavior:

```go
func dryRunIntegrateDeps() *integrateDeps {
    d := &integrateDeps{
        userHomeDir: os.UserHomeDir,
        readFile:    os.ReadFile,
        DryRun:      true,
    }
    d.writeFile = func(path string, data []byte, perm os.FileMode) error {
        existing, err := d.readFile(path)
        if err == nil {
            d.installed = true
            if !bytes.Equal(existing, data) {
                d.changed = true
            }
        }
        // File doesn't exist — this would be a create, not an update.
        // Don't set installed (this artifact doesn't exist yet).
        // Don't set changed (creating new files in an uninstalled agent
        // is not an "update" — auto-updater should skip).
        return nil
    }
    d.mkdirAll = func(path string, perm os.FileMode) error {
        return nil // no-op in dry run
    }
    d.removeAll = func(path string) error {
        // Check if target exists — if so, that's an installed artifact
        if _, err := os.Stat(path); err == nil {
            d.installed = true
        }
        return nil // no-op in dry run
    }
    d.stdout = nil // suppress output
    return d
}
```

**Note on Claude hooks detection**: The `installClaudeHooks` function reads `settings.json`, strips old pop hooks, then writes new ones. In dry-run mode:
- `d.readFile` works normally (reads the real file)
- The function builds the new content in a buffer
- `d.writeFile` compares the buffer to the existing file content
- If pop hooks already exist in `settings.json`, `removePopHooks` will find them → `d.installed = true` should also be set when existing pop hooks are detected

This requires a small addition: set `d.installed = true` inside `installClaudeHooks` when `removePopHooks` finds existing hooks. This is agent-specific logic that belongs in the install function, not the deps layer. Add after line 185:

```go
// In the loop at lines 180-191 of installClaudeHooks:
cleaned := removePopHooks(eventHooks)
if len(cleaned) < len(eventHooks) {
    // Pop hooks were found and removed — integration is installed
    if d.DryRun {
        d.installed = true
    }
}
```

### Step 3: Extract build revision helper

**File**: `cmd/root.go` (or a new internal helper)

Extract the `vcs.revision` reading into a reusable function. Currently `buildVersion()` reads it but formats it into a human-readable string. We need the raw revision.

```go
// buildRevision returns the VCS revision embedded by `go build`,
// or "dev" if not available (e.g., `go run`).
func buildRevision() string {
    info, ok := runtimedebug.ReadBuildInfo()
    if !ok {
        return "dev"
    }
    for _, s := range info.Settings {
        if s.Key == "vcs.revision" {
            return s.Value
        }
    }
    return "dev"
}
```

Refactor `buildVersion()` to call `buildRevision()` internally to avoid duplication.

### Step 4: Implement `state.json` read/write

**File**: `cmd/integrate.go` (or a small new section in an existing file)

```go
type appState struct {
    BuildRevision string `json:"build_revision"`
}

func loadAppState() (*appState, error) {
    // path: ~/.local/share/pop/state.json (respects XDG_DATA_HOME)
    path := filepath.Join(config.DefaultDataDir(), "state.json")
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return &appState{}, nil
        }
        return nil, err
    }
    var s appState
    if err := json.Unmarshal(data, &s); err != nil {
        return &appState{}, nil // treat corrupt file as empty
    }
    return &s, nil
}

func saveAppState(s *appState) error {
    path := filepath.Join(config.DefaultDataDir(), "state.json")
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
        return err
    }
    data, err := json.Marshal(s)
    if err != nil {
        return err
    }
    return os.WriteFile(path, data, 0o644)
}
```

**Note**: Check if `config.DefaultDataDir()` already exists or if a helper is needed. The history package uses `history.DefaultHistoryPath()` which builds the XDG path — follow the same pattern.

### Step 5: Implement `ensureIntegrations`

**File**: `cmd/integrate.go`

```go
// ensureIntegrations checks if installed agent integrations are stale
// and updates them if the binary has been rebuilt. Returns warnings
// for any agents that failed to update.
func ensureIntegrations() []string {
    rev := buildRevision()
    if rev == "dev" {
        return nil // can't track dev builds
    }

    state, err := loadAppState()
    if err != nil {
        debug.Error("ensureIntegrations: load state: %v", err)
        return nil
    }

    if state.BuildRevision == rev {
        return nil // already checked this binary
    }

    agents := []string{"claude", "pi", "opencode"}
    var warnings []string
    anyFailed := false

    for _, agent := range agents {
        // Dry-run to check if installed and needs update
        dryDeps := dryRunIntegrateDeps()
        if err := runIntegrateWith(dryDeps, agent); err != nil {
            debug.Error("ensureIntegrations: dry-run %s: %v", agent, err)
            // Don't warn on dry-run errors for uninstalled agents
            if dryDeps.installed {
                warnings = append(warnings, fmt.Sprintf("failed to check %s integration: %v", agent, err))
                anyFailed = true
            }
            continue
        }

        if !dryDeps.installed || !dryDeps.changed {
            continue
        }

        // Real run — agent is installed and stale
        realDeps := defaultIntegrateDeps()
        realDeps.stdout = nil // suppress output during auto-update
        if err := runIntegrateWith(realDeps, agent); err != nil {
            debug.Error("ensureIntegrations: update %s: %v", agent, err)
            warnings = append(warnings, fmt.Sprintf("failed to update %s integration (see pop.log)", agent))
            anyFailed = true
            continue
        }

        debug.Log("ensureIntegrations: updated %s integration", agent)
    }

    if !anyFailed {
        state.BuildRevision = rev
        if err := saveAppState(state); err != nil {
            debug.Error("ensureIntegrations: save state: %v", err)
        }
    }

    return warnings
}
```

### Step 6: Create `ensureSystemState` wrapper

**File**: `cmd/session.go` (alongside existing `ensureMonitorDaemon`)

```go
// ensureSystemState runs startup checks: updates stale integrations
// (synchronously) and ensures the monitor daemon is running (in background).
// Returns warnings to surface in the picker UI.
func ensureSystemState() []string {
    warnings := ensureIntegrations()
    go ensureMonitorDaemon()
    return warnings
}
```

### Step 7: Update call sites

**Files**: `cmd/select.go`, `cmd/worktree.go`, `cmd/dashboard.go`

Replace:
```go
go ensureMonitorDaemon()
```

With:
```go
systemWarnings := ensureSystemState()
```

Then incorporate `systemWarnings` into the picker's warning list. In `select.go`, this means appending to the existing `warnings` slice alongside expansion errors. In `worktree.go` and `dashboard.go`, follow the same pattern those commands use for surfacing warnings.

### Step 8: Tests

**File**: `cmd/integrate_test.go`

#### Test: dry-run detects no installation
```
Given: no pop artifacts exist for any agent
When: dry-run integrate runs
Then: installed=false, changed=false
```

#### Test: dry-run detects installed and current
```
Given: claude skills and hooks match embedded content exactly
When: dry-run integrate runs for claude
Then: installed=true, changed=false
```

#### Test: dry-run detects installed and stale
```
Given: claude skills exist but content differs from embedded
When: dry-run integrate runs for claude
Then: installed=true, changed=true
```

#### Test: ensureIntegrations skips when revision matches
```
Given: state.json contains current build revision
When: ensureIntegrations runs
Then: returns nil, no integrate calls made
```

#### Test: ensureIntegrations updates stale agent
```
Given: state.json has old revision, claude is installed with old skills
When: ensureIntegrations runs
Then: claude files updated, state.json stamped with current revision
```

#### Test: ensureIntegrations retries on failure
```
Given: state.json has old revision, claude update fails
When: ensureIntegrations runs
Then: warning returned, state.json NOT updated (retry next launch)
```

#### Test: partial failure doesn't stamp
```
Given: claude update succeeds, pi update fails
When: ensureIntegrations runs
Then: warning for pi, state.json NOT updated
```

#### Test: settings.json not rewritten when hooks unchanged
```
Given: claude settings.json has current pop hooks
When: dry-run integrate runs
Then: installed=true, changed=false (no rewrite)
```

## File Change Summary

| File | Changes |
|------|---------|
| `cmd/integrate.go` | Add `DryRun`/`changed`/`installed` fields to `integrateDeps`; add `dryRunIntegrateDeps()`; add `ensureIntegrations()`; add `appState` + load/save; small tweak in `installClaudeHooks` to detect existing hooks |
| `cmd/root.go` | Extract `buildRevision()` from `buildVersion()` |
| `cmd/session.go` | Add `ensureSystemState()` wrapper |
| `cmd/select.go` | Replace `go ensureMonitorDaemon()` with `ensureSystemState()`, wire warnings |
| `cmd/worktree.go` | Replace `go ensureMonitorDaemon()` with `ensureSystemState()`, wire warnings |
| `cmd/dashboard.go` | Replace `go ensureMonitorDaemon()` with `ensureSystemState()`, wire warnings |
| `cmd/integrate_test.go` | Add dry-run and ensureIntegrations tests |

## Risks & Mitigations

1. **Claude `settings.json` formatting churn**: Mitigated by dry-run content comparison — file is only rewritten when hooks actually differ.
2. **`state.json` corruption**: Treated as empty (re-checks everything, self-healing).
3. **`dev` builds**: Skipped entirely — no revision to track. Users building with `go run` won't get auto-update (acceptable).
4. **Race between goroutine and picker**: Not a risk — `ensureIntegrations` is synchronous.
5. **First launch after feature ships**: Every existing user's `state.json` won't exist, so one dry-run per agent on first launch. If integrations are current, stamps immediately. If they're not installed, dry-runs find `installed=false` and skip.
