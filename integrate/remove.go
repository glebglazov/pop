package integrate

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
func removeComponent(d *Deps, home string, id ComponentID, agent string) error {
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
// or a copy-mode entry with the `pop-owned: true` marker. A same-named entry
// pop does not own is never deleted; it is left in place and reported.
func removeFileComponent(d *Deps, home string, id ComponentID, agent string) error {
	agent = strings.ToLower(agent)

	dataDir, err := d.dataDir()
	if err != nil {
		return fmt.Errorf("failed to resolve pop data directory: %w", err)
	}
	integrationsRoot := filepath.Join(dataDir, "integrations")
	renderRoot := filepath.Join(integrationsRoot, agent, string(id))

	agentDir, err := agentSkillDir(d, home, agent, id)
	if err != nil {
		return err
	}

	installedNames, err := fileComponentInstalledNames(d, home, id, agent)
	if err != nil {
		return err
	}
	for name := range installedNames {
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

// removeStatusWiring removes the status-wiring component for an agent via
// that agent's integration profile (JSON-hook strip or extension-file delete).
func removeStatusWiring(d *Deps, home, agent string) error {
	p, ok := LookupProfile(agent)
	if !ok {
		return unknownAgentError(agent)
	}
	return p.RemoveStatusWiring(d, home)
}

// stripJSONHooks removes pop's hook entries from a JSON settings file, leaving
// every other key and every unrelated hook in place. The dialect's IsPop
// predicate is the format-specific filter (nested vs flat). A missing file or a
// file with no pop hooks is reported as nothing-to-remove and left unchanged.
func stripJSONHooks(d *Deps, settingsPath string, dialect HookDialect) error {
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
		cleaned := removeHooksMatching(eventHooks, dialect.IsPop)
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
func removeExtensionFile(d *Deps, path string) error {
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
func componentInstalled(d *Deps, home string, id ComponentID, agent string) (bool, error) {
	comp, ok := LookupComponent(id)
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
// agent, via that agent's integration profile.
func statusWiringInstalled(d *Deps, home, agent string) (bool, error) {
	p, ok := LookupProfile(agent)
	if !ok {
		return false, nil
	}
	return p.DetectStatusWiring(d, home)
}

// jsonHasPopHooks reports whether any hook entry in the JSON settings file is a
// pop hook, per the given format-specific predicate.
func jsonHasPopHooks(d *Deps, settingsPath string, isPop func(interface{}) bool) (bool, error) {
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
func fileExists(d *Deps, path string) (bool, error) {
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
func fileComponentInstalled(d *Deps, home string, id ComponentID, agent string) (bool, error) {
	names, err := fileComponentInstalledNames(d, home, id, agent)
	if err != nil {
		return false, err
	}
	return len(names) > 0, nil
}

// Remove is the entry point for `pop integrate remove <agent> [component...]`.
// With no RemoveComponents it removes every component currently installed for
// the agent; with identifiers it removes exactly that set. Only pop-owned
// artifacts are ever deleted, so removal can never destroy the user's own
// files (ADR 0011). Operational messages are written to d.stdout; the Report
// carries run-result fields for callers that need them.
func Remove(d *Deps, req Request) (Report, error) {
	agent := strings.ToLower(req.Agent)
	ids := append([]ComponentID(nil), req.RemoveComponents...)

	core, ok := LookupComponent(ComponentStatusWiring)
	if !ok {
		return Report{}, fmt.Errorf("status-wiring component missing from catalog")
	}
	// The status-wiring support set is exactly the known agents, so this
	// doubles as the unknown-agent guard.
	if !core.supported(agent) {
		return Report{}, unknownAgentError(agent)
	}

	home, err := d.userHomeDir()
	if err != nil {
		return Report{}, fmt.Errorf("failed to get home directory: %w", err)
	}

	if len(ids) == 0 {
		// Default set: every component currently installed for this agent, in
		// catalog order.
		for _, c := range catalog {
			inst, err := componentInstalled(d, home, c.id, agent)
			if err != nil {
				return Report{}, err
			}
			if inst {
				ids = append(ids, c.id)
			}
		}
		if len(ids) == 0 {
			if d.stdout != nil {
				fmt.Fprintf(d.stdout, "no pop components installed for %s — nothing to remove\n", agent)
			}
			return Report{}, nil
		}
	} else {
		// Explicit set: validate each identifier is a known component the agent
		// can host before touching anything.
		for _, id := range ids {
			comp, ok := LookupComponent(id)
			if !ok {
				return Report{}, fmt.Errorf("unknown component %q", id)
			}
			if !comp.supported(agent) {
				return Report{}, fmt.Errorf("component %q is not supported for agent %q", id, agent)
			}
		}
	}

	for _, id := range ids {
		if err := removeComponent(d, home, id, agent); err != nil {
			return Report{}, err
		}
	}
	return Report{}, nil
}
