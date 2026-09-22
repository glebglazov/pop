package tasks

import (
	"strings"

	"github.com/glebglazov/pop/config"
)

// The implement half of Planning source attribution (ADR-0276), beside the
// Verifier's half in verify_planning_sources.go: what a builder is told about
// the sources its task claims, and nothing about how they are judged.

// implementPlanningSourcesConvention resolves the notice-free convention that
// tells a builder how to read Planning sources. The same seam supplies the
// Verifier; the implement toggle only controls whether builders receive it.
func implementPlanningSourcesConvention(cfg *config.Config, resolve PlanningSourcesConvention, cwd string) string {
	if !cfg.ImplementIncludesPlanningSources() || resolve == nil {
		return ""
	}
	prose, err := resolve(cwd)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(prose)
}

// implementPlanningSourcesView is present only when the toggle is on and the
// set declares sources. This keeps every other prompt byte-identical.
type implementPlanningSourcesView struct {
	Recorded   bool
	Convention string
	Sources    []PlanningSource
}

func implementPlanningSources(m *Manifest, task Task, convention string) implementPlanningSourcesView {
	if m == nil || len(m.PlanningSources) == 0 || strings.TrimSpace(convention) == "" {
		return implementPlanningSourcesView{}
	}
	claimed := make(map[string]bool, len(task.PlanningSources))
	for _, id := range task.PlanningSources {
		claimed[id] = true
	}
	sources := make([]PlanningSource, 0, len(claimed))
	for _, source := range m.PlanningSources {
		if claimed[source.ID] {
			sources = append(sources, source)
		}
	}
	return implementPlanningSourcesView{Recorded: true, Convention: strings.TrimSpace(convention), Sources: sources}
}
