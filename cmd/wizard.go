package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Shared integration component state helpers and conflict/overwrite prompts.
// The Integration wizard (ADR 0010) was retired in ADR 0065; bare integrate
// installs the merged baseline with no per-component prompts.

// componentStateKind enumerates the states a component can be in for an agent.
// These mirror the states Doctor reports.
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
	tree, err := renderComponent(id, agent)
	if err != nil {
		return "", false, err
	}
	dataDir, err := d.dataDir()
	if err != nil {
		return "", false, err
	}
	integrationsRoot := filepath.Join(dataDir, "integrations")
	agentDir, err := agentSkillDir(home, agent, id)
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
		p, conflict, err := skillConflict(d, agentDir, name, integrationsRoot)
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
	tree, err := renderComponent(id, agent)
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

// promptYesNo writes the prompt and reads one line, returning true only for an
// affirmative answer. An empty answer, EOF, or nil input is a decline — the
// wizard's default for every opt-in step is "no".
func promptYesNo(in *bufio.Reader, out io.Writer, prompt string) (bool, error) {
	if in == nil {
		return false, nil
	}
	if out != nil {
		fmt.Fprintf(out, "%s [y/N]: ", prompt)
	}
	line, err := in.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

// promptOverwriteConflict asks whether to destroy an unowned entry blocking an
// integration install. Default is No (empty/Enter declines).
func promptOverwriteConflict(in io.Reader, out io.Writer, conflictPath string) (bool, error) {
	return promptYesNo(bufio.NewReader(stdinOrEmpty(in)), out, fmt.Sprintf("Overwrite %s? It is not owned by pop", conflictPath))
}

// stdinOrEmpty returns r, or an always-EOF reader when r is nil, so the wizard
// declines every prompt rather than panicking on a nil reader.
func stdinOrEmpty(r io.Reader) io.Reader {
	if r == nil {
		return strings.NewReader("")
	}
	return r
}

// orDiscard returns w, or io.Discard when w is nil, so state lines can be
// written unconditionally without nil checks at every call site.
func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}
