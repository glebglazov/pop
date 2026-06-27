package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
)

// installFileComponent installs a file-based Integration component (a skill)
// for an agent following ADR 0011: the rendered tree is written under pop's
// data directory and the agent's location receives a symlink into that tree.
//
// Ownership is machine-checkable — an agent-location entry pop owns is a
// symlink resolving into pop's render tree, or a real entry carrying the
// `pop-owned: true` frontmatter marker. Re-running is idempotent (the symlink
// is rewritten), an existing marker-owned copy-mode artifact is migrated to a
// symlink by the same wipe-and-rewrite path, and entries left under a previous
// skills_prefix are pruned (ADR 0063).
func installFileComponent(d *integrateDeps, home string, id ComponentID, agent string) error {
	agent = strings.ToLower(agent)
	prefix := d.resolveSkillsPrefix()

	tree, err := renderComponent(id, agent, prefix)
	if err != nil {
		return err
	}

	dataDir, err := d.dataDir()
	if err != nil {
		return fmt.Errorf("failed to resolve pop data directory: %w", err)
	}
	integrationsRoot := filepath.Join(dataDir, "integrations")
	renderRoot := filepath.Join(integrationsRoot, agent, string(id))

	agentDir, err := agentSkillDir(home, agent, id)
	if err != nil {
		return err
	}

	if d.logf != nil {
		d.logf("installFileComponent: agent=%s id=%s prefix=%q agentDir=%s renderRoot=%s", agent, id, prefix, agentDir, renderRoot)
	}

	d.prunedStale = nil

	for _, p := range legacyArtifacts(home, agent, id) {
		if d.logf != nil {
			d.logf("installFileComponent: removing legacy artifact %s", p)
		}
		if err := d.removeAll(p); err != nil {
			return fmt.Errorf("failed to remove legacy artifact %s: %w", p, err)
		}
	}

	for _, legacyRoot := range legacyComponentIDs(id) {
		legacyRender := filepath.Join(integrationsRoot, agent, legacyRoot)
		if d.logf != nil {
			d.logf("installFileComponent: clearing legacy render root %s", legacyRender)
		}
		if err := d.removeAll(legacyRender); err != nil {
			return fmt.Errorf("failed to clean legacy %s: %w", legacyRender, err)
		}
	}

	if d.logf != nil {
		d.logf("installFileComponent: clearing render root %s", renderRoot)
	}
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
		if d.logf != nil {
			d.logf("installFileComponent: wrote render file %s (%d bytes)", full, len(data))
		}
		topLevel[firstSegment(rel)] = true
	}

	if err := d.mkdirAll(agentDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", agentDir, err)
	}

	for name := range topLevel {
		dest := filepath.Join(agentDir, name)
		target := filepath.Join(renderRoot, name)

		conflictPath, conflict, err := skillConflict(d, agentDir, name, integrationsRoot, prefix)
		if err != nil {
			return fmt.Errorf("failed to check ownership of %s: %w", dest, err)
		}
		if conflict {
			if d.overwriteConflicts {
				overwrite, err := resolveConflictOverwrite(d, conflictPath)
				if err != nil {
					return fmt.Errorf("failed to resolve conflict at %s: %w", conflictPath, err)
				}
				if !overwrite {
					if d.logf != nil {
						d.logf("installFileComponent: skipping %s — conflict at %s (not owned by pop)", name, conflictPath)
					}
					continue
				}
				if err := d.removeAll(conflictPath); err != nil {
					return fmt.Errorf("failed to remove unowned entry %s: %w", conflictPath, err)
				}
				d.overwrotePaths = append(d.overwrotePaths, conflictPath)
				reportOverwriteDestroyed(d.stdout, conflictPath)
			} else {
				if d.logf != nil {
					d.logf("installFileComponent: skipping %s — conflict at %s (not owned by pop)", name, conflictPath)
				}
				if d.stdout != nil && d.agentName != "" {
					fmt.Fprintf(d.stdout, "  skipped %s: %s exists and is not owned by pop — run 'pop integrate %s --overwrite-conflicts' to replace it\n", name, conflictPath, d.agentName)
				} else if d.stdout != nil {
					fmt.Fprintf(d.stdout, "  skipped %s: %s exists and is not owned by pop — remove it and re-run integrate to install pop's version\n", name, conflictPath)
				}
				continue
			}
		}
		if err := d.removeAll(dest); err != nil {
			return fmt.Errorf("failed to remove %s: %w", dest, err)
		}
		if err := d.symlink(target, dest); err != nil {
			return fmt.Errorf("failed to symlink %s -> %s: %w", dest, target, err)
		}
		if d.logf != nil {
			d.logf("installFileComponent: linked %s -> %s", dest, target)
		}
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "  linked %s -> %s\n", dest, target)
		}
	}

	if err := pruneStaleAgentEntries(d, agentDir, renderRoot, id, agent, topLevel, prefix); err != nil {
		return err
	}

	return nil
}

