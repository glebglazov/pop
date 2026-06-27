package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// removeComponent removes a single Integration component for an agent. It
// dispatches to the right removal strategy per component kind (ADR 0010/0011):
//
//   - Status wiring strips pop's hook entries from the agent's settings while
//     preserving unrelated hooks (claude/codex/cursor), or deletes the
//     pop-owned status-sync extension file (pi/opencode).
//   - File-based skill components delete only pop-owned symlinks and their
//     render-tree entries; a same-named entry pop does not own is left
//     untouched and reported.
func removeComponent(d *integrateDeps, home string, id ComponentID, agent string) error {
	switch id {
	case ComponentStatusWiring:
		return removeStatusWiring(d, home, agent)
	default:
		return removeFileComponent(d, home, id, agent)
	}
}

// removeFileComponent removes a file-based component's pop-owned artifacts for
// an agent: the agent-location symlinks pop owns and the component's render
// tree under pop's data directory. Ownership is the same machine-checkable test
// the installer uses (ADR 0011) — a symlink resolving into pop's render tree,
// or a legacy `pop-` copy-mode entry. A same-named entry pop does not own is
// never deleted; it is left in place and reported.
func removeFileComponent(d *integrateDeps, home string, id ComponentID, agent string) error {
	agent = strings.ToLower(agent)

	tree, err := renderComponent(id, agent, d.resolveSkillPrefix())
	if err != nil {
		return err
	}

	dataDir, err := d.dataDir()
	if err != nil {
		return fmt.Errorf("failed to resolve pop data directory: %w", err)
	}
	integrationsRoot := filepath.Join(dataDir, "integrations")
	renderRoot := filepath.Join(integrationsRoot, agent, string(id))

	agentDir, err := agentSkillDir(home, agent)
	if err != nil {
		return err
	}

	// pop only ever links its artifacts at the canonical render-tree names
	// (`pop-<base>`), so those are the only agent-location entries removal
	// inspects. Each is removed only when pop owns it.
	topLevel := map[string]bool{}
	for rel := range tree {
		topLevel[firstSegment(rel)] = true
	}
	for name := range topLevel {
		dest := filepath.Join(agentDir, name)
		exists, owned, err := ownership(d, dest, integrationsRoot)
		if err != nil {
			return fmt.Errorf("failed to check ownership of %s: %w", dest, err)
		}
		if !exists {
			continue
		}
		if !owned {
			if d.stdout != nil {
				fmt.Fprintf(d.stdout, "  skipped %s: not owned by pop — left untouched\n", dest)
			}
			continue
		}
		if err := d.removeAll(dest); err != nil {
			return fmt.Errorf("failed to remove %s: %w", dest, err)
		}
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "  removed %s\n", dest)
		}
	}

	// The component's render tree is entirely pop-owned (it lives under pop's
	// data directory), so it is always safe to clean up.
	if err := d.removeAll(renderRoot); err != nil {
		return fmt.Errorf("failed to clean %s: %w", renderRoot, err)
	}
	return nil
}

// removeStatusWiring removes the status-wiring component for an agent by
// dispatching to that agent's hook-strip or extension-file removal. Hook
// stripping reuses the installer's idempotent pop-hook detection so unrelated
// hooks are preserved.
func removeStatusWiring(d *integrateDeps, home, agent string) error {
	switch strings.ToLower(agent) {
	case "claude":
		return stripJSONHooks(d, filepath.Join(home, ".claude", "settings.json"), removePopHooks)
	case "codex":
		return stripJSONHooks(d, filepath.Join(home, ".codex", "hooks.json"), removePopHooks)
	case "cursor":
		return stripJSONHooks(d, filepath.Join(home, ".cursor", "hooks.json"), removeCursorPopHooks)
	case "pi":
		return removeExtensionFile(d, filepath.Join(home, ".pi", "agent", "extensions", "pop-status-sync.ts"))
	case "opencode":
		return removeExtensionFile(d, filepath.Join(home, ".config", "opencode", "plugins", "pop-status-sync.ts"))
	default:
		return fmt.Errorf("unknown agent %q (expected: claude, codex, pi, opencode, cursor)", agent)
	}
}

