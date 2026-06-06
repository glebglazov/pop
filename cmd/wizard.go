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

// The Integration wizard (ADR 0010) is the interactive form of
// `pop integrate <agent>`: a re-entrant, step-by-step consent flow over the
// component catalog. It installs the core status wiring with no prompt (consent
// is implied by running the command), then walks one explained y/n step per
// opt-in component in catalog order. Declining any step skips it and the
// wizard continues. Each step first reports the component's current state; conflict
// and not-supported states print their report instead of prompting. The wizard
// closes with a note that re-running adds or removes components at any time.

// componentStateKind enumerates the states a component can be in for an agent,
// as shown to the user before each wizard step. These mirror the states Doctor
// reports in a later slice.
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

// runIntegrateWizard drives the interactive wizard for an agent. It assumes the
// agent is known and supported (the caller guards that) and that d.stdin is a
// terminal (the caller gates on interactivity).
func runIntegrateWizard(d *integrateDeps, home, agent string) error {
	out := d.stdout
	in := bufio.NewReader(stdinOrEmpty(d.stdin))

	// Core step — installed with no prompt; consent is implied by running the
	// integrate command at all (ADR 0010 consent gradient).
	if out != nil {
		fmt.Fprintf(out, "Integrating pop with %s.\n\n", agent)
		fmt.Fprintf(out, "Core: status wiring\n")
		fmt.Fprintf(out, "  Makes %s report pane status to pop's monitor. It changes no agent\n", agent)
		fmt.Fprintf(out, "  behavior, so it installs without a prompt.\n")
	}
	core, ok := lookupComponent(ComponentStatusWiring)
	if !ok {
		return fmt.Errorf("status-wiring component missing from catalog")
	}
	if err := core.install(d, home, agent); err != nil {
		return err
	}

	// Opt-in steps in catalog order.
	for _, comp := range integrationCatalog {
		switch comp.id {
		case ComponentStatusWiring:
			continue
		case ComponentPaneSkill:
			if err := wizardFileComponentStep(d, home, agent, in, comp.id, "Pane skill", paneSkillExplanation); err != nil {
				return err
			}
		case ComponentTaskSkills:
			if err := wizardFileComponentStep(d, home, agent, in, comp.id, "Task planning skills", taskSkillsExplanation); err != nil {
				return err
			}
		}
	}

	if out != nil {
		fmt.Fprintf(out, "\nDone. Re-run `pop integrate %s` anytime to add or remove components.\n", agent)
	}
	return nil
}

const paneSkillExplanation = `  The pane skill lets the agent drive tmux panes directly: running long
  processes in named panes, watching their output, sending input, and
  marking panes for your attention. This injects new agent behavior, so it
  is opt-in.`

const taskSkillsExplanation = `  The task planning skills install pop's planning-to-execution flow:
  a grilling session that stress-tests your design (grill-with-docs), then
  a PRD (to-prd), then a breakdown into independently-runnable tasks
  (to-tasks) that ` + "`pop tasks drain`" + ` executes, one agent per task.`

// wizardFileComponentStep runs one wizard step for a file-based opt-in
// component (the pane skill, the task planning skills). It reports the
// component's current state; for conflict and not-supported it prints the
// report and returns without prompting, otherwise it explains the component and
// asks y/n, installing on yes.
func wizardFileComponentStep(d *integrateDeps, home, agent string, in *bufio.Reader, id ComponentID, title, explanation string) error {
	out := d.stdout
	state, err := wizardFileComponentState(d, home, id, agent)
	if err != nil {
		return err
	}
	if out != nil {
		fmt.Fprintf(out, "\n%s\n", title)
	}
	switch state.kind {
	case stateNotSupported:
		if out != nil {
			fmt.Fprintf(out, "  Not supported for %s — skipping (pop never installs a degraded version).\n", agent)
		}
		return nil
	case stateConflict:
		if out != nil {
			fmt.Fprintf(out, "  Conflict: %s exists and is not owned by pop.\n", state.conflictPath)
			fmt.Fprintf(out, "  Remove it and re-run the wizard to install pop's version.\n")
		}
		return nil
	case stateInstalledCurrent:
		fmt.Fprintf(orDiscard(out), "  Currently: installed and up to date.\n")
	case stateStale:
		fmt.Fprintf(orDiscard(out), "  Currently: installed but out of date.\n")
	case stateNotInstalled:
		fmt.Fprintf(orDiscard(out), "  Currently: not installed.\n")
	}
	if out != nil {
		fmt.Fprintf(out, "%s\n", explanation)
	}
	yes, err := promptYesNo(in, out, "Install "+strings.ToLower(title)+"?")
	if err != nil {
		return err
	}
	if !yes {
		fmt.Fprintf(orDiscard(out), "  Skipped.\n")
		return nil
	}
	return installFileComponent(d, home, id, agent)
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
