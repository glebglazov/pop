package tasks

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// refineGateSession uses the manual Refine path; automatic drain passes keep
// their own options and never read the session choice.
type refineGateSession struct {
	options  RefineOptions
	deps     *Deps
	repo     string
	manifest *Manifest
}

func (r *refineGateSession) entries(cfg *config.Config) []AgentGroupEntry {
	entries := cfg.RefineAgentEntries()
	if over := r.manifest.RefinerOverride(); over != nil && len(nonEmptyStrings(over.Agents)) > 0 {
		entries = nil
		for _, spec := range nonEmptyStrings(over.Agents) {
			entries = append(entries, config.AgentEntry{Cmd: spec})
		}
	} else if len(entries.Commands()) == 0 {
		sel, err := resolveRefiner(nil, "", r.manifest, cfg)
		if err != nil {
			return nil
		}
		entries = nil
		for _, spec := range sel.Agents {
			entry := config.AgentEntry{Cmd: spec}
			for _, inherited := range cfg.ImplementAgentEntries() {
				if inherited.Cmd == spec {
					entry = inherited
					break
				}
			}
			entries = append(entries, entry)
		}
	}
	group := &config.Config{Work: &config.WorkConfig{Refine: &config.RefineConfig{Agents: entries}}}
	return usableGroupEntries(group, "refine")
}

func (r *refineGateSession) decorate(spec *ui.GateMenuSpec, choice *AgentGroupEntry, cfg *config.Config) {
	sel, err := resolveRefiner(r.options.Agents, r.options.Effort, r.manifest, cfg)
	label := ""
	var entry AgentGroupEntry
	if choice != nil {
		entry = *choice
	} else if err == nil && len(sel.Agents) > 0 {
		entry = LaunchedAttendedEntry(nil, sel.Agents[0])
		for _, configured := range r.entries(cfg) {
			if configured.Cmd == entry.Cmd {
				entry = configured
				break
			}
		}
	}
	if entry.Cmd != "" {
		// The fallback walk uses this same effort resolution at launch.
		resolved := resolveEffortModel(entry.Cmd, sel.Effort, true, cfg, nil)
		entry.Model = AgentSpecModel(resolved.Spec)
		label = FormatAgentEntry(entry)
	}
	for i := range spec.Items {
		if spec.Items[i].Role == "refine" {
			spec.Items[i].AgentLabel = label
			spec.Items[i].AgentPickable = len(r.entries(cfg)) > 1
		}
	}
}

func (r *refineGateSession) addItems(spec *ui.GateMenuSpec) {
	if r.manifest == nil || !r.manifest.Valid {
		return
	}
	exit := spec.Items[len(spec.Items)-1]
	spec.Items = append(spec.Items[:len(spec.Items)-1], ui.GateMenuItem{Key: "f", Label: "Refine (apply the implementation standard)", Role: "refine"})
	// HITL already supplies its numbered Refine report action.
	hasReport := false
	for _, item := range spec.Items {
		if item.Role == "refine-report" {
			hasReport = true
		}
	}
	if !hasReport {
		if pointer, ok := latestRefinePointer(r.deps, r.manifest); ok {
			spec.Items = append(spec.Items, ui.GateMenuItem{Key: "p", Label: "Read the refine report (no agent runs)", Details: []string{pointer.Path}})
		}
	}
	spec.Items = append(spec.Items, exit)
}

func (e gateEnv) refineAndReturnToMenu(m *Manifest) (bool, error) {
	options := e.cfg.refine.options
	_, runErr := refineResolvedSet(e.d, e.cfg.Value(), refineCoreOptions{
		DefPath: e.definitionPath, RuntimePath: e.runtimePath, SetID: e.taskSetID,
		Repo:   e.cfg.refine.repo,
		Agents: options.Agents, Effort: options.Effort, PhaseChoice: e.cfg.refineChoice,
		Timeout: options.Timeout, Output: e.out, Convention: options.Convention, Overlay: options.Overlay,
		admission: AdmissionWait,
	})
	if runErr != nil {
		fmt.Fprintf(outputFor(e.out), "Could not refine: %v\n", runErr)
	}
	after, err := RefreshWith(e.d, e.definitionPath, e.statePath)
	if err != nil {
		return false, err
	}
	fresh := after.Manifests[e.taskSetID]
	if fresh == nil {
		fresh = m
	}
	ApplyVerifyVerdicts(e.d, after, e.cfg.Value(), e.runtimePath)
	fmt.Fprintln(e.out)
	Render(e.out, after)
	return e.reopenGateMenu(fresh)
}

func (e gateEnv) readRefineReport(m *Manifest) {
	if pointer, ok := latestRefinePointer(e.d, m); ok {
		pageReportDocument(e.d, e.in, e.runtimePath, e.out, pointer)
	}
}