// stripJSONHooks removes pop's hook entries from a JSON settings file, leaving
// every other key and every unrelated hook in place. The strip function is the
// agent-format-specific filter (removePopHooks for the nested claude/codex
// format, removeCursorPopHooks for the flat cursor format). A missing file or a
// file with no pop hooks is reported as nothing-to-remove and left unchanged.
func stripJSONHooks(d *integrateDeps, settingsPath string, strip func([]interface{}) []interface{}) error {
	data, err := d.readFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			if d.stdout != nil {
				fmt.Fprintf(d.stdout, "no pop hooks in %s — nothing to remove\n", settingsPath)
			}
			return nil
		}
		return fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}

	settings := make(map[string]interface{})
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse %s: %w", settingsPath, err)
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	removedAny := false
	for event, val := range hooks {
		eventHooks, ok := val.([]interface{})
		if !ok {
			continue
		}
		cleaned := strip(eventHooks)
		if len(cleaned) < len(eventHooks) {
			removedAny = true
		}
		if len(cleaned) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = cleaned
		}
	}

	if !removedAny {
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "no pop hooks in %s — nothing to remove\n", settingsPath)
		}
		return nil
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(settings); err != nil {
		return fmt.Errorf("failed to serialize %s: %w", settingsPath, err)
	}
	if err := d.writeFile(settingsPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", settingsPath, err)
	}
	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Removed pop hooks from %s\n", settingsPath)
	}
	return nil
}

