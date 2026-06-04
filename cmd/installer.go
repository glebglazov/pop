package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// installFileComponent installs a file-based Integration component (a skill)
// for an agent following ADR 0011: the rendered tree is written under pop's
// data directory and the agent's location receives a symlink into that tree.
//
// Ownership is machine-checkable — an agent-location entry pop owns is a
// symlink resolving into pop's render tree. Re-running is idempotent (the
// symlink is rewritten), and an existing copy-mode artifact owned by a prior
// pop install (a real entry under the `pop-` name prefix) is migrated to a
// symlink transparently by the same wipe-and-rewrite path.
func installFileComponent(d *integrateDeps, home string, id ComponentID, agent string) error {
	agent = strings.ToLower(agent)

	tree, err := renderComponent(id, agent)
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

	// Remove any legacy copy-mode artifacts this component supersedes (e.g. the
	// claude command-style pane install) so the switch to skills leaves nothing
	// behind.
	for _, p := range legacyArtifacts(home, agent, id) {
		if err := d.removeAll(p); err != nil {
			return fmt.Errorf("failed to remove legacy artifact %s: %w", p, err)
		}
	}

	// Render the tree fresh under the data dir. Clear the prior render root
	// first so a renamed or removed file does not linger.
	if err := d.removeAll(renderRoot); err != nil {
		return fmt.Errorf("failed to clean %s: %w", renderRoot, err)
	}
	topLevel := map[string]bool{}
	for rel, data := range tree {
		full := filepath.Join(renderRoot, rel)
		if err := d.mkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("failed to create %s: %w", filepath.Dir(full), err)
		}
		if err := d.writeFile(full, data, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", full, err)
		}
		topLevel[firstSegment(rel)] = true
	}

	if err := d.mkdirAll(agentDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", agentDir, err)
	}

	for name := range topLevel {
		dest := filepath.Join(agentDir, name)
		target := filepath.Join(renderRoot, name)

		exists, owned, err := ownership(d, dest, integrationsRoot)
		if err != nil {
			return fmt.Errorf("failed to check ownership of %s: %w", dest, err)
		}
		// A same-named entry pop does not own is never overwritten — full
		// Integration conflict reporting lands in a later slice; here we simply
		// refuse to clobber a user's file.
		if exists && !owned {
			if d.stdout != nil {
				fmt.Fprintf(d.stdout, "  skipped %s (not owned by pop)\n", dest)
			}
			continue
		}
		// Remove the existing entry (a stale symlink, or a pop-owned copy-mode
		// directory being migrated) and link to the render tree.
		if err := d.removeAll(dest); err != nil {
			return fmt.Errorf("failed to remove %s: %w", dest, err)
		}
		if err := d.symlink(target, dest); err != nil {
			return fmt.Errorf("failed to symlink %s -> %s: %w", dest, target, err)
		}
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "  linked %s -> %s\n", dest, target)
		}
	}

	return nil
}

// ownership reports whether an entry exists at dest and whether pop owns it.
//
// Ownership is decided in two ways, strongest first:
//   - A symlink whose target resolves under pop's integrations root — the
//     canonical ADR 0011 marker.
//   - A real entry whose name carries the legacy `pop-` prefix — a copy-mode
//     install from before symlinks, eligible for migration.
//
// Anything else that exists is not owned by pop.
func ownership(d *integrateDeps, dest, integrationsRoot string) (exists, owned bool, err error) {
	mode, err := d.lstatMode(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}
	if mode&os.ModeSymlink != 0 {
		target, err := d.readlink(dest)
		if err != nil {
			return true, false, err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(dest), target)
		}
		target = filepath.Clean(target)
		root := filepath.Clean(integrationsRoot)
		inTree := target == root || strings.HasPrefix(target, root+string(filepath.Separator))
		return true, inTree, nil
	}
	return true, strings.HasPrefix(filepath.Base(dest), "pop-"), nil
}

// agentSkillDir returns the directory at the agent's location where pop's skill
// entries are symlinked. claude switched from slash commands to skills, so its
// location is the skills directory (not commands/pop). opencode hosts skills as
// flat single files under its agent directory, so the symlinked entry there is
// a `pop-<name>.md` file rather than a skill directory.
func agentSkillDir(home, agent string) (string, error) {
	switch strings.ToLower(agent) {
	case "claude":
		return filepath.Join(home, ".claude", "skills"), nil
	case "pi":
		return filepath.Join(home, ".pi", "agent", "skills"), nil
	case "cursor":
		return filepath.Join(home, ".cursor", "skills"), nil
	case "opencode":
		return filepath.Join(home, ".config", "opencode", "agent"), nil
	default:
		return "", fmt.Errorf("agent %q has no skill location", agent)
	}
}

// legacyArtifacts lists copy-mode paths a component's new install must clean up.
// For claude's pane skill this is the old slash-command file under
// ~/.claude/commands/pop/, removed when the skill takes over.
func legacyArtifacts(home, agent string, id ComponentID) []string {
	if strings.ToLower(agent) == "claude" && id == ComponentPaneSkill {
		return []string{filepath.Join(home, ".claude", "commands", "pop", "pane.md")}
	}
	return nil
}

// firstSegment returns the first path component of a relative path.
func firstSegment(rel string) string {
	rel = filepath.ToSlash(rel)
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return rel
}
