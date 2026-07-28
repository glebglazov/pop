package integrate

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
)

// Shared integration component state helpers and conflict/overwrite prompts.
// The Integration wizard (ADR 0010) was retired in ADR 0065; bare integrate
// installs the merged baseline with no per-component prompts.

// ComponentStateKind enumerates the states a component can be in for an agent.
// These mirror the states Doctor reports.
type ComponentStateKind int

const (
	StateNotInstalled ComponentStateKind = iota
	StateInstalledCurrent
	StateStale
	StateConflict
	StateNotSupported
)

// ComponentStateInfo carries a component's computed state and, for a conflict,
// the path of the entry pop does not own.
type ComponentStateInfo struct {
	Kind         ComponentStateKind
	ConflictPath string
}

// wizardFileComponentState computes the displayable state of a file-based
// component for an agent: not-supported, conflict, not-installed, stale, or
// installed-current. It composes the catalog support matrix, the link
// installer's ownership/conflict checks, and a render-tree byte comparison —
// adding no state logic of its own.
func wizardFileComponentState(d *Deps, home string, id ComponentID, agent string) (ComponentStateInfo, error) {
	comp, ok := LookupComponent(id)
	if !ok {
		return ComponentStateInfo{}, fmt.Errorf("unknown component %q", id)
	}
	if !comp.supported(agent) {
		return ComponentStateInfo{Kind: StateNotSupported}, nil
	}
	conflictPath, conflict, err := componentConflict(d, home, id, agent)
	if err != nil {
		return ComponentStateInfo{}, err
	}
	if conflict {
		return ComponentStateInfo{Kind: StateConflict, ConflictPath: conflictPath}, nil
	}
	installed, err := fileComponentInstalled(d, home, id, agent)
	if err != nil {
		return ComponentStateInfo{}, err
	}
	if !installed {
		return ComponentStateInfo{Kind: StateNotInstalled}, nil
	}
	stale, err := fileComponentStale(d, home, id, agent)
	if err != nil {
		return ComponentStateInfo{}, err
	}
	if stale {
		return ComponentStateInfo{Kind: StateStale}, nil
	}
	return ComponentStateInfo{Kind: StateInstalledCurrent}, nil
}