// pruneStaleAgentEntries performs the agent-location half of stale-name cleanup
// (ADR 0063). After this component's fresh names have been linked, any
// pop-owned entry left at the agent location that this component no longer
// renders is stale — it was installed under a different skills_prefix — and is
// removed.
func pruneStaleAgentEntries(d *integrateDeps, agentDir, renderRoot string, id ComponentID, agent string, keep map[string]bool, prefix string) error {
	if d.readDirNames == nil {
		return nil
	}
	names, err := d.readDirNames(agentDir)
	if err != nil {
		return fmt.Errorf("failed to list %s: %w", agentDir, err)
	}
	dataDir, err := d.dataDir()
	if err != nil {
		return err
	}
	renderRoots := fileComponentRenderRoots(dataDir, agent, id)
	possible := componentPossibleNames(id, agent, config.DefaultSkillsPrefix, prefix, "")

	for _, name := range names {
		if keep[name] {
			continue
		}
		dest := filepath.Join(agentDir, name)
		mode, err := d.lstatMode(dest)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to stat %s: %w", dest, err)
		}

		if !componentOwnsAgentEntry(d, name, dest, mode, renderRoots, possible) {
			continue
		}
		if d.logf != nil {
			d.logf("installFileComponent: pruning stale %s — no longer a rendered name for %s/%s", dest, agent, id)
		}
		if err := d.removeAll(dest); err != nil {
			return fmt.Errorf("failed to remove stale entry %s: %w", dest, err)
		}
		d.prunedStale = append(d.prunedStale, name)
	}
	return nil
}

func componentPossibleNames(id ComponentID, agent string, prefixes ...string) map[string]bool {
	names := map[string]bool{}
	for _, p := range prefixes {
		tree, err := renderComponent(id, agent, p)
		if err != nil {
			continue
		}
		for rel := range tree {
			names[firstSegment(rel)] = true
		}
	}
	return names
}

func componentOwnsAgentEntry(d *integrateDeps, name, dest string, mode os.FileMode, renderRoots []string, possible map[string]bool) bool {
	switch {
	case mode&os.ModeSymlink != 0:
		target, err := d.readlink(dest)
		if err != nil {
			return false
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(dest), target)
		}
		return symlinkUnderRenderRoot(target, renderRoots)
	case possible[name]:
		return ownedByMarker(d, dest, mode)
	default:
		return false
	}
}

// fileComponentInstalledNames returns pop-owned agent-location entry names for
// this component, regardless of prefix or base name (ADR 0063).
func fileComponentInstalledNames(d *integrateDeps, home string, id ComponentID, agent string) (map[string]bool, error) {
	agent = strings.ToLower(agent)
	dataDir, err := d.dataDir()
	if err != nil {
		return nil, err
	}
	agentDir, err := agentSkillDir(home, agent, id)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	if d.readDirNames == nil {
		return out, nil
	}
	names, err := d.readDirNames(agentDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", agentDir, err)
	}
	renderRoots := fileComponentRenderRoots(dataDir, agent, id)
	prefix := d.resolveSkillsPrefix()
	possible := componentPossibleNames(id, agent, config.DefaultSkillsPrefix, prefix, "")
	for _, name := range names {
		dest := filepath.Join(agentDir, name)
		mode, err := d.lstatMode(dest)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to stat %s: %w", dest, err)
		}
		if componentOwnsAgentEntry(d, name, dest, mode, renderRoots, possible) {
			out[name] = true
		}
	}
	return out, nil
}

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
		if d.logf != nil {
			d.logf("ownership: %s is symlink -> %s (inTree=%v)", dest, target, inTree)
		}
		return true, inTree, nil
	}
	return true, ownedByMarker(d, dest, mode), nil
}

func ownedByMarker(d *integrateDeps, dest string, mode os.FileMode) bool {
	target := dest
	if mode.IsDir() {
		target = filepath.Join(dest, "SKILL.md")
	}
	data, err := d.readFile(target)
	if err != nil {
		if d.logf != nil {
			d.logf("ownership: %s not pop-owned (cannot read %s: %v)", dest, target, err)
		}
		return false
	}
	owned := frontmatterHasOwnershipMarker(string(data))
	if d.logf != nil {
		d.logf("ownership: %s pop-owned=%v via marker in %s", dest, owned, target)
	}
	return owned
}

func skillConflict(d *integrateDeps, agentDir, name, integrationsRoot, prefix string) (conflictPath string, conflict bool, err error) {
	for _, cand := range conflictCandidates(name, prefix) {
		p := filepath.Join(agentDir, cand)
		exists, owned, err := ownership(d, p, integrationsRoot)
		if err != nil {
			return "", false, err
		}
		if exists && !owned {
			return p, true, nil
		}
	}
	return "", false, nil
}

func resolveConflictOverwrite(d *integrateDeps, conflictPath string) (bool, error) {
	if d.assumeYes {
		return true, nil
	}
	if d.interactive {
		return promptOverwriteConflict(d.stdin, d.stdout, conflictPath)
	}
	return false, nil
}

func conflictCandidates(name, prefix string) []string {
	if prefix == "" {
		return []string{name}
	}
	if bare := strings.TrimPrefix(name, prefix); bare != name {
		return []string{name, bare}
	}
	return []string{name}
}

func agentSkillDir(home, agent string, id ComponentID) (string, error) {
	switch strings.ToLower(agent) {
	case "claude":
		return filepath.Join(home, ".claude", "skills"), nil
	case "codex":
		return filepath.Join(home, ".codex", "skills"), nil
	case "pi":
		return filepath.Join(home, ".pi", "agent", "skills"), nil
	case "cursor":
		return filepath.Join(home, ".cursor", "skills"), nil
	case "opencode":
		if id == ComponentPaneSkill {
			return filepath.Join(home, ".config", "opencode", "agent"), nil
		}
		return filepath.Join(home, ".config", "opencode", "skills"), nil
	default:
		return "", fmt.Errorf("agent %q has no skill location", agent)
	}
}

func legacyArtifacts(home, agent string, id ComponentID) []string {
	if strings.ToLower(agent) == "claude" && id == ComponentPaneSkill {
		return []string{filepath.Join(home, ".claude", "commands", "pop", "pane.md")}
	}
	return nil
}

func firstSegment(rel string) string {
	rel = filepath.ToSlash(rel)
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return rel
}
