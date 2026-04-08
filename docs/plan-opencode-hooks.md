# Development Plan: Opencode Integration for Pop

> **Note:** This is a historical planning doc. The status name `needs_attention`
> mentioned throughout was later renamed to `unread`. The transitions and
> semantics are otherwise unchanged.

## Overview

Add `pop integrate opencode` command to install pop lifecycle hooks and skills for the opencode AI coding agent, similar to existing integrations for Claude and Pi.

## Background

Pop currently supports integration with two AI coding agents:
- **Claude**: Uses `~/.claude/settings.json` hooks and `~/.claude/commands/pop/` slash commands
- **Pi**: Uses `~/.pi/agent/extensions/pop-pane-status.ts` TypeScript extension and `~/.pi/agent/skills/pop-*/SKILL.md` skills

Opencode is an open-source AI coding agent with a plugin system that supports lifecycle hooks via JavaScript/TypeScript plugins.

## Goals

1. Extend `pop integrate` to support `opencode` as a third agent option
2. Install pop pane-status plugin for opencode lifecycle hooks
3. Install pop skills as opencode agents/commands
4. Maintain consistency with existing Claude and Pi integrations
5. Ensure idempotent installation (re-running replaces old pop files)

## Opencode Plugin Architecture

Based on research of opencode's plugin system:

**Plugin Location**: `~/.config/opencode/plugins/*.ts` (global) or `.opencode/plugins/*.ts` (project-local)

**Plugin Structure**:
```typescript
export const PluginName = async ({ project, client, $, directory, worktree }) => {
  return {
    // Hook implementations
    "tool.execute.before": async (input, output) => { },
    "session.idle": async (event) => { },
  }
}
```

**Relevant Lifecycle Hooks**:
| Opencode Event | Pop Status | Description |
|----------------|------------|-------------|
| `tui.prompt.append` | `working` | User submitted a prompt |
| `tool.execute.before` | `working` | Tool is about to execute |
| `session.idle` | `needs_attention` | Agent became idle |
| `session.created` | `needs_attention` | Session started (reset state) |

## Implementation Plan

### Phase 1: Add Opencode Extension File

Create `cmd/extensions/opencode/pop-pane-status.ts`:

```typescript
/**
 * pop-pane-status
 *
 * Opencode plugin that keeps the surrounding pop tmux pane's status in sync
 * with the agent's lifecycle.
 */

export const PopPaneStatus = async ({ $ }) => {
  const setStatus = (status: "working" | "needs_attention") => {
    // Fire-and-forget; swallow errors
    $`pop pane set-status ${status}`.catch(() => {});
  };

  return {
    // User submitted input → working
    "tui.prompt.append": async () => {
      setStatus("working");
    },

    // Tool execution starting → working
    "tool.execute.before": async () => {
      setStatus("working");
    },

    // Agent became idle → needs_attention
    "session.idle": async () => {
      setStatus("needs_attention");
    },

    // Session started → reset to idle
    "session.created": async () => {
      setStatus("needs_attention");
    },
  };
};
```

### Phase 2: Update Integrate Command

Modify `cmd/integrate.go`:

1. Add `opencode` to `ValidArgs` in Cobra command definition
2. Add `//go:embed extensions/opencode/pop-pane-status.ts` directive
3. Implement `integrateOpencode(d *integrateDeps)` function:
   - Install plugin to `~/.config/opencode/plugins/pop-pane-status.ts`
   - Install skills to `~/.config/opencode/agent/pop-<name>.md`
4. Update `runIntegrateWith` switch statement to handle `"opencode"`

### Phase 3: Skill Adaptation for Opencode

Opencode uses markdown files for agents/commands in `~/.config/opencode/agent/` and `~/.config/opencode/command/`.

Skills should be adapted:
- Claude uses `~/.claude/commands/pop/<name>.md` (plain markdown)
- Pi uses `~/.pi/agent/skills/pop-<name>/SKILL.md` (with frontmatter `name:` field)
- Opencode uses `~/.config/opencode/agent/<name>.md` or `~/.config/opencode/command/<name>.md`

The existing `pane.md` skill can be reused with minimal changes since opencode also supports markdown-based agent definitions.

### Phase 4: Testing

Update `cmd/integrate_test.go`:

1. Add test cases for `integrateOpencode`:
   - Test plugin installation to `~/.config/opencode/plugins/`
   - Test skill installation to `~/.config/opencode/agent/`
   - Test idempotency (re-running replaces old files)
   - Test error handling for filesystem operations

2. Add tests for new fake filesystem operations if needed

### Phase 5: Documentation

Update documentation:
1. Update `cmd/skills/pop/pane.md` to mention `pop integrate opencode`
2. Update command help text in `integrate.go`
3. Update README.md with opencode integration instructions

## File Changes

### New Files
```
cmd/extensions/opencode/pop-pane-status.ts    # Opencode plugin
docs/plan-opencode-hooks.md                    # This plan
```

### Modified Files
```
cmd/integrate.go          # Add opencode integration logic
cmd/integrate_test.go     # Add tests for opencode integration
cmd/skills/pop/pane.md    # Update to mention opencode
README.md                 # Document opencode integration
```

## Testing Strategy

### Unit Tests

Following the existing pattern in `integrate_test.go`:

```go
func TestIntegrateOpencode(t *testing.T) {
    tests := []struct {
        name        string
        existingFiles map[string][]byte
        wantFiles   map[string]string  // path -> content substring
        wantErr     bool
    }{
        {
            name: "fresh install installs plugin and skills",
            wantFiles: map[string]string{
                "/home/user/.config/opencode/plugins/pop-pane-status.ts": "export const PopPaneStatus",
                "/home/user/.config/opencode/agent/pop-pane.md": "pop pane",
            },
        },
        {
            name: "re-install is idempotent",
            existingFiles: map[string][]byte{
                "/home/user/.config/opencode/plugins/pop-pane-status.ts": []byte("old content"),
            },
            wantFiles: map[string]string{
                "/home/user/.config/opencode/plugins/pop-pane-status.ts": "session.idle",
            },
        },
    }
}
```

### Integration Tests

Manual verification steps:
1. Run `pop integrate opencode`
2. Verify plugin file exists at `~/.config/opencode/plugins/pop-pane-status.ts`
3. Verify skill files exist at `~/.config/opencode/agent/pop-*.md`
4. Start opencode in a tmux pane
5. Verify `pop pane status` shows `working` when submitting a prompt
6. Verify `pop pane status` shows `needs_attention` when agent becomes idle

## Acceptance Criteria

- [ ] `pop integrate opencode` installs plugin to `~/.config/opencode/plugins/`
- [ ] `pop integrate opencode` installs skills to `~/.config/opencode/agent/`
- [ ] Running command twice is idempotent (replaces old pop files)
- [ ] Plugin correctly sets `working` status on user input and tool execution
- [ ] Plugin correctly sets `needs_attention` status when agent becomes idle
- [ ] Tests cover all new functionality with >80% coverage
- [ ] Documentation updated to mention opencode support

## Future Enhancements (Out of Scope)

- Custom opencode commands in `~/.config/opencode/command/` (vs agents)
- Project-local plugin installation (`.opencode/plugins/`)
- Additional lifecycle hooks as opencode adds them
- Integration with opencode's custom tools API for `pop pane` commands

## References

- Opencode Plugin Docs: https://opencode.ai/docs/plugins
- Existing Pi Extension: `cmd/extensions/pi/pop-pane-status.ts`
- Existing Claude Hooks: `cmd/integrate.go` (popHooks variable)
- Existing Tests: `cmd/integrate_test.go`