// componentConflict reports the first agent-location entry that would collide
// with a render-tree top-level name and that pop does not own (an Integration
// conflict, ADR 0011).
func componentConflict(d *Deps, home string, id ComponentID, agent string) (string, bool, error) {
	agent = strings.ToLower(agent)
	prefix := d.resolveSkillsPrefix()
	tree, err := renderComponent(id, agent, prefix)
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
func fileComponentStale(d *Deps, home string, id ComponentID, agent string) (bool, error) {
	agent = strings.ToLower(agent)
	tree, err := renderComponent(id, agent, d.resolveSkillsPrefix())
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

func fileComponentStaleResolved(d *Deps, home string, id ComponentID, agent string, installedNames map[string]bool) (bool, error) {
	agent = strings.ToLower(agent)
	tree, err := renderComponent(id, agent, d.resolveSkillsPrefix())
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
		return true, nil
	}
	return fileComponentStale(d, home, id, agent)
}

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

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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

// AgentIntent describes one agent pop treats as intended for integration.
type AgentIntent struct {
	Agent   string
	Sources []string
}

// AgentSuggestion describes an agent executable found on PATH without intent.
type AgentSuggestion struct {
	Agent  string
	Reason string
}

// AgentIntentReport is the doctor-facing agent intent read surface.
type AgentIntentReport struct {
	Intended    []AgentIntent
	Suggestions []AgentSuggestion
}

// ComponentState computes a component's state for an agent.
func ComponentState(d *Deps, home string, id ComponentID, agent string) (ComponentStateInfo, error) {
	comp, ok := LookupComponent(id)
	if !ok {
		return ComponentStateInfo{}, fmt.Errorf("unknown component %q", id)
	}
	if !comp.supported(agent) {
		return ComponentStateInfo{Kind: StateNotSupported}, nil
	}
	switch id {
	case ComponentStatusWiring:
		installed, err := statusWiringInstalled(d, home, agent)
		if err != nil {
			return ComponentStateInfo{}, err
		}
		return installedState(installed), nil
	default:
		return fileComponentState(d, home, id, agent)
	}
}

func installedState(installed bool) ComponentStateInfo {
	if installed {
		return ComponentStateInfo{Kind: StateInstalledCurrent}
	}
	return ComponentStateInfo{Kind: StateNotInstalled}
}

func fileComponentState(d *Deps, home string, id ComponentID, agent string) (ComponentStateInfo, error) {
	return wizardFileComponentState(d, home, id, agent)
}

// KnownAgent reports whether agent is a supported integration agent name.
func KnownAgent(agent string) bool {
	for _, candidate := range Agents {
		if candidate == strings.ToLower(agent) {
			return true
		}
	}
	return false
}

// DetectAgentIntent derives intended agents from config, installed artifacts, and PATH.
func DetectAgentIntent(d *Deps, home string, loadConfig func(string) (*config.Config, error), explicit []string, executableAvailable func(string) bool) (*AgentIntentReport, error) {
	report := &AgentIntentReport{}
	intentByAgent := map[string]int{}
	addIntent := func(agent, source string) {
		agent = strings.ToLower(agent)
		if !KnownAgent(agent) {
			return
		}
		idx, ok := intentByAgent[agent]
		if !ok {
			report.Intended = append(report.Intended, AgentIntent{Agent: agent})
			idx = len(report.Intended) - 1
			intentByAgent[agent] = idx
		}
		if source != "" && !stringSliceContains(report.Intended[idx].Sources, source) {
			report.Intended[idx].Sources = append(report.Intended[idx].Sources, source)
		}
	}

	if loadConfig != nil {
		cfg, err := loadConfig(config.DefaultConfigPath())
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("load task config for agent intent: %w", err)
			}
		} else if cfg != nil && cfg.Task != nil {
			for agent := range cfg.Task.Presets {
				addIntent(agent, "task config")
			}
		}
	}

	MergeExplicitAgentContext(report, explicit)

	for _, agent := range Agents {
		for _, comp := range catalog {
			state, err := ComponentState(d, home, comp.id, agent)
			if err != nil {
				return nil, err
			}
			switch state.Kind {
			case StateInstalledCurrent, StateStale:
				addIntent(agent, "pop-owned integration artifacts")
			}
		}
	}

	for _, agent := range Agents {
		if _, ok := intentByAgent[agent]; ok {
			continue
		}
		if executableAvailable != nil && executableAvailable(agent) {
			report.Suggestions = append(report.Suggestions, AgentSuggestion{
				Agent:  agent,
				Reason: "agent executable is available on PATH but no Pop intent was detected",
			})
		}
	}

	return report, nil
}

// MergeExplicitAgentContext adds explicit command-context agents to a report.
func MergeExplicitAgentContext(report *AgentIntentReport, agents []string) {
	if report == nil {
		return
	}
	intentByAgent := map[string]int{}
	for i := range report.Intended {
		intentByAgent[report.Intended[i].Agent] = i
	}
	for _, agent := range agents {
		agent = strings.ToLower(agent)
		if !KnownAgent(agent) {
			continue
		}
		idx, ok := intentByAgent[agent]
		if !ok {
			report.Intended = append(report.Intended, AgentIntent{Agent: agent})
			idx = len(report.Intended) - 1
			intentByAgent[agent] = idx
		}
		if !stringSliceContains(report.Intended[idx].Sources, "explicit command context") {
			report.Intended[idx].Sources = append(report.Intended[idx].Sources, "explicit command context")
		}
	}
}

func stringSliceContains(items []string, item string) bool {
	for _, existing := range items {
		if existing == item {
			return true
		}
	}
	return false
}
