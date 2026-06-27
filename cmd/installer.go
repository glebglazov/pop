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
// skill_prefix are pruned (ADR 0063).
func installFileComponent(d *integrateDeps, home string, id ComponentID, agent string) error {
	agent = strings.ToLower(agent)
	prefix := d.resolveSkillPrefix()

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

	agentDir, err := agentSkillDir(home, agent)
	if err != nil {
		return err
	}

	if d.logf != nil {
		d.logf("installFileComponent: agent=%s id=%s prefix=%q agentDir=%s renderRoot=%s", agent, id, prefix, agentDir, renderRoot)
	}

	// Remove any legacy copy-mode artifacts this component supersedes (e.g. the
	// claude command-style pane install) so the switch to skills leaves nothing
	// behind.
	for _, p := range legacyArtifacts(home, agent, id) {
		if d.logf != nil {
			d.logf("installFileComponent: removing legacy artifact %s", p)
		}
		if err := d.removeAll(p); err != nil {
			return fmt.Errorf("failed to remove legacy artifact %s: %w", p, err)
		}
	}

	// Render the tree fresh under the data dir. Clear the prior render root
	// first so a renamed or removed file does not linger.
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

		// Integration conflict check (ADR 0011): a same-named entry pop does
		// not own — under the resolved install name OR the bare (prefix-stripped)
		// form — is never touched. The skill is skipped: never overwritten,
		// never removed, never refreshed. Non-conflicting skills in the same run
		// still install.
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
		// Remove the existing entry (a stale symlink, or a pop-owned copy-mode
		// directory being migrated) and link to the render tree.
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

	// Stale-name cleanup (ADR 0063): the resolved name can change between runs
	// (e.g. a new skill_prefix flips `pop-pane` → `pane`). The render-tree side
	// is already pruned — the removeAll(renderRoot) above wiped any old-named
	// directory before the fresh tree was written. The agent location still
	// holds the old-named pop-owned symlink, so subtract the freshly rendered
	// names from what's there and remove the leftovers.
	if err := pruneStaleAgentEntries(d, agentDir, renderRoot, id, agent, topLevel); err != nil {
		return err
	}

	return nil
}

// pruneStaleAgentEntries performs the agent-location half of stale-name cleanup
// (ADR 0063). After this component's fresh names have been linked, any
// pop-owned entry left at the agent location that this component no longer
// renders is stale — it was installed under a different skill_prefix — and is
// removed. This is set subtraction: `keep` is the set of freshly rendered
// top-level names; an entry whose name is in `keep` is left as-is.
//
// Ownership and scoping mirror the install path so cleanup never reaches across
// components or touches a user's own skill (criterion: never remove an unowned
// entry):
//
//   - A symlink resolving into THIS component's render root is a leftover link
//     from a prior install of this component under a different prefix → removed.
//     This covers any old prefix, including a custom one, because the render
//     root is per-component, not per-name.
//   - A real, marker-owned entry whose name is one this component could have
//     produced under the default (`pop-`) or bare prefix is a stale copy-mode
//     artifact → removed.
//   - Anything else — a symlink into another component's render tree, an
//     unowned entry, a foreign skill — is left untouched.
func pruneStaleAgentEntries(d *integrateDeps, agentDir, renderRoot string, id ComponentID, agent string, keep map[string]bool) error {
	if d.readDirNames == nil {
		return nil // no directory listing available — nothing to prune
	}
	names, err := d.readDirNames(agentDir)
	if err != nil {
		return fmt.Errorf("failed to list %s: %w", agentDir, err)
	}
	// Names this component could have produced under the default or bare prefix
	// — the scope for pruning real (copy-mode) marker-owned leftovers, which
	// carry no render-tree target to attribute them to a component.
	possible := componentPossibleNames(id, agent, config.DefaultSkillPrefix, "")
	cleanRenderRoot := filepath.Clean(renderRoot)

	for _, name := range names {
		if keep[name] {
			continue // a freshly rendered name — keep it
		}
		dest := filepath.Join(agentDir, name)
		mode, err := d.lstatMode(dest)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to stat %s: %w", dest, err)
		}

		if !componentOwnsAgentEntry(d, name, dest, mode, cleanRenderRoot, possible) {
			continue
		}
		if d.logf != nil {
			d.logf("installFileComponent: pruning stale %s — no longer a rendered name for %s/%s", dest, agent, id)
		}
		if err := d.removeAll(dest); err != nil {
			return fmt.Errorf("failed to remove stale entry %s: %w", dest, err)
		}
		if d.stdout != nil {
			fmt.Fprintf(d.stdout, "  removed stale %s\n", dest)
		}
	}
	return nil
}

// componentPossibleNames returns the union of the top-level render-tree names a
// component produces under each of the given prefixes. Used to scope stale
// marker-owned (copy-mode) cleanup to names this component owns, so a prune of
// one component never removes another's entries.
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