// removeExtensionFile deletes a pop-owned status-sync extension file (pi,
// opencode). The file is wholly pop's — it carries no user content — so
// removal is unconditional when present. A missing file is reported as
// nothing-to-remove.
func removeExtensionFile(d *integrateDeps, path string) error {
	if _, err := d.lstatMode(path); err != nil {
		if os.IsNotExist(err) {
			if d.stdout != nil {
				fmt.Fprintf(d.stdout, "no pop extension at %s — nothing to remove\n", path)
			}
			return nil
		}
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if err := d.removeAll(path); err != nil {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	if d.stdout != nil {
		fmt.Fprintf(d.stdout, "Removed %s\n", path)
	}
	return nil
}

// componentInstalled reports whether a component currently has artifacts
// installed for an agent. It backs the default removal set: `pop integrate
// remove <agent>` with no component identifiers removes exactly the components
// reported installed here. An unsupported component is never installed.
func componentInstalled(d *integrateDeps, home string, id ComponentID, agent string) (bool, error) {
	comp, ok := lookupComponent(id)
	if !ok {
		return false, fmt.Errorf("unknown component %q", id)
	}
	if !comp.supported(agent) {
		return false, nil
	}
	switch id {
	case ComponentStatusWiring:
		return statusWiringInstalled(d, home, agent)
	default:
		return fileComponentInstalled(d, home, id, agent)
	}
}

// statusWiringInstalled reports whether pop's status wiring is present for an
// agent: a pop hook in the JSON settings (claude/codex/cursor) or the
// status-sync extension file (pi/opencode).
func statusWiringInstalled(d *integrateDeps, home, agent string) (bool, error) {
	switch strings.ToLower(agent) {
	case "claude":
		return jsonHasPopHooks(d, filepath.Join(home, ".claude", "settings.json"), isPopHook)
	case "codex":
		return jsonHasPopHooks(d, filepath.Join(home, ".codex", "hooks.json"), isPopHook)
	case "cursor":
		return jsonHasPopHooks(d, filepath.Join(home, ".cursor", "hooks.json"), isCursorPopHook)
	case "pi":
		return fileExists(d, filepath.Join(home, ".pi", "agent", "extensions", "pop-status-sync.ts"))
	case "opencode":
		return fileExists(d, filepath.Join(home, ".config", "opencode", "plugins", "pop-status-sync.ts"))
	default:
		return false, nil
	}
}

// jsonHasPopHooks reports whether any hook entry in the JSON settings file is a
// pop hook, per the given format-specific predicate.
func jsonHasPopHooks(d *integrateDeps, settingsPath string, isPop func(interface{}) bool) (bool, error) {
	data, err := d.readFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}
	settings := make(map[string]interface{})
	if err := json.Unmarshal(data, &settings); err != nil {
		return false, fmt.Errorf("failed to parse %s: %w", settingsPath, err)
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	for _, val := range hooks {
		eventHooks, ok := val.([]interface{})
		if !ok {
			continue
		}
		for _, e := range eventHooks {
			if isPop(e) {
				return true, nil
			}
		}
	}
	return false, nil
}

// fileExists reports whether an entry exists at path (via lstat, not following
// symlinks).
func fileExists(d *integrateDeps, path string) (bool, error) {
	if _, err := d.lstatMode(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// fileComponentInstalled reports whether any pop-owned artifact for a
// file-based component is present at the agent's location.
func fileComponentInstalled(d *integrateDeps, home string, id ComponentID, agent string) (bool, error) {
	agent = strings.ToLower(agent)
	tree, err := renderComponent(id, agent, d.resolveSkillPrefix())
	if err != nil {
		return false, err
	}
	dataDir, err := d.dataDir()
	if err != nil {
		return false, err
	}
	integrationsRoot := filepath.Join(dataDir, "integrations")
	agentDir, err := agentSkillDir(home, agent)
	if err != nil {
		return false, err
	}
	seen := map[string]bool{}
	for rel := range tree {
		name := firstSegment(rel)
		if seen[name] {
			continue
		}
		seen[name] = true
		dest := filepath.Join(agentDir, name)
		exists, owned, err := ownership(d, dest, integrationsRoot)
		if err != nil {
			return false, err
		}
		if exists && owned {
			return true, nil
		}
	}
	return false, nil
}

// runIntegrateRemoveComponents is the entry point for `pop integrate remove
// <agent> [component...]`. With no component identifiers it removes every
// component currently installed for the agent; with identifiers it removes
// exactly that set. Only pop-owned artifacts are ever deleted, so removal can
// never destroy the user's own files (ADR 0011).
func runIntegrateRemoveComponents(d *integrateDeps, agent string, ids []ComponentID) error {
	agent = strings.ToLower(agent)

	core, ok := lookupComponent(ComponentStatusWiring)
	if !ok {
		return fmt.Errorf("status-wiring component missing from catalog")
	}
	// The status-wiring support set is exactly the known agents, so this
	// doubles as the unknown-agent guard.
	if !core.supported(agent) {
		return fmt.Errorf("unknown agent %q (expected: claude, codex, pi, opencode, cursor)", agent)
	}

	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	if len(ids) == 0 {
		// Default set: every component currently installed for this agent, in
		// catalog order.
		for _, c := range integrationCatalog {
			inst, err := componentInstalled(d, home, c.id, agent)
			if err != nil {
				return err
			}
			if inst {
				ids = append(ids, c.id)
			}
		}
		if len(ids) == 0 {
			if d.stdout != nil {
				fmt.Fprintf(d.stdout, "no pop components installed for %s — nothing to remove\n", agent)
			}
			return nil
		}
	} else {
		// Explicit set: validate each identifier is a known component the agent
		// can host before touching anything.
		for _, id := range ids {
			comp, ok := lookupComponent(id)
			if !ok {
				return fmt.Errorf("unknown component %q", id)
			}
			if !comp.supported(agent) {
				return fmt.Errorf("component %q is not supported for agent %q", id, agent)
			}
		}
	}

	for _, id := range ids {
		if err := removeComponent(d, home, id, agent); err != nil {
			return err
		}
	}
	// Record the removed default-on components as persisted opt-outs (negative
	// consent, ADR 0064 slice 08) so refresh does not re-add them. A bare
	// `pop integrate <agent>` later clears these.
	return persistRemoveOptOut(d, agent, ids)
}

// persistRemoveOptOut merges the default-on components just removed into the
// agent's persisted opt-out set, so a later refresh never re-adds them. Removal
// is targeted, so this merges (unlike the install path, which replaces). The
// core status wiring and unknown ids are not opt-outs and are ignored. New path
// logs per slice 01.
func persistRemoveOptOut(d *integrateDeps, agent string, removed []ComponentID) error {
	if d.saveOptOut == nil || d.loadOptOut == nil {
		return nil
	}
	agent = strings.ToLower(agent)
	set := d.loadOptOut(agent)
	if set == nil {
		set = map[ComponentID]bool{}
	}
	changed := false
	for _, id := range removed {
		comp, ok := lookupComponent(id)
		if !ok || !comp.defaultOn || set[id] {
			continue
		}
		set[id] = true
		changed = true
	}
	if !changed {
		return nil
	}
	if d.logf != nil {
		d.logf("persistRemoveOptOut: %s opt-out set -> %v", agent, sortedComponentIDs(set))
	}
	return d.saveOptOut(agent, set)
}
