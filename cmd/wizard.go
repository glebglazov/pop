package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The per-component consent wizard (ADR 0010) has been retired: `pop integrate
// <agent>` now installs the full default set without prompting (ADR 0064). The
// component state-computation helpers below survive because Doctor and
// Integration refresh consume them to report and reconcile component state.

// componentStateKind enumerates the states a component can be in for an agent.
// Doctor reports these; refresh reconciles against them.
type componentStateKind int

const (
	stateNotInstalled componentStateKind = iota
	stateInstalledCurrent
	stateStale
	stateConflict
	stateNotSupported
)

// componentStateInfo carries a component's computed state and, for a conflict,
// the path of the entry pop does not own.
type componentStateInfo struct {
	kind         componentStateKind
	conflictPath string
}

// wizardFileComponentState computes the displayable state of a file-based
// component for an agent: not-supported, conflict, not-installed, stale, or
// installed-current. It composes the catalog support matrix, the link
// installer's ownership/conflict checks, and a render-tree byte comparison —
// adding no state logic of its own.
func wizardFileComponentState(d *integrateDeps, home string, id ComponentID, agent string) (componentStateInfo, error) {
	comp, ok := lookupComponent(id)
	if !ok {
		return componentStateInfo{}, fmt.Errorf("unknown component %q", id)
	}
	if !comp.supported(agent) {
		return componentStateInfo{kind: stateNotSupported}, nil
	}
	conflictPath, conflict, err := componentConflict(d, home, id, agent)
	if err != nil {
		return componentStateInfo{}, err
	}
	if conflict {
		return componentStateInfo{kind: stateConflict, conflictPath: conflictPath}, nil
	}
	installed, err := fileComponentInstalled(d, home, id, agent)
	if err != nil {
		return componentStateInfo{}, err
	}
	if !installed {
		return componentStateInfo{kind: stateNotInstalled}, nil
	}
	stale, err := fileComponentStale(d, home, id, agent)
	if err != nil {
		return componentStateInfo{}, err
	}
	if stale {
		return componentStateInfo{kind: stateStale}, nil
	}
	return componentStateInfo{kind: stateInstalledCurrent}, nil
}

// componentConflict reports the first agent-location entry that would collide
// with a render-tree top-level name and that pop does not own (an Integration
// conflict, ADR 0011).
func componentConflict(d *integrateDeps, home string, id ComponentID, agent string) (string, bool, error) {
	agent = strings.ToLower(agent)
	prefix := d.resolveSkillPrefix()
	tree, err := renderComponent(id, agent, prefix)
	if err != nil {
		return "", false, err
	}
	dataDir, err := d.dataDir()
	if err != nil {
		return "", false, err
	}
	integrationsRoot := filepath.Join(dataDir, "integrations")
	agentDir, err := agentSkillDir(home, agent)
	if err != nil {
		return "", false, err
	}
	seen := map[string]bool{}
	for rel := range tree {
		name := firstSegment(rel)
		if seen[name] {
			continue
		}
		seen[name] = true
		p, conflict, err := skillConflict(d, agentDir, name, integrationsRoot, prefix)
		if err != nil {
			return "", false, err
		}
		if conflict {
			return p, true, nil
		}
	}
	return "", false, nil
}

// fileComponentStale reports whether the render tree on disk under pop's data
// directory differs from a fresh render of the embedded sources (a missing
// rendered file counts as stale). The component must already be installed for
// this to be meaningful; callers check installed first.
func fileComponentStale(d *integrateDeps, home string, id ComponentID, agent string) (bool, error) {
	agent = strings.ToLower(agent)
	tree, err := renderComponent(id, agent, d.resolveSkillPrefix())
	if err != nil {
		return false, err
	}
	dataDir, err := d.dataDir()
	if err != nil {
		return false, err
	}
	renderRoot := filepath.Join(dataDir, "integrations", agent, string(id))
	for rel, data := range tree {
		existing, err := d.readFile(filepath.Join(renderRoot, rel))
		if err != nil {
			if os.IsNotExist(err) {
				return true, nil
			}
			return false, err
		}
		if !bytes.Equal(existing, data) {
			return true, nil
		}
	}
	return false, nil
}

// fileComponentStaleResolved reports whether the installed state of a file
// component diverges from the expected resolved state (ADR 0063), the reconcile
// definition of "stale". It generalises fileComponentStale's content check with
// a resolved-name check: the caller passes the set of pop-owned entry names
// currently installed for this component (from fileComponentInstalledNames), and
// this compares it against the freshly resolved render names.
//
// Two kinds of divergence make a component stale:
//   - Name: the set of installed owned names ≠ the set the current config/binary
//     would render (a skill_prefix change or a base rename — `pop-pane` →
//     `pop-tmux-pane`). The wrong name being linked, or the right name missing,
//     both surface here even when the rendered bytes are identical.
//   - Content: names match but the rendered bytes on disk differ from a fresh
//     render (the original staleness, preserved).
//
// Re-rendering and re-linking under the resolved name and pruning the old entry
// is handled by installFileComponent, which the caller invokes when stale.
func fileComponentStaleResolved(d *integrateDeps, home string, id ComponentID, agent string, installedNames map[string]bool) (bool, error) {
	agent = strings.ToLower(agent)
	tree, err := renderComponent(id, agent, d.resolveSkillPrefix())
	if err != nil {
		return false, err
	}
	expected := map[string]bool{}
	for rel := range tree {
		expected[firstSegment(rel)] = true
	}
	if !nameSetsEqual(installedNames, expected) {
		if d.logf != nil {
			d.logf("fileComponentStaleResolved: %s/%s resolved-name divergence installed=%v expected=%v — stale",
				agent, id, sortedSet(installedNames), sortedSet(expected))
		}
		return true, nil // resolved install name differs from what's installed
	}
	return fileComponentStale(d, home, id, agent)
}

// nameSetsEqual reports whether two name sets hold exactly the same keys.
func nameSetsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// sortedSet returns the keys of a name set in sorted order, for stable logs.
func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