// componentOwnsAgentEntry reports whether the agent-location entry `name` (with
// the given lstat `mode`) is a pop-owned artifact attributable to THIS
// component — the shared predicate behind both stale-name pruning and the
// name-agnostic installed check (ADR 0063). Attribution mirrors the install
// path so it never reaches across components or touches a user's own skill:
//
//   - A symlink resolving into the component's render root — a link this
//     component installed, under any prefix (the render root is per-component,
//     not per-name), so a renamed/re-prefixed entry is still recognised.
//   - A real, marker-owned entry whose name this component could produce under
//     the default (`pop-`) or bare prefix — a copy-mode artifact, which carries
//     no render-tree target to attribute it by, so it is scoped by name.
//
// Anything else — a symlink into another component's render tree, an unowned
// entry, a foreign skill — is not owned by this component.
func componentOwnsAgentEntry(d *integrateDeps, name, dest string, mode os.FileMode, cleanRenderRoot string, possible map[string]bool) bool {
	switch {
	case mode&os.ModeSymlink != 0:
		target, err := d.readlink(dest)
		if err != nil {
			return false
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(dest), target)
		}
		target = filepath.Clean(target)
		return target == cleanRenderRoot || strings.HasPrefix(target, cleanRenderRoot+string(filepath.Separator))
	case possible[name]:
		return ownedByMarker(d, dest, mode)
	default:
		return false
	}
}

// fileComponentInstalledNames returns the set of agent-location entry names that
// are pop-owned artifacts attributable to this component, regardless of the
// prefix or base name they were installed under (ADR 0063). It is the
// name-agnostic "is this component opted in" probe used by refresh: a skill
// installed as `pop-pane` is still found after the base renames to
// `pop-tmux-pane` or the prefix flips to bare, because attribution keys on the
// per-component render root, not the entry's name.
func fileComponentInstalledNames(d *integrateDeps, home string, id ComponentID, agent string) (map[string]bool, error) {
	agent = strings.ToLower(agent)
	dataDir, err := d.dataDir()
	if err != nil {
		return nil, err
	}
	renderRoot := filepath.Join(dataDir, "integrations", agent, string(id))
	agentDir, err := agentSkillDir(home, agent)
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
	possible := componentPossibleNames(id, agent, config.DefaultSkillPrefix, "")
	cleanRenderRoot := filepath.Clean(renderRoot)
	for _, name := range names {
		dest := filepath.Join(agentDir, name)
		mode, err := d.lstatMode(dest)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to stat %s: %w", dest, err)
		}
		if componentOwnsAgentEntry(d, name, dest, mode, cleanRenderRoot, possible) {
			out[name] = true
		}
	}
	return out, nil
}

// ownership reports whether an entry exists at dest and whether pop owns it.
//
// Ownership is decided in two ways, strongest first:
//   - A symlink whose target resolves under pop's integrations root — the
//     canonical ADR 0011 marker (also covers dangling symlinks, which have no
//     frontmatter to read).
//   - A real entry whose frontmatter carries the `pop-owned: true` marker — a
//     copy-mode install rendered by pop, eligible for migration. The marker is
//     name-independent (skill-prefix slice 02), replacing the legacy `pop-`
//     name-prefix test.
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
		if d.logf != nil {
			d.logf("ownership: %s is symlink -> %s (inTree=%v)", dest, target, inTree)
		}
		return true, inTree, nil
	}
	return true, ownedByMarker(d, dest, mode), nil
}

// ownedByMarker reports whether a real (non-symlink) entry is pop-owned by
// reading its frontmatter for the `pop-owned: true` marker. A multi-file skill
// carries the marker in its `SKILL.md`; a flat copy-mode skill carries it in
// the file itself. An unreadable file (or one without the marker) is not owned.
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

// skillConflict reports whether installing the render-tree entry `name` into
// agentDir would collide with a skill pop does not own. The candidates derive
// from the resolved install name (ADR 0063): the resolved name as rendered,
// plus the bare form with the configured prefix stripped (e.g. with prefix
// `pop-`, `pop-pane` → also check `pane`). A hand-written skill could sit under
// either, and either shadows pop's version. The first existing entry that pop
// does not own is the conflict. With an empty prefix the resolved name is
// already bare, so it is the sole candidate.
//
// A pop-owned entry (a symlink resolving into pop's render tree, or a
// marker-owned copy-mode directory eligible for migration) is never a conflict,
// so re-install and refresh proceed normally.
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

// resolveConflictOverwrite decides whether to destroy an unowned conflict entry
// during --overwrite-conflicts. --yes overwrites unattended; an interactive TTY
// prompts; a non-interactive run skips without blocking.
func resolveConflictOverwrite(d *integrateDeps, conflictPath string) (bool, error) {
	if d.assumeYes {
		return true, nil
	}
	if d.interactive {
		return promptOverwriteConflict(d.stdin, d.stdout, conflictPath)
	}
	return false, nil
}

// conflictCandidates returns the entry names a render-tree entry can collide
// with at the agent location, derived from the resolved install `name` and the
// configured `prefix` (ADR 0063): the resolved name as rendered, plus the bare
// form with the prefix stripped (e.g. with prefix `pop-`, `pop-pane` → `pane`,
// `pop-pane.md` → `pane.md`). An empty prefix, or a name that does not carry
// the prefix, yields only the name itself.
func conflictCandidates(name, prefix string) []string {
	if prefix == "" {
		return []string{name}
	}
	if bare := strings.TrimPrefix(name, prefix); bare != name {
		return []string{name, bare}
	}
	return []string{name}
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
	case "codex":
		return filepath.Join(home, ".codex", "skills"), nil
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
