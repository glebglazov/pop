package tasks

import "strings"

// PlanningSourcesConvention resolves how a repository reads Planning sources.
// The caller holds both packages; conventions depends on tasks and cannot be
// imported here.
type PlanningSourcesConvention func(cwd string) (string, error)

func resolvePlanningSourcesConvention(m *Manifest, opts verifyCoreOptions) string {
	if len(m.PlanningSources) == 0 || opts.PlanningSourcesConvention == nil {
		return ""
	}
	prose, err := opts.PlanningSourcesConvention(opts.RuntimePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(prose)
}

type verifierPlanningSourceRow struct {
	PlanningSource
	Claimed bool
	Claims  []verifierTaskRow
}

func verifierPlanningSourceRows(m *Manifest) []verifierPlanningSourceRow {
	rows := make([]verifierPlanningSourceRow, 0, len(m.PlanningSources))
	for _, source := range m.PlanningSources {
		row := verifierPlanningSourceRow{PlanningSource: source}
		for _, task := range m.Tasks {
			for _, id := range task.PlanningSources {
				if id == source.ID {
					row.Claims = append(row.Claims, verifierTaskRow{ID: task.ID, Type: task.Type, Status: task.Status, Title: task.Title})
					break
				}
			}
		}
		row.Claimed = len(row.Claims) > 0
		rows = append(rows, row)
	}
	return rows
}
